# ZBOT Sprint — Coworker Mission Brief
## Objective: UI Polish + Code Mode + Thalamus Backend

You are working on ZBOT, a self-hosted AI agent.
Codebase: ~/Desktop/Projects/zbot
Public repo: https://github.com/ziloss-tech/zbot
Architecture doc: docs/ARCHITECTURE.md (READ THIS FIRST)

## Current State (v0.1 — just shipped)
- Full agentic chat via /api/chat/stream (Claude Sonnet 4.6)
- Event bus emitting structured events (turn_start, tool_called, tool_result, turn_complete)
- SSE endpoint at /api/events/:sessionID streaming events to frontend
- useEventBus React hook consuming events
- Live activity indicator showing tool calls in real-time
- In-session conversation history (20 exchanges)
- Thalamus pane exists in frontend (ThalamusPane.tsx) but uses fake queries through /api/chat
- Brain-region naming: Cortex, Hippocampus, Thalamus, Amygdala, Cerebellum
- No Postgres connected (running in degraded mode — memory uses in-memory store)

## Tech Stack
- Backend: Go 1.26, hexagonal architecture (ports.go defines all interfaces)
- Frontend: React + TypeScript + Tailwind + Framer Motion + Vite
- LLM: Claude Sonnet 4.6 via Anthropic API
- Build: `cd internal/webui/frontend && npx vite build` then `go build -o zbot-bin ./cmd/zbot`
- Run: `set -a && source .env && set +a && ./zbot-bin`
- Test: `go test ./...`

## Tasks (in priority order)

### TASK 1: Thalamus Backend Endpoint
Create POST /api/thalamus with:
- Accepts: { "question": "why did cortex search for that?", "session_id": "web-chat" }
- Reads last 20 events from eventBus.Recent(sessionID, 20)
- Builds a compact event summary (~500 tokens)
- Calls Claude Haiku (cheap) with the Thalamus system prompt from docs/ARCHITECTURE.md
- Returns: { "reply": "Cortex searched because..." }
- Wire to ThalamusPane.tsx (replace the current /api/chat hack)
- DO NOT send full Cortex context — only the event summary. This is what keeps Thalamus cheap.

### TASK 2: Code Mode File Tree
When Cortex calls read_file or write_file tools, emit file_read/file_write events on the event bus.
- Update internal/tools/filesystem.go to emit events via the event bus
- The event bus needs to be accessible from tools — either inject it or use a package-level singleton
- Create FileTreePane.tsx component that:
  - Subscribes to file_read/file_write events
  - Builds a tree view of touched files
  - Highlights the currently active file
  - Shows diffs for write_file events (before/after)

### TASK 3: Metrics That Update
The MetricsStrip shows WORKFLOWS 0 | TASKS 0/0 | TOKENS 0 | COST $0.00 because there's no Postgres.
- Track tokens + cost in-memory from turn_complete events on the event bus
- Update MetricsStrip to read from event bus state instead of /api/metrics
- Show per-session token count and running cost

### TASK 4: Chat UX Polish
- The "CORTEX" event strip at the top (→ Turn started → Loaded 1 memories → Turn complete) should auto-hide after 3 seconds when the turn completes
- Messages should render markdown (bold, code blocks, headers, links)
- Add a "clear conversation" button that resets both the UI messages and the server-side conversation history
- The Thalamus tab should only appear when Cortex is working (auto-hide when idle)

## Definition of Done
1. `go build ./...` passes clean
2. `go test ./...` all pass
3. `npx vite build` succeeds
4. Thalamus endpoint returns real event-aware responses (not just forwarding to Cortex)
5. File tree pane appears when file tools are used
6. Metrics show running token/cost count
7. No regressions — existing chat + streaming + event bus still work

## Architecture Rules
- All interfaces in internal/agent/ports.go — add new ones there
- Tools don't import agent package directly — use interfaces
- Events go through the EventBus interface, never direct channel access
- Frontend components read from useEventBus hook, not polling
- System prompts go in internal/prompts/ as Go string constants
- Never hardcode API keys — everything through SecretsManager

## Files to Read First
1. docs/ARCHITECTURE.md — the full cognitive loop spec
2. internal/agent/ports.go — all interfaces
3. internal/agent/agent.go — the agent loop
4. internal/agent/eventbus.go — event bus implementation
5. internal/webui/chat_stream_handler.go — streaming endpoint
6. internal/webui/frontend/src/hooks/useEventBus.ts — frontend event hook
7. internal/webui/frontend/src/components/ThalamusPane.tsx — current Thalamus UI
8. internal/webui/frontend/src/components/ChatPane.tsx — main chat UI
