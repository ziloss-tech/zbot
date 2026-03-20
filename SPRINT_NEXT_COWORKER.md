# ZBOT Sprint — Coworker Mission Brief
## Objective: Real-Time UI Polish + Code Mode Foundation

You are working on ZBOT, a self-hosted AI agent with brain-region architecture.
The codebase is at: ~/Desktop/Projects/zbot
GitHub: https://github.com/ziloss-tech/zbot (public, Apache 2.0)
GCP Project: your-project

## Current State (What Just Shipped — March 17-18, 2026)

- Full agentic chat via `/api/chat/stream` — SSE streaming ✅
- Event bus emitting structured events (turn_start, tool_called, tool_result, turn_complete) ✅
- SSE endpoint at `/api/events/:sessionID` ✅
- useEventBus React hook ✅
- Live Cortex status indicator in metrics bar ✅
- Live activity indicator in ChatPane (shows tool calls as they happen) ✅
- In-session conversation history (20 exchanges) ✅
- Thalamus pane (auto-opens, needs real backend) ✅
- Serper search provider (16x cheaper than Brave) ✅
- Brain-region naming throughout (Cortex, Thalamus, Hippocampus, Amygdala, Cerebellum) ✅
- Architecture doc at docs/ARCHITECTURE.md ✅

## Sprint Tasks — In Priority Order

### TASK 1: Fix Event Bus Display in ChatPane

The live event strip at the top of ChatPane uses `(window as any).__zbotEvents` which is hacky.
Refactor: pass event bus state down through props or React context. The ChatPane should receive
events from PaneManager or App, not from window globals.

Files to touch:
- `src/App.tsx` — remove window.__zbotEvents sync
- `src/components/PaneManager.tsx` — pass eventBus to ChatPane
- `src/components/ChatPane.tsx` — accept eventBus as prop, use it for the event strip

### TASK 2: Improve Chat UX Polish

Current issues to fix:
1. The "Cortex" label still shows the Anthropic "A" logo — replace with a brain icon
2. Message markdown rendering — the reply text shows raw markdown. Add a markdown renderer (react-markdown or similar)
3. The event strip stays visible after the turn completes — it should fade out 2s after turn_complete
4. Add a "Clear conversation" button to the chat header
5. Add token/cost display per message (the stream events include this data)

### TASK 3: Emit file_read/file_write Events from Tools

The event bus defines `EventFileRead` and `EventFileWrite` types but nothing emits them yet.
Update the file tools to emit these events so the UI can eventually show a live file tree.

Files to touch:
- `internal/tools/filesystem.go` — emit file_read/file_write events
- Need to pass the EventBus to tools (currently tools don't have access to it)
- Option A: Add EventBus to the tool constructor
- Option B: Add EventBus to the Execute context

Prefer Option B — add it to context so tools stay decoupled:
```go
type ctxKeyEventBus struct{}
func EventBusFromCtx(ctx context.Context) EventBus { ... }
```

Then in agent.go executeTools(), inject the event bus into the context before calling tool.Execute().

### TASK 4: MetricsStrip Live Updates

The MetricsStrip currently polls `/api/metrics` which returns zeros without Postgres.
Update it to also show data from the event bus:
- Tokens today: accumulate from turn_complete events
- Cost today: accumulate from turn_complete events
- Active workflows: from workflow events (if any)

This makes the metrics bar useful even without Postgres.

## Technical Notes

- Frontend: React + TypeScript + Tailwind + Framer Motion
- Build frontend: `cd internal/webui/frontend && npx vite build`
- Build Go: `go build -o zbot-bin ./cmd/zbot`
- Run: `set -a && source .env && set +a && ./zbot-bin`
- The `.env` file has ZBOT_ANTHROPIC_API_KEY and ZBOT_BRAVE_API_KEY
- Test: `go test ./...`
- No Postgres right now — all Postgres-dependent features gracefully degrade

## DO NOT

- Do NOT change the brain-region naming convention
- Do NOT modify the event bus interface in ports.go (it's stable)
- Do NOT add new dependencies without checking they're needed
- Do NOT modify the agent loop in agent.go (it's working correctly)
- Do NOT touch wire.go unless absolutely necessary
- Do NOT break the streaming endpoint — test after every change

## Definition of Done

1. `npx vite build` passes clean
2. `go build ./...` passes clean
3. `go test ./...` passes
4. Send a message in the UI → see live events → get response → events fade
5. Messages render markdown properly
6. Token/cost shows per message
7. git commit with descriptive message
