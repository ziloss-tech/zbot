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

## What's Working

- Full agentic chat via /api/chat/stream (tool use, memory, event bus)
- In-session conversation history (20 exchanges)
- SSE event stream at /api/events/:sessionID
- Live Cortex status indicator in metrics bar
- Live activity indicator showing tool calls as they happen
- Thalamus pane (auto-opens, basic event display)
- Brain-region naming throughout UI

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
- Modify the streaming chat handler (it works)
- Add new npm dependencies without checking bundle size
- Break the existing chat functionality
