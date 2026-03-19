# ZBOT Sprint — Coworker Mission Brief
## Objective: UI Polish + Code Mode Panes

**Date:** 2026-03-18
**Assignee:** Coworker (Jean Claude)
**Repo:** ~/Desktop/Projects/zbot
**Branch:** public-release

---

## Context

ZBOT v0.1.0 is live at https://github.com/ziloss-tech/zbot.
The core agent loop works: chat → tools → memory → streaming events.
The brain-region architecture (Cortex, Thalamus, Hippocampus, Amygdala, Cerebellum) is established.
The event bus is emitting real-time events. The SSE endpoint streams them to the frontend.

## What's Working (verified 2026-03-18)

- Full agentic chat via /api/chat/stream (tool use, memory, event bus)
- In-session conversation history (20 exchanges per session)
- SSE event stream at /api/events/:sessionID (real-time tool_called, tool_result, turn_complete)
- Live Cortex status indicator in metrics bar (pulsing cyan when working, green when idle)
- Live activity indicator showing tool calls animating in real-time during chat
- Thalamus pane with dedicated /api/thalamus endpoint + ThalamusSystemPrompt (FIXED — no longer uses user-turn injection)
- Brain-region naming throughout UI (Cortex, Thalamus, Hippocampus, Amygdala)
- Streaming chat: POST /api/chat/stream runs agent in goroutine, relays events, sends final reply
- Serper search tool (alternative to Brave, 16x cheaper)
- Stall recovery pattern designed (see IDEAS/NEURAL_ARCHITECTURE_AI.md)
- Works without Postgres (graceful degradation — nil guards on metrics + SSE handlers)

## What's NOT Working / Known Issues

- Metrics strip shows all zeros (no Postgres — tokens/cost not tracked locally)
- Thalamus event display still reads from workflowState instead of real event bus
- No conversation persistence across restarts (in-memory only)
- Cortex sometimes asks permission before writing files (Claude safety training — see stall recovery pattern)
- The .env needs manual `set -a && source .env && set +a` before running (no dotenv loader)

## What Needs Work

### TASK 1: Emit file_read/file_write events from tools
**Files:** internal/tools/filesystem.go, internal/tools/filetools.go
**What:** When read_file or write_file tools execute, emit EventFileRead/EventFileWrite events to the event bus. The events should include the file path and size.
**Why:** The code mode file tree needs these events to know which files are being touched.
**How:** The tools don't currently have access to the event bus. Options:
  a. Pass the event bus to each tool at construction time (cleanest)
  b. Use a global event bus singleton (fastest to implement)
  c. Have the agent emit the events in executeTools after each tool result, based on tool name
**Recommendation:** Option C is simplest — add to agent.go executeTools, check if tool name is "read_file" or "write_file" and emit the corresponding event.

### TASK 2: Code mode file tree component
**File:** NEW: internal/webui/frontend/src/components/FileTreePane.tsx
**What:** A pane that shows the workspace file structure (~/zbot-workspace/) and highlights files that Cortex is reading/writing. Uses file_read/file_write events from the event bus.
**Design:** Dark theme, monospace font, indented tree. Files being read = cyan highlight. Files being written = amber highlight. Click a file to preview its content.
**API:** Use existing /api/workspace endpoint for the file list.

### TASK 3: Code preview component
**File:** NEW: internal/webui/frontend/src/components/CodePreviewPane.tsx
**What:** Shows the content of a selected file with syntax highlighting. When Cortex writes a file, the preview auto-updates.
**Design:** Line numbers, monospace, dark theme. Diffs highlighted in cyan (for lines Cortex added).

### TASK 4: Auto-split for code tasks
**File:** internal/webui/frontend/src/components/PaneManager.tsx
**What:** When Cortex emits file_read or file_write events, auto-split into file tree (18%) + code preview (40%) + chat (42%). Similar to how Thalamus auto-opens when cortexWorking triggers.

### TASK 5: Fix the Thalamus pane event display
**File:** internal/webui/frontend/src/components/ThalamusPane.tsx
**What:** Currently ThalamusPane fakes events by watching workflowState.toolCalls. Update it to read from the real event bus (either via useEventBus hook or by accepting events as props from PaneManager).

---

## Definition of Done

1. `go build ./...` passes clean
2. `npx vite build` passes clean
3. `go test ./...` passes
4. Send "write a hello world python script to my workspace" in the UI
5. File tree pane auto-opens showing ~/zbot-workspace/
6. Code preview shows the file content after it's written
7. Thalamus shows real event bus events (not faked)
8. Commit and push to public-release branch

---

## Architecture Reference

Read docs/ARCHITECTURE.md for the full cognitive loop.
Event types defined in internal/agent/ports.go.
Event bus implementation in internal/agent/eventbus.go.
SSE endpoint in internal/webui/events_handler.go.
Frontend event hook in internal/webui/frontend/src/hooks/useEventBus.ts.

## DO NOT

- Change the agent loop in agent.go (it works, don't touch it)
- Modify the streaming chat handler chat_stream_handler.go (it works)
- Modify the Thalamus backend handler thalamus_handler.go (it works, identity is correct)
- Change the system prompt in wire.go (the "execute immediately" approach was reverted intentionally)
- Add new npm dependencies without checking bundle size
- Break the existing chat or streaming functionality
- Push directly to main without testing build + tests

## IMPORTANT FILES ADDED RECENTLY (you may not know about these)

- internal/webui/events_handler.go — SSE endpoint for event bus
- internal/webui/chat_stream_handler.go — streaming chat endpoint  
- internal/webui/thalamus_handler.go — dedicated Thalamus API with its own system prompt
- internal/webui/frontend/src/hooks/useEventBus.ts — React hook for event bus SSE
- internal/agent/eventbus.go — in-memory event bus implementation
- internal/tools/search_serper.go — Serper search provider
