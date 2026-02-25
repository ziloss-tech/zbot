# ZBOT Sprint 8 — Coworker Mission Brief
## Objective: Web UI + Audit Logging Dashboard

You are working on ZBOT, a personal AI agent for Jeremy Lerwick (CEO, Ziloss Technologies).
The codebase is at: ~/Desktop/zbot
GitHub: https://github.com/jeremylerwick-max/zbot (private)
GCP Project: ziloss (project number: 203743871797)

ZBOT has a full skills system. Sprint 7 is done.
Your job is to build a local web dashboard so Jeremy can see everything ZBOT has done.

---

## Current State (Sprint 7 Complete)

- Real Claude responses via Slack ✅
- Cross-session memory via pgvector ✅
- Vision: images + PDFs ✅
- Anti-block web scraper ✅
- Multi-step parallel workflow engine ✅
- Cron scheduler + webhooks ✅
- Skills: GHL, GitHub, Google Sheets, Email ✅
- go build ./... passes clean ✅

### Existing audit infrastructure:
- internal/audit/noop.go — NoopLogger logs to slog but doesn't persist
- agent/ports.go — AuditLogger interface defined

---

## Sprint 8 Tasks — Complete ALL of These

### TASK 1: Real Audit Logger (Postgres)

Create: internal/audit/pglogger.go

Replace the noop logger with real Postgres persistence.

Database schema (add to new migration file migrations/004_audit.sql):

```sql
CREATE TABLE IF NOT EXISTS zbot_audit_tool_calls (
    id           TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    session_id   TEXT NOT NULL,
    tool_name    TEXT NOT NULL,
    input        JSONB NOT NULL,
    output       TEXT,
    is_error     BOOLEAN NOT NULL DEFAULT FALSE,
    duration_ms  BIGINT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS zbot_audit_model_calls (
    id            TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    session_id    TEXT NOT NULL,
    model         TEXT NOT NULL,
    input_tokens  INTEGER NOT NULL,
    output_tokens INTEGER NOT NULL,
    duration_ms   BIGINT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS zbot_audit_workflow_events (
    id           TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    workflow_id  TEXT NOT NULL,
    task_id      TEXT,
    event        TEXT NOT NULL,
    detail       TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS audit_tool_session_idx ON zbot_audit_tool_calls(session_id, created_at DESC);
CREATE INDEX IF NOT EXISTS audit_model_session_idx ON zbot_audit_model_calls(session_id, created_at DESC);
CREATE INDEX IF NOT EXISTS audit_workflow_idx ON zbot_audit_workflow_events(workflow_id, created_at DESC);
```

Implement agent.AuditLogger with async writes (use a buffered channel, don't block the agent):
```go
type PGAuditLogger struct {
    db     *pgxpool.Pool
    writes chan auditWrite
    logger *slog.Logger
}

// Start the background writer goroutine.
func (a *PGAuditLogger) Start(ctx context.Context)
```

### TASK 2: HTTP Server (Loopback Only)

Create: internal/webui/server.go

```go
package webui

import (
    "net/http"
    "log/slog"
)

// Server serves the ZBOT web dashboard.
// CRITICAL: Binds to 127.0.0.1:18790 ONLY — never 0.0.0.0.
type Server struct {
    port   int
    db     *pgxpool.Pool
    logger *slog.Logger
    mux    *http.ServeMux
}

func New(db *pgxpool.Pool, logger *slog.Logger) *Server

func (s *Server) Start(ctx context.Context) error
```

Bind address: 127.0.0.1:18790

Routes:
```
GET  /                          → redirect to /conversations
GET  /conversations             → conversation history browser
GET  /conversations/{sessionID} → single session view
GET  /memory                   → memory viewer
DELETE /memory/{id}             → delete a memory
GET  /workflows                → workflow status list
POST /workflows/{id}/cancel    → cancel a workflow
GET  /schedules                → scheduled jobs list
DELETE /schedules/{id}         → delete a scheduled job
GET  /audit                    → audit log viewer
GET  /api/stats                → JSON stats (token usage, tool call counts)
```

### TASK 3: Web UI — Conversation History Browser

In internal/webui/handlers.go implement GET /conversations:

Query: select distinct session_id, max(created_at) as last_active from zbot_audit_model_calls group by session_id order by last_active desc limit 50

Render a simple HTML page (use Go html/template):
- List of sessions with session ID, last active time, total tokens used
- Click session → /conversations/{sessionID} shows the model calls and tool calls for that session in chronological order

No JavaScript frameworks. Plain HTML + minimal CSS (embed with go:embed).
Use the existing Go standard library html/template — no external template engines.

### TASK 4: Web UI — Memory Viewer

GET /memory:
- List all zbot_memories sorted by created_at DESC
- Show: ID (truncated), source, tags, content preview (first 120 chars), created_at
- Delete button next to each memory → calls DELETE /memory/{id}
- Search box at top → filters by keyword (simple ILIKE query)

Use htmx (loaded from CDN, no npm) for the delete and search without full page reload:
```html
<script src="https://unpkg.com/htmx.org@1.9.10"></script>
```

### TASK 5: Web UI — Workflow Status

GET /workflows:
- List recent workflows with ID, status, task count, created_at
- Expandable rows showing individual task status
- Cancel button for running workflows

### TASK 6: Web UI — Audit Log Viewer

GET /audit:
- Tabs: Tool Calls | Model Calls | Workflow Events
- Tool Calls table: time, session, tool name, duration, success/error, input preview
- Model Calls table: time, session, model, input tokens, output tokens, cost estimate
- Workflow Events table: time, workflow ID, task ID, event, detail
- Search/filter by session ID or tool name
- Pagination: 50 rows per page

Cost estimate formula: (input_tokens * 0.000003) + (output_tokens * 0.000015) for claude-sonnet-4-6

### TASK 7: Static Assets

Create: internal/webui/static/

Embed with go:embed:
- style.css — minimal dark theme, no frameworks
- Use system fonts only (no Google Fonts requests)

```go
//go:embed static/*
var staticFiles embed.FS
```

### TASK 8: Wire into wire.go

```go
// Real audit logger
pgAudit := audit.NewPGAuditLogger(pgDB, logger)
pgAudit.Start(ctx)

// Web UI
webServer := webui.New(pgDB, logger)
go webServer.Start(ctx)

logger.Info("Web UI available", "url", "http://localhost:18790")
```

Replace NoopLogger with PGAuditLogger in the agent constructor.

---

## Definition of Done

1. Run ZBOT, open http://localhost:18790 in browser
2. See conversation history — click a session to see its full tool call + model call log
3. Go to /memory — see all saved memories, delete one, confirm it's gone
4. Go to /workflows — see recent workflow runs and task statuses
5. Go to /audit — see every tool call with timing and success/error status
6. See cost estimates on the Model Calls tab
7. go build ./... passes clean

---

## Go Dependencies

No new external dependencies. htmx loaded from CDN in HTML templates.
Use stdlib: net/http, html/template, embed

---

## Git Commit

```bash
cd ~/Desktop/zbot
git add -A
git commit -m "Sprint 8: Web UI + real audit logging — full visibility into ZBOT activity

- migrations/004_audit.sql: tool calls, model calls, workflow events tables
- internal/audit/pglogger.go: Async Postgres audit logger
- internal/webui/server.go: HTTP server bound to 127.0.0.1:18790
- internal/webui/handlers.go: Conversations, memory, workflows, audit views
- internal/webui/static/: Embedded CSS
- cmd/zbot/wire.go: PGAuditLogger + WebUI wired
- Open http://localhost:18790 to see everything ZBOT has done"
git push origin main
```

## Important Notes

- go build ./... must pass after every change.
- Web server MUST bind to 127.0.0.1 — NEVER 0.0.0.0. This is a security requirement.
- Audit writes are async (buffered channel) — never block the agent for a DB write.
- Use go:embed for static files — don't serve files from disk at runtime.
- DELETE /memory/{id} should soft-confirm in the UI before actually deleting.
- No authentication on the web UI — it's loopback-only, that's the security model.
