# ZBOT Sprint 6 — Coworker Mission Brief
## Objective: Proactive Automation + Scheduling

You are working on ZBOT, a personal AI agent for Jeremy Lerwick (CEO, Ziloss Technologies).
The codebase is at: ~/Desktop/zbot
GitHub: https://github.com/jeremylerwick-max/zbot (private)
GCP Project: ziloss (project number: 203743871797)

ZBOT runs multi-step parallel workflows. Sprint 5 is done.
Your job is to make ZBOT do things on a schedule without being prompted.

---

## Current State (Sprint 5 Complete)

- Real Claude responses via Slack ✅
- Cross-session memory via pgvector ✅
- Vision: images + PDFs ✅
- Anti-block web scraper ✅
- Multi-step parallel workflow engine ✅
- go build ./... passes clean ✅

---

## Sprint 6 Tasks — Complete ALL of These

### TASK 1: Cron Scheduler (Pure Go, No External Deps)

Create: internal/scheduler/scheduler.go

```go
package scheduler

import (
    "context"
    "log/slog"
    "sync"
    "time"
)

// Job is a scheduled task.
type Job struct {
    ID          string
    Name        string
    CronExpr    string    // standard cron: "0 8 * * 1" = every Monday 8am
    Instruction string    // what to tell the agent when it fires
    SessionID   string    // which Slack session to reply to
    NextRun     time.Time
    CreatedAt   time.Time
}

// Scheduler ticks every minute and fires any due jobs.
type Scheduler struct {
    mu      sync.RWMutex
    jobs    map[string]*Job
    handler func(ctx context.Context, sessionID, instruction string)
    store   JobStore
    logger  *slog.Logger
}

func New(store JobStore, handler func(ctx context.Context, sessionID, instruction string), logger *slog.Logger) *Scheduler

// Start launches the background tick loop.
func (s *Scheduler) Start(ctx context.Context)

// Add creates a new scheduled job (persists to DB).
func (s *Scheduler) Add(ctx context.Context, job Job) error

// Remove deletes a job by ID.
func (s *Scheduler) Remove(ctx context.Context, id string) error

// List returns all active jobs.
func (s *Scheduler) List(ctx context.Context) ([]Job, error)
```

Tick loop — check every 30 seconds:
```go
func (s *Scheduler) tick(ctx context.Context) {
    for {
        select {
        case <-ctx.Done():
            return
        case <-time.After(30 * time.Second):
            s.firedue(ctx)
        }
    }
}

func (s *Scheduler) fireDue(ctx context.Context) {
    s.mu.RLock()
    defer s.mu.RUnlock()
    now := time.Now()
    for _, job := range s.jobs {
        if now.After(job.NextRun) {
            go s.handler(ctx, job.SessionID, job.Instruction)
            job.NextRun = nextCronTime(job.CronExpr, now)
            s.store.UpdateNextRun(ctx, job.ID, job.NextRun)
        }
    }
}
```

Implement nextCronTime() using standard 5-field cron parsing (min, hour, dom, month, dow).
No external cron library — implement the parser from scratch (it's ~100 lines).

### TASK 2: Database Schema for Scheduled Jobs

Add to migrations (create migrations/003_scheduler.sql):

```sql
CREATE TABLE IF NOT EXISTS zbot_scheduled_jobs (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL,
    cron_expr    TEXT NOT NULL,
    instruction  TEXT NOT NULL,
    session_id   TEXT NOT NULL,
    next_run     TIMESTAMPTZ NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    active       BOOLEAN NOT NULL DEFAULT TRUE
);
```

Create: internal/scheduler/pgstore.go — JobStore interface backed by this table.

```go
type JobStore interface {
    Save(ctx context.Context, job Job) error
    Load(ctx context.Context) ([]Job, error)
    UpdateNextRun(ctx context.Context, id string, nextRun time.Time) error
    Delete(ctx context.Context, id string) error
}
```

On scheduler.Start(), load all active jobs from DB so scheduled jobs survive restarts.

### TASK 3: Natural Language Schedule Parser

Create: internal/scheduler/nlparse.go

Use Claude to parse natural language schedules into cron expressions:

```go
// ParseSchedule converts natural language to a cron expression.
// Examples:
//   "every morning at 8am"       → "0 8 * * *"
//   "every Monday at 9am"        → "0 9 * * 1"
//   "every weekday at 6pm"       → "0 18 * * 1-5"
//   "every hour"                 → "0 * * * *"
//   "every 30 minutes"           → "*/30 * * * *"
func ParseSchedule(ctx context.Context, llm agent.LLMClient, natural string) (cronExpr string, err error)
```

Prompt template:
```
Convert this schedule description to a standard 5-field cron expression.
Respond with ONLY the cron expression, nothing else.
Schedule: "{natural}"
```

Validate the returned cron expression before using it.

### TASK 4: URL/RSS Monitor

Create: internal/scheduler/monitor.go

```go
// URLMonitor watches a URL for content changes.
// Fires the handler when content differs from the last-seen hash.
type URLMonitor struct {
    url       string
    lastHash  string
    sessionID string
    handler   func(ctx context.Context, sessionID, change string)
}
```

Implementation:
- Fetch URL every N minutes (configurable, default 60)
- SHA256 hash the response body
- If hash differs from lastHash → fire handler with the diff summary
- Use Claude to generate a human-readable summary of what changed
- Store lastHash in zbot_data table (use DataStore from Sprint 5)

### TASK 5: Slack Commands for Schedule Management

In internal/gateway/slack.go, add command routing before passing to agent:

```
/schedule "every Monday at 9am" check the Ziloss CRM GitHub issues and summarize them
/schedules                          → list all active scheduled jobs
/unschedule <id>                    → cancel a scheduled job
/monitor https://example.com        → watch URL for changes, alert when it changes
```

When /schedule is received:
1. Parse the schedule with nlparse.ParseSchedule()
2. Create a Job with the instruction and sessionID
3. Persist to DB
4. Respond: "✅ Scheduled: 'check the Ziloss CRM GitHub issues' — next run Monday 9:00 AM"

### TASK 6: Inbound Webhook Receiver

Create: internal/gateway/webhook.go

```go
// WebhookGateway listens for HTTP POST requests and triggers the agent.
// Allows GHL, Zapier, or any external service to trigger ZBOT.
// Binds to 127.0.0.1 only — never exposed publicly without a tunnel.
type WebhookGateway struct {
    port    int
    secret  string // HMAC secret for request validation
    handler func(ctx context.Context, sessionID, userID, text string, attachments []Attachment) (string, error)
    logger  *slog.Logger
}

// POST /webhook
// Headers: X-ZBOT-Secret: <secret>
// Body: {"session_id": "...", "instruction": "..."}
// Response: {"reply": "..."}
```

Webhook secret stored in GCP Secret Manager as "zbot-webhook-secret".
Validate secret on every request — reject with 401 if missing or wrong.
Bind to 127.0.0.1:18791 — local only.

### TASK 7: Wire Everything into wire.go

```go
// Scheduler
jobStore := scheduler.NewPGJobStore(pgDB)
sched := scheduler.New(jobStore, func(ctx context.Context, sessionID, instruction string) {
    reply, _ := handler(ctx, sessionID, "ZBOT", instruction, nil)
    slackGW.Send(ctx, sessionID, reply)
}, logger)
sched.Start(ctx)

// Webhook
webhookSecret, _ := sm.Get(ctx, "zbot-webhook-secret")
webhookGW := gateway.NewWebhookGateway(18791, webhookSecret, handler, logger)
go webhookGW.Start(ctx)
```

---

## Definition of Done

1. DM ZBOT: `/schedule "every Monday at 9am" check the Ziloss CRM GitHub issues and summarize them`
2. ZBOT responds: "✅ Scheduled — next run Monday 9:00 AM"
3. Restart ZBOT → job is still scheduled (loaded from DB on boot)
4. DM ZBOT: `/schedules` → see the job listed
5. DM ZBOT: `/unschedule <id>` → job removed
6. Test webhook: `curl -X POST http://localhost:18791/webhook -H "X-ZBOT-Secret: <secret>" -d '{"session_id":"test","instruction":"say hello"}'` → get a response
7. go build ./... passes clean

---

## Git Commit

```bash
cd ~/Desktop/zbot
git add -A
git commit -m "Sprint 6: Proactive automation — cron scheduler, NL schedule parsing, URL monitor, webhooks

- internal/scheduler/scheduler.go: Pure Go cron scheduler with 30s tick
- internal/scheduler/nlparse.go: Natural language → cron via Claude
- internal/scheduler/monitor.go: URL change monitor
- internal/scheduler/pgstore.go: Persisted jobs survive restarts
- internal/gateway/webhook.go: Inbound webhook receiver (127.0.0.1 only)
- migrations/003_scheduler.sql: zbot_scheduled_jobs table
- cmd/zbot/wire.go: Scheduler + webhook wired
- ZBOT now runs tasks autonomously on a schedule"
git push origin main
```

## Important Notes

- Never put secrets in code. Webhook secret in GCP Secret Manager as "zbot-webhook-secret".
- go build ./... must pass after every change.
- Scheduler binds to nothing — it's internal only, fires the same handler function.
- Webhook binds to 127.0.0.1 ONLY — never 0.0.0.0.
- On startup: load all jobs from DB before starting the tick loop.
- nextCronTime() must handle DST transitions gracefully — use time.In(time.Local).
