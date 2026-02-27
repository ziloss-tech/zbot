# ZBOT Logging & Observability Spec
**Status:** Planning  
**Author:** Jeremy Lerwick / Claude  
**Date:** 2026-02-26

---

## Current State Audit

### What's already good
- Structured `slog` JSON logging throughout — every log line is machine-readable
- Async Postgres audit logger (PGAuditLogger) writing tool calls, model calls, workflow events
- Most critical events have `session_id`, `task_id`, `workflow_id` fields

### What's broken or missing

#### 1. Missing correlation fields on key events
| Event | Has session_id | Has workflow_id | Has task_id |
|---|---|---|---|
| `agent turn start` | ✅ | ❌ | ❌ |
| `agent turn complete` | ✅ | ❌ | ❌ |
| `planning goal` | ❌ | ❌ | ❌ |
| `plan ready` | ❌ | ❌ | ❌ |
| `worker error` | ❌ | ❌ | ❌ |

Without `workflow_id` on agent turns, you can't answer: "which workflow caused this model call?"

#### 2. No task failure log event
When a task fails, only `worker error` fires — generic, missing task_id and workflow_id.
There's no `task failed` event with the error detail, task name, and retry count.

#### 3. No cost tracking
Token counts are logged on model calls but nothing accumulates them.
No daily cost field. No per-workflow cost. Flying blind on spend.

#### 4. No `component` field
Can't filter logs by subsystem. "Did the planner error or the executor?"
requires reading the message text instead of filtering a field.

#### 5. Audit channel drops silently
When the 256-slot audit channel fills, events are dropped with a WARN.
No counter tracks how often this happens. Data loss is invisible.

#### 6. No standard severity field
`slog` levels (DEBUG/INFO/WARN/ERROR) exist but aren't consistent.
Some errors are logged at WARN, some important events at DEBUG.

#### 7. Errors missing context
`worker error` only logs `worker` and `err`. No `task_id`, `workflow_id`, or `task_name`.
Makes post-mortem debugging painful.

---

## Target Architecture

```
ZBOT process (Go slog → JSON to stdout)
        │
        ▼
  Promtail (log shipper — reads stdout, adds labels, forwards)
        │
        ▼
   Loki (log aggregation & storage on GCP)
        │
        ▼
  Grafana (query, dashboards, alerts)
        │
        ├── Log Explorer (full text search, filter by workflow/session/level)
        ├── Dashboard: Live Activity Feed
        ├── Dashboard: Workflow Health (success/fail rates, duration)
        ├── Dashboard: Token Cost Tracker (daily/weekly spend)
        └── Alerts: Slack notification on error spike or workflow failure
```

Prometheus sits alongside Loki for metrics (counters, gauges):
```
ZBOT /metrics endpoint (Prometheus exposition format)
        │
        ▼
  Prometheus (scrapes every 15s)
        │
        ▼
  Grafana (same instance — both data sources)
```

---

## Phase 1: Fix the Logs (Code Changes in ZBOT)

Before shipping to Loki, make sure every event has the right fields.

### Standard fields every log line must have
```
time        — already present (slog default)
level       — already present
msg         — already present
component   — NEW: "planner" | "executor" | "orchestrator" | "gateway" | "scheduler" | "memory"
```

### Standard fields for workflow-related events
```
workflow_id  — on any event touching a workflow
task_id      — on any event touching a task
session_id   — on any event touching a user session
```

### New log events needed

**task_failed** (orchestrator):
```json
{
  "level": "ERROR",
  "msg": "task failed",
  "component": "orchestrator",
  "workflow_id": "abc123",
  "task_id": "task-1",
  "task_name": "Fetch HubSpot pricing",
  "error": "fetch_url: connection timeout after 30s",
  "attempt": 1,
  "duration_ms": 30042
}
```

**plan_complete** (planner — after tasks submitted to DB):
```json
{
  "level": "INFO",
  "msg": "plan submitted",
  "component": "planner",
  "workflow_id": "abc123",
  "session_id": "U09EQGVUVNE",
  "goal": "research GHL competitors",
  "task_count": 5,
  "parallel_tasks": 4,
  "warnings": []
}
```

**token_usage** (agent — per turn):
```json
{
  "level": "INFO",
  "msg": "agent turn complete",
  "component": "executor",
  "session_id": "workflow-abc123-task-1",
  "workflow_id": "abc123",
  "task_id": "task-1",
  "model": "claude-sonnet-4-6",
  "input_tokens": 4821,
  "output_tokens": 1203,
  "cost_usd": 0.019,
  "tools_invoked": 3,
  "duration_ms": 8432
}
```

**audit_drop** (counter — when channel fills):
```json
{
  "level": "WARN",
  "msg": "audit event dropped",
  "component": "audit",
  "kind": "tool_call",
  "total_dropped": 47
}
```

### Cost calculation constants (Claude Sonnet pricing)
```go
const (
    costPerInputToken  = 0.000003   // $3 per million input tokens
    costPerOutputToken = 0.000015   // $15 per million output tokens
)
```

---

## Phase 2: Infrastructure on GCP

### What to deploy
Single `docker-compose.yml` on a GCP `e2-micro` VM (free tier) or `e2-small` ($7/month):

```yaml
services:
  loki:
    image: grafana/loki:latest
    ports: ["3100:3100"]
    volumes: [loki_data:/loki]

  promtail:
    image: grafana/promtail:latest
    # reads ZBOT stdout via Cloud Logging or log file mount

  prometheus:
    image: prom/prometheus:latest
    ports: ["9090:9090"]
    # scrapes ZBOT :9091/metrics every 15s

  grafana:
    image: grafana/grafana:latest
    ports: ["3000:3000"]
    environment:
      - GF_AUTH_ANONYMOUS_ENABLED=false
      - GF_SECURITY_ADMIN_PASSWORD=${GRAFANA_PASSWORD}
    volumes: [grafana_data:/var/lib/grafana]
```

### Log shipping option
ZBOT runs on your Mac locally right now. Two options for getting logs to Loki:
- **Option A (dev):** Run Loki/Grafana locally via Docker, Promtail reads ZBOT stdout
- **Option B (prod):** ZBOT deployed to Cloud Run → Cloud Logging → Loki via log sink

Start with Option A since ZBOT is local. Migrate to Option B when ZBOT goes to Cloud Run.

### ZBOT metrics endpoint
Add a `/metrics` endpoint to ZBOT's web server exposing:
```
zbot_messages_total{status="success|error"} counter
zbot_tasks_total{status="complete|failed|canceled"} counter
zbot_workflows_total{status="complete|failed"} counter
zbot_tokens_total{model="claude-sonnet-4-6|gpt-4o", type="input|output"} counter
zbot_cost_usd_total{model="..."} counter
zbot_tool_calls_total{tool="web_search|fetch_url|..."} counter
zbot_task_duration_seconds histogram
zbot_model_latency_seconds histogram
```

---

## Phase 3: Grafana Dashboards

### Dashboard 1: Live Activity Feed
- Log stream filtered to level >= INFO
- Filter controls: workflow_id, session_id, component, level
- Color coded: ERROR=red, WARN=yellow, INFO=green, DEBUG=grey
- Auto-refresh every 5s

### Dashboard 2: Workflow Health
- Table: recent workflows with status, duration, task count, cost
- Graph: tasks complete/failed per hour
- Stat: current active workflows
- Alert: fire Slack if any workflow has >2 failed tasks

### Dashboard 3: Token Cost Tracker
- Stat: today's cost in USD (running total)
- Graph: cost per day last 30 days
- Table: most expensive workflows
- Stat: tokens used today by model

### Dashboard 4: Tool Performance
- Bar: tool calls by tool name (last 24h)
- Graph: tool error rate over time
- Table: slowest tool calls (p95 latency)

---

## Phase 4: Alerts

All alerts fire to your ZBOT Slack channel:

| Alert | Condition | Message |
|---|---|---|
| Workflow failure | >2 tasks failed in one workflow | "⚠️ Workflow {id} failing — {n} tasks failed" |
| Error spike | >5 ERRORs in 5 minutes | "🔴 ZBOT error spike — {n} errors in 5min" |
| Daily cost | cost_usd_total > $5/day | "💰 Daily token spend hit $5 — check workflows" |
| Audit drops | audit_event_dropped fires | "⚠️ Audit events being dropped — channel full" |

---

## Implementation Order

1. **Phase 1 code changes** — fix correlation fields, add missing events, add cost field (~2 hours)
2. **Phase 2 infrastructure** — Docker Compose locally, wire Promtail to ZBOT stdout (~1 hour)
3. **Phase 3 dashboards** — build in Grafana UI or via provisioned JSON (~2 hours)
4. **Phase 4 alerts** — configure in Grafana, wire to Slack webhook (~30 min)

Total: ~6 hours from zero to full observability.

---

## Files to Create/Modify

| File | Change |
|---|---|
| `internal/agent/agent.go` | Add workflow_id, task_id, component, cost_usd to log fields |
| `internal/workflow/orchestrator.go` | Add task_failed event, fix worker error fields |
| `internal/planner/planner.go` | Add session_id, workflow_id, component to log fields |
| `internal/audit/pglogger.go` | Add drop counter, track total dropped |
| `internal/metrics/metrics.go` | NEW — Prometheus counters and histograms |
| `internal/web/server.go` | Add /metrics endpoint |
| `deploy/observability/docker-compose.yml` | NEW — Loki + Promtail + Prometheus + Grafana |
| `deploy/observability/promtail.yml` | NEW — Promtail config pointing at ZBOT stdout |
| `deploy/observability/prometheus.yml` | NEW — scrape config |
| `deploy/observability/grafana/dashboards/` | NEW — provisioned dashboard JSON |
