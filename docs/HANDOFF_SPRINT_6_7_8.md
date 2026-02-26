# ZBOT Sprints 6, 7, 8 — Handoff Notes

## What Was Built

### Sprint 6: Proactive Automation + Scheduling
- **`internal/scheduler/cron.go`** — Pure Go 5-field cron parser (min, hour, dom, month, dow). Supports `*`, `*/N`, `N-M`, `N,M,O`. No external deps.
- **`internal/scheduler/scheduler.go`** — Tick loop (30s), fires due jobs async, persists to Postgres, survives restarts.
- **`internal/scheduler/pgstore.go`** — `zbot_scheduled_jobs` table, auto-created on first run.
- **`internal/scheduler/nlparse.go`** — Uses Claude to convert natural language → cron expression.
- **`internal/scheduler/monitor.go`** — URL change monitor with SHA256 diff detection.
- **`internal/gateway/webhook.go`** — HTTP webhook on 127.0.0.1:18791, secret validation via `X-ZBOT-Secret` header.

**Slack commands added to wire.go handler:**
- `/schedule <cron> | <instruction>` — create a scheduled job
- `/schedules` — list all active jobs
- `/unschedule <id>` — remove a job

### Sprint 7: Skills System
- **`internal/skills/registry.go`** — `Skill` interface + `Registry` for extensible tool collections.
- **`internal/skills/ghl/`** — GoHighLevel CRM (6 tools: get_contacts, get_contact, update_contact, get_conversations, send_message, get_pipeline)
- **`internal/skills/github/`** — GitHub REST API (5 tools: list_issues, get_issue, create_issue, list_prs, get_file)
- **`internal/skills/sheets/`** — Google Sheets v4 API (4 tools: read, write, append, list)
- **`internal/skills/email/`** — SMTP send with confirmation (1 tool: send_email)

**Key patterns:**
- Each skill implements `Skill` interface: `Name()`, `Description()`, `Tools()`, `SystemPromptAddendum()`
- Missing secrets = skill skipped with warning (no crash)
- `send_message` and `send_email` require `confirm=true` to actually send

### Sprint 8: Web UI + Audit Logging
- **`internal/audit/pglogger.go`** — Async Postgres audit logger (256-entry buffered channel). Auto-creates tables on startup.
- **`internal/webui/server.go`** — HTTP server on 127.0.0.1:18790
- **`internal/webui/handlers.go`** — All page handlers:
  - `/conversations` — session list with token counts and costs
  - `/conversations/{id}` — model calls + tool calls for a session
  - `/memory` — memory viewer with search (ILIKE) and htmx delete
  - `/workflows` — workflow list with task progress
  - `/audit` — tabbed view (Tool Calls / Model Calls / Workflow Events) with pagination and session filter
  - `/api/stats` — JSON stats endpoint
- **`internal/webui/static/style.css`** — Dark theme, system fonts only
- **`migrations/004_audit.sql`** — Schema for audit tables + indexes

## Manual Steps Required

### 1. Install Google Sheets dependency
```bash
cd ~/Desktop/zbot
go get google.golang.org/api/sheets/v4
go get google.golang.org/api/option
go mod tidy
```

### 2. Add secrets to GCP Secret Manager
These secrets need to exist for the skills to activate:
```
ghl-api-key              — GoHighLevel API key
github-token             — GitHub personal access token
google-sheets-credentials — Google service account JSON
smtp-host                — SMTP server (e.g. smtp.gmail.com)
smtp-user                — SMTP username
smtp-pass                — SMTP password
smtp-from                — From address (e.g. jeremy@ziloss.com)
zbot-webhook-secret      — Optional HMAC secret for webhook auth
```

### 3. Run the audit migration
```bash
psql "postgresql://ziloss:ZilossMemory2024!@34.28.163.109:5432/ziloss_memory" -f migrations/004_audit.sql
```
Note: The PGAuditLogger also auto-creates tables on startup, so this is optional.

### 4. Build and test
```bash
cd ~/Desktop/zbot
go build ./...
go run ./cmd/zbot
```

### 5. Verify
- Open http://localhost:18790 in browser → should see dashboard
- Send ZBOT a Slack message → should see it in /conversations after
- Try `/schedules` in Slack → should return empty list
- Try `/schedule 0 9 * * 1 | Check open GHL leads` → should confirm

## Architecture Summary

```
cmd/zbot/wire.go          ← orchestrates everything
internal/
├── agent/                ← core domain (ports.go, agent.go)
├── audit/                ← NoopLogger + PGAuditLogger
├── gateway/              ← Slack + Webhook gateways
├── llm/                  ← Anthropic Claude client
├── memory/               ← pgvector memory store
├── platform/             ← config, rate limiter, task graph parser
├── scheduler/            ← cron engine + job store
├── scraper/              ← proxy pool, rate limiter, browser, cache
├── secrets/              ← GCP Secret Manager
├── skills/               ← Skill interface + registry
│   ├── ghl/              ← GoHighLevel CRM
│   ├── github/           ← GitHub REST API
│   ├── sheets/           ← Google Sheets v4
│   └── email/            ← SMTP email
├── tools/                ← core agent tools
├── webui/                ← dashboard (server + handlers + CSS)
└── workflow/             ← parallel task orchestrator
```

## What's Not Wired Yet

- **URL Monitor** (`scheduler/monitor.go`): Created but not wired into wire.go. To use it, add a `/monitor <url>` Slack command that creates a `URLMonitor` instance.
- **NL Schedule Parser** (`scheduler/nlparse.go`): Created but the `/schedule` command uses raw cron expressions. To add natural language: detect non-cron input in the handler and call `ParseSchedule()` to convert.
