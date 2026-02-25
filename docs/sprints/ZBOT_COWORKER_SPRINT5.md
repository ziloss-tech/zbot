# ZBOT Sprint 5 — Coworker Mission Brief
## Objective: Workflow Engine — Multi-Step Autonomous Tasks

You are working on ZBOT, a personal AI agent for Jeremy Lerwick (CEO, Ziloss Technologies).
The codebase is at: ~/Desktop/zbot
GitHub: https://github.com/jeremylerwick-max/zbot (private)
GCP Project: ziloss (project number: 203743871797)

ZBOT can scrape any website reliably. Sprint 4 is done.
Your job is to make ZBOT run multi-step parallel workflows that survive restarts.

---

## Current State (Sprint 4 Complete)

- Real Claude responses via Slack ✅
- Cross-session memory via pgvector ✅
- Vision: images + PDFs ✅
- Anti-block web scraper ✅
- go build ./... passes clean ✅

### Scaffolding already exists:
- internal/workflow/orchestrator.go — scaffold only, not yet functional
- agent/ports.go — WorkflowStore, DataStore interfaces already defined
- agent/ports.go — Task, TaskStatus types already defined

---

## Sprint 5 Tasks — Complete ALL of These

### TASK 1: Database Schema for Workflows

Create: migrations/002_workflows.sql

```sql
CREATE TABLE IF NOT EXISTS zbot_workflows (
    id          TEXT PRIMARY KEY,
    status      TEXT NOT NULL DEFAULT 'running', -- running, done, failed, canceled
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS zbot_tasks (
    id           TEXT PRIMARY KEY,
    workflow_id  TEXT NOT NULL REFERENCES zbot_workflows(id),
    step         INTEGER NOT NULL,
    name         TEXT NOT NULL,
    instruction  TEXT NOT NULL,
    status       TEXT NOT NULL DEFAULT 'pending', -- pending, running, done, failed, canceled
    depends_on   TEXT[] NOT NULL DEFAULT '{}',   -- array of task IDs
    input_ref    TEXT,   -- key in zbot_data store
    output_ref   TEXT,   -- key in zbot_data store
    worker_id    TEXT,   -- which worker claimed this task
    error        TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS zbot_data (
    ref         TEXT PRIMARY KEY,
    payload     JSONB NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS zbot_tasks_workflow_idx ON zbot_tasks(workflow_id);
CREATE INDEX IF NOT EXISTS zbot_tasks_status_idx ON zbot_tasks(status);
```

Run this migration on startup from wire.go (same pattern as zbot_memories migration).

### TASK 2: WorkflowStore Postgres Adapter

Create: internal/workflow/pgstore.go

Implement agent.WorkflowStore interface:

```go
type PGWorkflowStore struct {
    db *pgxpool.Pool
}

func NewPGWorkflowStore(db *pgxpool.Pool) (*PGWorkflowStore, error)
```

Key implementation detail for ClaimNextTask — use SKIP LOCKED to prevent double-claiming:
```sql
UPDATE zbot_tasks SET status = 'running', worker_id = $1, updated_at = NOW()
WHERE id = (
    SELECT t.id FROM zbot_tasks t
    WHERE t.status = 'pending'
      AND NOT EXISTS (
          SELECT 1 FROM zbot_tasks dep
          WHERE dep.id = ANY(t.depends_on)
            AND dep.status != 'done'
      )
    ORDER BY t.step ASC, t.created_at ASC
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
RETURNING id, workflow_id, step, name, instruction, status, depends_on, input_ref, output_ref, created_at, updated_at
```

This is the critical piece — no external queue needed, Postgres handles concurrency.

### TASK 3: DataStore Postgres Adapter

Create: internal/workflow/datastore.go

Implement agent.DataStore interface using zbot_data table:

```go
type PGDataStore struct {
    db *pgxpool.Pool
}

// Put stores JSON data and returns a random ref key.
func (s *PGDataStore) Put(ctx context.Context, data any) (string, error) {
    ref := randomID() // use same crypto/rand helper
    // INSERT INTO zbot_data (ref, payload) VALUES ($1, $2)
    return ref, nil
}

// Get retrieves and unmarshals data by ref.
func (s *PGDataStore) Get(ctx context.Context, ref string, dest any) error

// Delete removes data by ref.
func (s *PGDataStore) Delete(ctx context.Context, ref string) error
```

### TASK 4: Real Workflow Orchestrator

Replace the scaffold in internal/workflow/orchestrator.go with a real implementation:

```go
type Orchestrator struct {
    store     agent.WorkflowStore
    data      agent.DataStore
    llm       agent.LLMClient
    agent     *agent.Agent
    workerN   int        // number of parallel workers (default 4)
    logger    *slog.Logger
}

func New(store agent.WorkflowStore, data agent.DataStore, llm agent.LLMClient, ag *agent.Agent, logger *slog.Logger) *Orchestrator

// Start launches the worker pool goroutines.
func (o *Orchestrator) Start(ctx context.Context)

// Submit decomposes a user instruction into tasks and creates a workflow.
func (o *Orchestrator) Submit(ctx context.Context, sessionID, instruction string) (workflowID string, err error)

// Status returns all tasks for a workflow.
func (o *Orchestrator) Status(ctx context.Context, workflowID string) ([]agent.Task, error)

// Cancel cancels all pending tasks in a workflow.
func (o *Orchestrator) Cancel(ctx context.Context, workflowID string) error
```

Worker loop:
```go
func (o *Orchestrator) runWorker(ctx context.Context, workerID string) {
    for {
        select {
        case <-ctx.Done():
            return
        default:
            task, err := o.store.ClaimNextTask(ctx, workerID)
            if err != nil || task == nil {
                time.Sleep(500 * time.Millisecond)
                continue
            }
            o.executeTask(ctx, task)
        }
    }
}
```

### TASK 5: Task Decomposition via LLM

In orchestrator.go, implement Submit():

Use Claude to decompose the instruction into a task graph:
```go
decompositionPrompt := fmt.Sprintf(`
You are a task planner. Decompose this instruction into concrete sequential or parallel tasks.
Respond with ONLY a JSON array of tasks. No explanation, no markdown.

Instruction: %s

JSON format:
[
  {"step": 1, "name": "short name", "instruction": "detailed instruction for an AI agent", "depends_on": []},
  {"step": 2, "name": "short name", "instruction": "detailed instruction", "depends_on": ["task-id-from-step-1"]},
]

Rules:
- Maximum 10 tasks
- Tasks with no dependencies run in parallel
- instruction must be self-contained (the worker won't have conversation context)
- Keep it practical — don't over-decompose simple tasks
`, instruction)
```

Parse the JSON response and create the workflow in the database.

### TASK 6: Workflow Status Reporting to Slack

Add a /workflow or /status command handler in the Slack gateway.

When a user types "/status <workflowID>", send back:
```
Workflow abc123 — 3/7 tasks complete
✅ Step 1: Research GoHighLevel pricing — done
✅ Step 2: Research Salesforce pricing — done  
🔄 Step 3: Research HubSpot pricing — running
⏳ Step 4: Compare pricing models — waiting
⏳ Step 5: Write comparison report — waiting
```

### TASK 7: Wire Orchestrator into ZBOT

In cmd/zbot/wire.go:

```go
workflowStore, _ := workflow.NewPGWorkflowStore(pgDB)
dataStore := workflow.NewPGDataStore(pgDB)
orch := workflow.New(workflowStore, dataStore, llmClient, ag, logger)
orch.Start(ctx) // launch worker pool in background
```

Detect workflow requests in the message handler:
If message starts with "/workflow " or contains "do all of this" or "research and compare" → submit to orchestrator.
Otherwise → normal single-turn agent.Run().

---

## Definition of Done

1. DM ZBOT: "/workflow Research 5 CRM competitors, summarize each, build a comparison table"
2. ZBOT responds: "Starting workflow abc123 — 7 tasks queued"
3. Tasks run in parallel where possible
4. DM ZBOT: "/status abc123" → see live task progress
5. 2-3 minutes later, ZBOT sends back a formatted markdown comparison table
6. Restart ZBOT mid-workflow → on restart, pending tasks are picked up and continue
7. go build ./... passes clean

---

## Git Commit

```bash
cd ~/Desktop/zbot
git add -A
git commit -m "Sprint 5: Workflow engine live — parallel tasks, Postgres queue, restart-safe

- migrations/002_workflows.sql: zbot_workflows, zbot_tasks, zbot_data tables
- internal/workflow/pgstore.go: WorkflowStore with SKIP LOCKED claim
- internal/workflow/datastore.go: JSONB DataStore
- internal/workflow/orchestrator.go: Real worker pool replacing scaffold
- cmd/zbot/wire.go: Orchestrator wired, workflow detection in handler
- ZBOT can now run multi-step parallel workflows that survive restarts"
git push origin main
```

## Important Notes

- Never put secrets in code. All secrets via GCP Secret Manager only.
- go build ./... must pass after every change.
- SKIP LOCKED is essential — without it, multiple workers will double-claim tasks.
- Worker goroutines must respect ctx.Done() — clean shutdown on SIGTERM.
- Task decomposition JSON from Claude must be validated — handle malformed responses gracefully.
- Maximum 10 tasks per workflow to prevent runaway decomposition.
