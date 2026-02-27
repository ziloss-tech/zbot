# ZBOT Sprint 11 — Dual Brain Command Center
**"Watch two AIs think in real time"**

---

## The Vision

A split-screen command center where you watch GPT-4o plan and Claude execute simultaneously — two AI minds working in parallel, fully visible. You issue a command, GPT-4o's thoughts stream in on the left in real time as it decomposes the goal, and Claude's execution streams on the right as it works through each task. Multiple plans can run at once. Everything persists in Postgres. The UI is a dark, cinematic ops dashboard — not a chat bubble interface.

**This does not exist anywhere else. This is new.**

---

## Aesthetic Direction

**Theme: Dark Ops / Mission Control**
- Near-black background (#0a0b0d) with subtle noise texture
- Two primary panel colors: GPT-4o gets cool blue-violet (#6366f1 accent), Claude gets warm amber (#f59e0b accent)
- Monospace font for streaming AI output (JetBrains Mono or Geist Mono)
- Display font for headers: Syne or DM Serif Display — architectural, serious
- Animated token-by-token streaming with a blinking cursor
- Subtle scanline effect on active panels
- Status badges pulse when active
- Task dependency graph renders as a live DAG (directed acyclic graph) between the two panels
- Smooth transitions — panels slide, tokens fade in, status changes animate

**The one unforgettable thing:** When GPT-4o finishes planning and hands off to Claude, there's a brief "handoff" animation — a glowing line traces from the left panel to the right, task cards materialize on the right, and Claude's cursor appears and starts typing. Nobody has ever seen this.

---

## Architecture Overview

```
Browser (React)
    │
    ├── SSE /api/stream/planner     ← GPT-4o token stream
    ├── SSE /api/stream/executor    ← Claude execution stream (per task)
    ├── GET /api/workflows          ← all active workflows
    ├── GET /api/workflow/:id       ← specific workflow + task status
    └── POST /api/plan              ← submit new goal

Go Backend (webui package)
    │
    ├── PlannerStreamHandler        ← calls GPT-4o with streaming, broadcasts tokens via SSE
    ├── ExecutorStreamHandler       ← tails Claude task output from Postgres, broadcasts via SSE
    ├── WorkflowsHandler            ← lists active/recent workflows
    └── WorkflowDetailHandler       ← task graph + statuses

Postgres
    ├── zbot_workflows              ← workflow state
    ├── zbot_tasks                  ← task state + output (add output column)
    └── zbot_stream_events          ← NEW: append-only event log for SSE replay
```

---

## New Database Columns Needed

```sql
-- Store Claude's output per task
ALTER TABLE zbot_tasks ADD COLUMN IF NOT EXISTS output TEXT NOT NULL DEFAULT '';
ALTER TABLE zbot_tasks ADD COLUMN IF NOT EXISTS started_at TIMESTAMPTZ;
ALTER TABLE zbot_tasks ADD COLUMN IF NOT EXISTS finished_at TIMESTAMPTZ;
ALTER TABLE zbot_tasks ADD COLUMN IF NOT EXISTS error TEXT NOT NULL DEFAULT '';

-- SSE event log for replay on reconnect
CREATE TABLE IF NOT EXISTS zbot_stream_events (
    id          BIGSERIAL PRIMARY KEY,
    workflow_id TEXT NOT NULL,
    task_id     TEXT,
    source      TEXT NOT NULL, -- 'planner' | 'executor'
    event_type  TEXT NOT NULL, -- 'token' | 'status' | 'handoff' | 'complete' | 'error'
    payload     TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_stream_events_workflow ON zbot_stream_events(workflow_id, id);
```

---

## Phase 1 — Backend SSE + Streaming Planner (Day 1 AM)

### 1.1 Upgrade the Planner to Stream

Currently `planner.Plan()` calls GPT-4o and waits for the full response. Upgrade it to stream tokens in real time via OpenAI's streaming API.

**File:** `internal/planner/planner.go`

Add a new method:
```go
// PlanStream calls GPT-4o with streaming and sends tokens to the provided channel.
// When planning is complete, it parses the full JSON and returns the TaskGraph.
func (p *Planner) PlanStream(ctx context.Context, goal string, tokens chan<- string) (*TaskGraph, error)
```

Use `client.CreateChatCompletionStream()` from the go-openai SDK. Each delta chunk gets sent to `tokens` channel. After stream completes, parse the accumulated JSON into TaskGraph.

### 1.2 SSE Hub

Create `internal/webui/hub.go` — a pub/sub hub for broadcasting SSE events to connected browser clients.

```go
type Hub struct {
    mu          sync.RWMutex
    subscribers map[string][]chan Event  // key: workflow_id
}

type Event struct {
    WorkflowID string
    TaskID     string
    Source     string // "planner" | "executor"
    Type       string // "token" | "status" | "handoff" | "complete" | "error"
    Payload    string
}

func (h *Hub) Subscribe(workflowID string) (<-chan Event, func())
func (h *Hub) Publish(e Event)
```

### 1.3 SSE HTTP Endpoints

**File:** `internal/webui/handlers.go`

```
GET /api/stream/:workflowID
```
- Sets headers: Content-Type: text/event-stream, Cache-Control: no-cache, Connection: keep-alive
- Subscribes to hub for workflowID
- Streams events as SSE: data: {json}\n\n
- On reconnect: replays last 100 events from zbot_stream_events before live stream
- Ping every 15s to keep connection alive

```
POST /api/plan
Body: { "goal": "..." }
```
- Generates workflowID immediately, returns it
- Launches goroutine: PlanStream → broadcast planner tokens → when done, Submit to orchestrator → broadcast handoff event

```
GET /api/workflows
```
- Returns last 20 workflows with task counts and status summary

```
GET /api/workflow/:id
```
- Returns full workflow: all tasks, statuses, outputs, timing

### 1.4 Wire Claude Task Output Back to SSE

In `internal/workflow/orchestrator.go`, when a task completes (or fails), write the output back to `zbot_tasks.output` and publish an event to the hub.

The executor's ---TASK_COMPLETE--- block should be parsed and stored cleanly.

**⛔ STOP AND TEST — Phase 1 Checkpoint**
- `curl -N http://localhost:18790/api/stream/test` should hold connection open
- `curl -X POST http://localhost:18790/api/plan -d '{"goal":"test"}'` should return a workflowID and you should see tokens streaming in the curl SSE connection
- Check Postgres: `SELECT * FROM zbot_stream_events LIMIT 10;` — events should be persisted

---

## Phase 2 — React Frontend Shell (Day 1 PM)

### Stack
- React 18 + Vite (served by Go's embed.FS — single binary)
- Tailwind CSS (JIT, dark mode only)
- Framer Motion for animations
- Recharts for any metrics
- Fonts: Syne (headings) + JetBrains Mono (AI output) via Google Fonts

### File Structure
```
internal/webui/
├── handlers.go          (Go HTTP handlers)
├── hub.go               (SSE pub/sub)
├── embed.go             (go:embed frontend/dist)
└── frontend/
    ├── index.html
    ├── vite.config.ts
    ├── tailwind.config.ts
    └── src/
        ├── main.tsx
        ├── App.tsx
        ├── components/
        │   ├── CommandBar.tsx       ← the input at the top
        │   ├── PlannerPanel.tsx     ← left panel (GPT-4o)
        │   ├── ExecutorPanel.tsx    ← right panel (Claude)
        │   ├── TaskCard.tsx         ← individual task in executor panel
        │   ├── HandoffAnimation.tsx ← the connecting animation
        │   ├── WorkflowHistory.tsx  ← bottom drawer / sidebar
        │   └── StatusBadge.tsx      ← pulsing status indicators
        ├── hooks/
        │   ├── useSSE.ts            ← SSE connection + reconnect logic
        │   └── useWorkflow.ts       ← workflow state management
        └── lib/
            ├── api.ts               ← API client
            └── types.ts             ← TypeScript types
```

### Layout

```
┌─────────────────────────────────────────────────────────────────┐
│  ZBOT  ●●●  [active workflows: 2]          [history]  [settings]│
├─────────────────────────────────────────────────────────────────┤
│ ▶ plan: [_________________________________________________] [⏎] │  ← Command Bar
├──────────────────────────────┬──────────────────────────────────┤
│  🧠 GPT-4o  PLANNING         │  ⚡ Claude  EXECUTING            │
│  ─────────────────────────── │  ─────────────────────────────── │
│                              │                                  │
│  "Research GoHighLevel       │  ┌─ task-1  ✓ done ─────────┐   │
│  competitors..."             │  │ Identified: HubSpot,      │   │
│                              │  │ ActiveCampaign, Keap      │   │
│  Interrogating goal...       │  └───────────────────────────┘   │
│  ├ What is "top 3"?          │                                  │
│  ├ By what metric?           │  ┌─ task-2  ⟳ running ───────┐   │
│  └ What output format?       │  │ Fetching HubSpot pricing   │   │
│                              │  │ page...                    │   │
│  Plan:                       │  │ ▌                          │   │
│  ✓ task-1: Identify (∥)      │  └───────────────────────────┘   │
│  ✓ task-2: Detail (→task-1)  │                                  │
│  ✓ task-3: Report (→task-2)  │  ┌─ task-3  ◌ waiting ───────┐   │
│                              │  │ Depends on task-2          │   │
│  ──── handoff ────────────▶  │  └───────────────────────────┘   │
│                              │                                  │
│  [workflow: 65a6ae78]        │  [0 / 3 complete]  [cancel]      │
├──────────────────────────────┴──────────────────────────────────┤
│  Recent: [65a6ae78 ✓] [3fa8bc12 ⟳] [d91e4421 ✗]               │  ← History Bar
└─────────────────────────────────────────────────────────────────┘
```

### Key UI Behaviors

**Command Bar**
- Full-width input at the top, monospace font, dark with amber cursor
- Prefix auto-detected: `plan:` routes to dual-panel, plain text goes to quick chat mode
- Submit with Enter or click — immediately creates workflow, both panels activate
- Shows typing indicator while GPT-4o is thinking

**Planner Panel (Left — GPT-4o)**
- Token-by-token streaming with blinking cursor at end
- Text renders with syntax-aware coloring: task titles in blue-violet, task IDs in muted, instructions in white
- When plan is complete: task list renders with dependency arrows
- "Handoff" trigger: when Submit is called, a glowing line animates from left panel to right

**Executor Panel (Right — Claude)**
- Task cards stack vertically
- Each card has: task ID badge, title, status badge (pulsing if running), output text streaming in
- Status colors: pending=muted, running=amber pulse, done=green, failed=red
- Dependency arrows shown between cards
- Output text is monospace, streams in token by token
- ---TASK_COMPLETE--- block is parsed and rendered as a clean summary card, not raw text
- Scroll follows active task automatically

**Handoff Animation**
- When GPT-4o finishes planning: left panel dims slightly, a glowing horizontal line traces from right edge of left panel to left edge of right panel (CSS animation ~800ms)
- Task cards on right materialize one by one with a stagger (150ms apart)
- Claude's cursor appears in the first task card and starts typing

**Multiple Workflows**
- Bottom bar shows recent workflow pills
- Click any pill to load that workflow into both panels
- Active workflows show a subtle pulse on their pill

**⛔ STOP AND TEST — Phase 2 Checkpoint**
- Open http://localhost:18790 — should see the dark split-screen layout
- Type a goal in the command bar, hit Enter
- Left panel should stream GPT-4o tokens in real time
- After ~5s the handoff animation should fire
- Task cards should appear on the right
- Claude's output should stream into the running task card
- Check on your phone — responsive layout should stack panels vertically on mobile

---

## Phase 3 — Polish, Animations, Multiple Simultaneous Plans (Day 2 AM)

### 3.1 Multiple Simultaneous Workflows

- Command bar always available even when a workflow is running
- Submitting a second `plan:` while one is running creates a new workflow ID
- Bottom history bar updates immediately with new workflow pill (pulsing)
- The two main panels switch to show the most recently active workflow
- Click history pill to focus on any workflow

### 3.2 Quick Chat Mode

For non-`plan:` messages (straight questions, quick tasks):
- No split screen — full width single response panel
- Streams Claude's response directly
- Same token streaming, same visual treatment
- Auto-saves to memory
- Press `plan:` prefix to switch to dual-brain mode at any time

### 3.3 Metrics Strip

Thin strip between command bar and panels showing live stats:
```
[Active: 2 workflows]  [Tasks: 5/12 done]  [Tokens today: 48,320]  [Cost today: $0.87]
```
- Token count and cost pulled from zbot_audit_log in Postgres
- Updates every 10s

### 3.4 Keyboard Shortcuts
- Cmd+K — focus command bar from anywhere
- Cmd+1/2 — switch between active workflows
- Esc — collapse history drawer
- Cmd+Enter — submit plan

### 3.5 Failure States
- If a task fails: card turns red, error message shown, shake animation
- If planner fails: left panel shows error, handoff animation doesn't fire
- Network drop: SSE auto-reconnects with exponential backoff, shows reconnecting banner
- On reconnect: replays last events from Postgres so no state is lost

### 3.6 Output File Preview

When Claude writes a file to ~/zbot-workspace/:
- Task card shows a view file button
- Click opens a slide-in drawer on the right with the file contents rendered (markdown → HTML)
- Code blocks get syntax highlighting

**⛔ STOP AND TEST — Phase 3 Checkpoint**
- Submit two plans back-to-back without waiting — both should run simultaneously
- Kill the browser tab and reopen — current workflow state should restore from Postgres
- Try quick chat (no plan: prefix) — full-width response should stream
- Check metrics strip is updating
- Trigger a failure (ask Claude to do something impossible) — red card + error should appear

---

## Phase 4 — The GPT-4o Critic Loop (Day 2 PM)

This wires the GPTCriticSystem prompt that already exists in internal/prompts/gpt_prompts.go but is currently unused.

### Flow
```
Claude completes task
    ↓
Write output to zbot_tasks.output
    ↓
Send to GPT-4o Critic (GPTCriticSystem prompt)
    ↓
GPT-4o returns: { verdict: "pass"|"fail"|"partial", issues: [...] }
    ↓
if pass:  mark task done, move to next
if fail:  requeue task with corrected instruction (max 1 retry)
if partial: mark done with warning badge, note issues
    ↓
Broadcast critic verdict via SSE
```

### UI for Critic
- After Claude's task card shows TASK_COMPLETE, a small "🔍 GPT-4o reviewing..." badge appears
- 1-3 seconds later: badge updates to ✓ Passed or ⚠ Partial or ✗ Retry
- If retry: card animates back to "running" state with a "GPT-4o requested retry" note
- Issues list shown as small warning pills on the card

**⛔ STOP AND TEST — Phase 4 Checkpoint**
- Run a plan and watch the critic badge appear on each completed task
- Deliberately give Claude a bad instruction to trigger a fail/retry
- Confirm the retry runs and card state resets correctly

---

## Phase 5 — Go Binary Embed + Deployment (Day 2 Evening)

### Embed Frontend into Go Binary

```go
// internal/webui/embed.go
//go:embed frontend/dist
var frontendFS embed.FS
```

Vite builds to frontend/dist/, Go embeds it, serves via http.FileServer. Single binary with full UI — no separate frontend server.

### Build Script

```bash
#!/bin/bash
# scripts/build-ui.sh
cd internal/webui/frontend
npm install
npm run build
cd ../../..
go build -o zbot ./cmd/zbot/
echo "✓ zbot binary with embedded UI ready"
```

### Makefile Targets
```makefile
ui:        cd internal/webui/frontend && npm run dev
ui-build:  bash scripts/build-ui.sh
dev:       ZBOT_ENV=development GCP_PROJECT=ziloss go run ./cmd/zbot/
run:       ./zbot
```

**⛔ STOP AND TEST — Phase 5 Checkpoint**
- make ui-build completes without errors
- ./zbot starts and serves UI at http://localhost:18790 from embedded binary
- No separate npm/node process needed

---

## Coworker (Opus) Instructions

**This is a Coworker session. Execute each phase sequentially. Do not skip phases. At each ⛔ STOP AND TEST checkpoint, pause and report what you tested and the result before proceeding.**

### Before starting:
1. Read ~/Desktop/zbot/internal/webui/handlers.go fully
2. Read ~/Desktop/zbot/internal/workflow/orchestrator.go fully
3. Read ~/Desktop/zbot/internal/planner/planner.go fully
4. Read ~/Desktop/zbot/go.mod for current dependencies
5. Run go build ./... to confirm clean build baseline

### Phase 1 instructions:
1. Run the migration SQL (add output/started_at/finished_at/error to zbot_tasks, create zbot_stream_events)
2. Add PlanStream method to planner (streaming GPT-4o via go-openai SDK)
3. Create internal/webui/hub.go
4. Update internal/webui/handlers.go with SSE endpoints
5. Wire task output writing in orchestrator
6. Run go build ./... — must be clean
7. CHECKPOINT: test SSE curl commands as specified above

### Phase 2 instructions:
1. Scaffold the React/Vite project in internal/webui/frontend/
2. Install dependencies: react, react-dom, vite, tailwindcss, framer-motion, @types/react
3. Build component tree top-down: App → CommandBar → PlannerPanel → ExecutorPanel → TaskCard
4. Implement useSSE hook first — everything depends on it
5. Wire up API calls
6. npm run dev during development, proxy /api to Go on port 18790
7. CHECKPOINT: visual test in browser as specified above

### Phase 3-5 instructions:
Follow each phase in order. At each checkpoint, test every item in the list. Report pass/fail for each.

### Style rules:
- No inline styles — Tailwind only
- Dark mode only — no light theme
- All animations via Framer Motion (React)
- TypeScript strict mode — no any
- No console.log in production code

### Error handling rules:
- Every fetch call has a try/catch
- SSE disconnects auto-reconnect with exponential backoff (1s, 2s, 4s, max 30s)
- Failed tasks show their error clearly — never silently swallow errors

---

## Definition of Done

- [ ] Submitting `plan: <goal>` shows GPT-4o tokens streaming in left panel
- [ ] Handoff animation fires when plan is complete
- [ ] Claude task cards appear in right panel and stream output
- [ ] Two simultaneous workflows run without interfering
- [ ] Reconnecting browser restores state from Postgres
- [ ] GPT-4o critic reviews each completed task (Phase 4)
- [ ] Embedded into Go binary — single file deployment
- [ ] Works on mobile (stacked layout)
- [ ] Metrics strip shows live token/cost counts

---

## Estimated Timeline

| Phase | Work | Time |
|-------|------|------|
| 1 | Backend SSE + streaming planner | 3h |
| 2 | React frontend shell | 4h |
| 3 | Polish + multi-workflow + quick chat | 3h |
| 4 | GPT-4o critic loop | 2h |
| 5 | Binary embed + deployment | 1h |
| Total | | ~13h (1.5 days with Opus) |
