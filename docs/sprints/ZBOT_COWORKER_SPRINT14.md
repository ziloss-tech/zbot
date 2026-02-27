# ZBOT Sprint 14 — Coworker Mission Brief
## Objective: Proactive Scheduling — ZBOT Does Things Without Being Asked

You are working on ZBOT, a personal AI agent for Jeremy Lerwick (CEO, Ziloss Technologies).
The codebase is at: ~/Desktop/zbot
GitHub: https://github.com/jeremylerwick-max/zbot (private)
GCP Project: ziloss (project number: 203743871797)

Sprint 13 is complete. Workspace file panel is live.
Your job is to make ZBOT proactive — able to run tasks on a schedule without Jeremy having to prompt it.

---

## Current State (Sprint 13 Complete)

- Dual Brain Command Center UI live at http://localhost:18790 ✅
- GPT-4o planner + Claude executor + GPT-4o critic ✅
- Cross-session memory via pgvector ✅
- Workspace file panel with preview ✅
- Skills: search, memory, GHL, GitHub, Google Sheets, Email ✅
- Scheduler package exists (cron.go, nlparse.go, pgstore.go, monitor.go) ✅
- BUT: scheduler is NOT connected to the planner/orchestrator ❌
- No way to schedule tasks from the UI ❌
- Jeremy has to manually trigger everything ❌

---

## Architecture Overview

```
internal/scheduler/
  cron.go      — cron expression parser + NextCronTime() ✅
  nlparse.go   — ParseSchedule() converts "every Monday at 9am" → cron expr ✅
  pgstore.go   — PGJobStore saves/loads jobs from DB ✅
  monitor.go   — URLMonitor watches URLs for changes ✅
  runner.go    — NEW: connects scheduler to planner+orchestrator

DB: zbot_scheduled_jobs table (create if not exists)
UI: SchedulePanel.tsx — manage scheduled tasks

Flow:
  User types: "schedule: every morning at 8am research AI news and send me a Slack summary"
  → CommandBar detects "schedule:" prefix
  → ParseSchedule() converts to cron
  → Job saved to DB with the goal text
  → Runner fires goal at scheduled time → planner → orchestrator → executes
  → Result sent to Jeremy via Slack
```

---

## Sprint 14 Tasks — Complete ALL in Order

### PHASE 1: Scheduler Runner — Connect to Dual-Brain

File: `internal/scheduler/runner.go` (NEW)

The runner is the bridge between the scheduler and the planner+orchestrator:

```go
type Runner struct {
    store      JobStore
    planner    Planner         // interface — same planner used by web UI
    workflow   WorkflowStore   // interface — same orchestrator
    slack      SlackGateway    // interface — to send results back
    logger     *slog.Logger
    interval   time.Duration   // check interval, default 30s
}

// Start() — goroutine that:
// 1. Every 30 seconds, loads all active jobs from DB
// 2. For each job, calls NextCronTime(job.cronExpr, job.lastRun)
// 3. If next run time is in the past (overdue) or within the next 30s window:
//    a. Mark job as running in DB (prevent double-fire)
//    b. Submit goal to planner → get task graph
//    c. Execute via orchestrator
//    d. When complete, send result summary to Jeremy via Slack
//    e. Update job.lastRun = now
//    f. Mark job as idle in DB
// 4. Log: "scheduled job fired: {job.name} ({job.cronExpr})"

// Handle missed runs: if lastRun is more than 2 intervals ago, fire once and update
// Do NOT fire multiple times for missed runs — just catch up once
```

**Job schema (create table if not exists in pgstore.go):**
```sql
CREATE TABLE IF NOT EXISTS zbot_scheduled_jobs (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    goal TEXT NOT NULL,
    cron_expr TEXT NOT NULL,
    natural_schedule TEXT NOT NULL,  -- "every morning at 8am" (human readable)
    status TEXT NOT NULL DEFAULT 'active',  -- active, paused, running
    last_run TIMESTAMPTZ,
    next_run TIMESTAMPTZ,
    run_count INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

Wire Runner into wire.go — start it after the orchestrator is ready.

**Checkpoint:** Build passes. Logs show "scheduler runner started — checking every 30s".

---

### PHASE 2: Schedule Command in Command Bar

File: `internal/webui/api_handlers.go` — add schedule endpoints
File: `internal/webui/frontend/src/components/CommandBar.tsx` — detect "schedule:" prefix

**New API endpoints:**
```
POST /api/schedule          — create a new scheduled job
GET  /api/schedules         — list all scheduled jobs
PUT  /api/schedule/:id/pause  — pause a job
PUT  /api/schedule/:id/resume — resume a job
DELETE /api/schedule/:id    — delete a job
POST /api/schedule/:id/run  — trigger a job immediately (run now)
```

**POST /api/schedule request body:**
```json
{
  "name": "Morning AI News Briefing",
  "goal": "research the top 5 AI news stories from the last 24 hours and write a summary report",
  "natural_schedule": "every morning at 8am"
}
```

Server-side flow:
1. Call `scheduler.ParseSchedule(ctx, llm, natural_schedule)` → get cron expression
2. Compute next_run from cron expression
3. Save to DB
4. Return job with cron_expr and next_run

**CommandBar changes:**
- If input starts with "schedule:" — show a schedule creation modal instead of submitting a plan
- Extract the goal and schedule from the text:
  - "schedule: every morning at 8am research AI news" 
  - → goal: "research AI news", schedule: "every morning at 8am"
- Show confirmation: "Schedule this to run every morning at 8:00 AM?" [Confirm] [Cancel]
- On confirm, POST /api/schedule

**Checkpoint:** Type "schedule: every day at 9am check my GitHub issues" → confirmation modal → confirm → job created.

---

### PHASE 3: Schedule Panel in UI

File: `internal/webui/frontend/src/components/SchedulePanel.tsx` (NEW)
File: `internal/webui/frontend/src/App.tsx` — add schedule panel toggle

**SchedulePanel layout:**
```
┌────────────────────────────────────────┐
│ ⏰ Scheduled Tasks           [+ New]   │
│ 3 active • 1 paused                    │
├────────────────────────────────────────┤
│ ● Morning AI News Briefing             │
│   "research top 5 AI news stories..."  │
│   every morning at 8am                 │
│   Next run: tomorrow 8:00 AM           │
│   Last run: 6 hours ago ✅             │
│   [Run Now] [Pause] [Delete]           │
│────────────────────────────────────────│
│ ● Weekly GitHub Issues Summary         │
│   "check zbot GitHub issues and..."    │
│   every Monday at 9am                  │
│   Next run: Mon Feb 2 9:00 AM          │
│   Never run yet                        │
│   [Run Now] [Pause] [Delete]           │
│────────────────────────────────────────│
│ ⏸ GHL Lead Check (paused)             │
│   "check new GHL leads tagged..."      │
│   every weekday at 10am                │
│   [Resume] [Delete]                    │
└────────────────────────────────────────┘
```

- Triggered by clock icon in top nav
- Status dot: green = active, yellow = running, gray = paused
- "Run Now" fires the job immediately (POST /api/schedule/:id/run)
- "Pause" / "Resume" toggles job status
- "Delete" removes the job
- "+ New" button opens an inline form:
  - Name field
  - Goal textarea
  - Schedule field with example: "every morning at 8am", "every Monday at 9am", "every hour"
  - [Create Schedule] button
- Auto-refresh every 30 seconds
- Show last run result status: ✅ success, ❌ failed, ⏳ running

**Checkpoint:** Schedule panel opens. Jobs visible. Run Now fires a job immediately.

---

### PHASE 4: URL/Change Monitoring

File: `internal/scheduler/monitor.go` — already exists, wire it up
File: `internal/webui/api_handlers.go` — add monitor endpoints

The URLMonitor already exists. Wire it so users can create URL watches from the UI:

**New API endpoints:**
```
POST /api/monitor          — start watching a URL
GET  /api/monitors         — list active monitors
DELETE /api/monitor/:id    — stop watching a URL
```

**POST /api/monitor request body:**
```json
{
  "name": "Ziloss CRM GitHub — new issues",
  "url": "https://github.com/jeremylerwick-max/ziloss-crm-private/issues",
  "check_interval_minutes": 60,
  "notify_on_change": "When new issues appear, summarize them and send me a Slack message"
}
```

When a change is detected:
1. Log the change diff
2. Submit as a goal to the planner: "{notify_on_change}\n\nChange detected:\n{diff}"
3. Orchestrator executes and sends Slack notification

Add monitors to the SchedulePanel as a second tab: "Watches"

**Checkpoint:** Create a URL watch. Manually trigger a check. Confirm Slack notification fires.

---

### PHASE 5: Slack Notifications for Scheduled Results

File: `internal/gateway/slack.go` or wherever Slack messaging lives

When a scheduled job completes, send a formatted Slack message to Jeremy:

```
🤖 *Scheduled: Morning AI News Briefing*
_Ran at 8:00 AM • 3 tasks • 47 seconds_

Here's your AI news briefing for Feb 27, 2026:

1. **Anthropic releases Claude 4** — major capability jump...
2. **OpenAI launches GPT-5** — benchmark results show...
3. **Google DeepMind announces Gemini Ultra 2**...

📄 Full report saved: `reports/ai_news_2026-02-27.md`
[View in ZBOT UI](http://localhost:18790)
```

Format:
- Bold job name at top
- Run time, task count, duration
- Summarized output (max 500 chars from workflow result)
- Link to any files created
- Link to ZBOT UI

**Checkpoint:** Run a scheduled job. Formatted Slack message arrives with results.

---

## Definition of Done

1. `go build ./...` passes clean
2. Scheduler runner starts and logs "scheduler runner started — checking every 30s"
3. "schedule:" command in CommandBar opens confirmation modal
4. Jobs save to DB and fire at scheduled time
5. Schedule panel shows all jobs with status, next run, last run
6. Run Now works
7. Pause/Resume works
8. URL monitors work with Slack notification on change
9. Completed scheduled jobs send formatted Slack messages
10. Manual test: "schedule: every minute test — research what day of the week it is and write a one-line note" → fires after 60 seconds → Slack notification arrives → file created in workspace

---

## Final Git Commit

```bash
cd ~/Desktop/zbot
git add -A
git commit -m "Sprint 14: Proactive scheduling — ZBOT works without being asked

- scheduler/runner.go: connects scheduler to planner+orchestrator
- zbot_scheduled_jobs: DB table with cron, status, run tracking
- CommandBar.tsx: schedule: prefix → confirmation modal → job creation
- POST/GET /api/schedule endpoints + pause/resume/delete/run-now
- SchedulePanel.tsx: manage all scheduled tasks and URL watches
- URL monitoring wired to planner with Slack notification on change
- Slack: formatted job completion notifications with results + file links"

git tag -a v1.14.0 -m "Sprint 14: Proactive scheduling"
git push origin main
git push origin v1.14.0
```

---

## Important Notes

- ParseSchedule() in nlparse.go already calls Claude to convert natural language to cron — use it as-is
- The cron check interval is 30s — jobs with per-minute frequency are fine, per-second is not supported
- Missed run recovery: fire ONCE if overdue, then update lastRun — never fire multiple times
- Slack notification: use the existing Slack client in wire.go — Jeremy's user ID is in GCP Secret Manager as "zbot-allowed-user-id"
- go build ./... must pass after every phase before moving to the next
- All secrets come from GCP Secret Manager — never hardcode credentials
