# COWORKER PROMPT — Heartbeat UI: Real-Time Activity Indicators

**Date:** 2026-03-25
**Repo:** ~/Desktop/Projects/zbot
**Branch:** Create `feat/heartbeat-ui` from `public-release`
**Priority:** This is the #1 UX issue — users can't tell if ZBOT is working or frozen.

---

## WHO YOU ARE

You are a senior React/TypeScript frontend engineer with Go backend experience. You're fixing the most critical UX problem in ZBOT: when the agent is working (thinking, searching, calling tools), the UI looks frozen. The user has no way to know if their request was received, what step ZBOT is on, or how long it will take.

---

## THE PROBLEM

ZBOT's backend emits structured events through an EventBus → SSE pipeline. The data IS flowing — `turn_start`, `tool_called`, `tool_result`, `turn_complete` events all stream to the browser. But the frontend doesn't display them prominently enough. When a user sends "find cyber trucks for sale in SLC", the UI looks dead for 10-30 seconds while Cortex thinks and calls tools.

---

## FIRST: READ THE CODEBASE

```bash
cd ~/Desktop/Projects/zbot

# Understand the event flow (backend → frontend)
cat internal/agent/eventbus.go
cat internal/agent/ports.go | head -80
cat internal/webui/events_handler.go
cat internal/webui/chat_stream_handler.go

# Current frontend components you'll modify
cat internal/webui/frontend/src/components/ChatPane.tsx
cat internal/webui/frontend/src/components/PaneManager.tsx
cat internal/webui/frontend/src/components/MetricsStrip.tsx
cat internal/webui/frontend/src/components/ThalamusPane.tsx
cat internal/webui/frontend/src/hooks/useEventBus.ts
cat internal/webui/frontend/src/hooks/useSSE.ts
cat internal/webui/frontend/src/lib/types.ts

# Styling reference
cat internal/webui/frontend/tailwind.config.ts
cat internal/webui/frontend/src/index.css
```

Confirm baseline builds:
```bash
go build ./...
cd internal/webui/frontend && npx vite build && cd ../../..
go test ./...
```

---

## WHAT TO BUILD

### COMPONENT 1: ActivityTimeline — The Persistent Activity Strip

**New file:** `internal/webui/frontend/src/components/ActivityTimeline.tsx`

A slim, always-visible strip below the chat input (or above it) that streams every action in real-time. This is the heartbeat — you always see what ZBOT is doing.

**Design:**
- Height: 32-40px, full width, fixed at bottom of chat area (above input bar)
- Background: rgba(0, 212, 255, 0.05) when active, transparent when idle
- Scrolls horizontally if entries overflow, newest on the right
- Each entry: timestamp + icon + short description
- Entries animate in from the right with a subtle slide

**Entries format:**
```
⏳ 23:43:51  Received: "find cyber trucks..."
🧠 23:43:51  Thinking... (Sonnet 4.6)
🔧 23:43:52  web_search("cyber truck for sale salt lake city")
📄 23:43:54  Got 10 results
🧠 23:43:54  Analyzing results...
🔧 23:43:55  fetch_url("https://www.cargurus.com/...")
📄 23:43:57  Page loaded (4.2KB)
✏️ 23:43:58  Writing response...
✅ 23:43:59  Done ($0.024, 7.5K tokens)
```

**Data source:** Subscribe to the SSE event stream via `useEventBus` hook. Map event types to display entries:
- `turn_start` → "Received: {goal}" with ⏳ icon
- `thinking` → "Thinking... ({model})" with 🧠 icon
- `tool_called` → "{tool_name}({args_summary})" with 🔧 icon
- `tool_result` → "Got results" or "Page loaded" with 📄 icon  
- `turn_complete` → "Done ({cost}, {tokens})" with ✅ icon
- `crawl_action` → "Clicked {grid_cell}" or "Navigated to {url}" with 🌐 icon
- `review_finding` → "Reviewer: {severity} — {description}" with 🔍 icon
- `error` → error message with ❌ icon

**State management:**
```tsx
interface TimelineEntry {
  id: string
  timestamp: number
  icon: string
  text: string
  type: 'received' | 'thinking' | 'tool' | 'result' | 'complete' | 'error' | 'crawl' | 'review'
}
```

Keep last 50 entries in state. Clear on new turn_start.

### COMPONENT 2: Enhanced Status Indicator

**Modify:** `internal/webui/frontend/src/components/MetricsStrip.tsx`

The current Cortex status dot is too small and subtle. Replace it with an obvious, animated indicator:

**Idle state:**
- Small green dot (8px) with text "Ready"
- Subtle pulse animation

**Thinking state:**
- Large pulsing cyan orb (20px) with animated glow
- Text: "Thinking..." in cyan, with elapsed time counting up
- The orb should have a breathing animation (scale 1.0 → 1.2 → 1.0)

**Tool calling state:**
- Amber orb (20px) with spin animation
- Text: "Calling {tool_name}..." in amber

**Streaming state:**
- Cyan orb with typing dots animation (• •• •••)
- Text: "Writing response..."

**Error state:**
- Red dot with text "Error"
- Auto-clears after 10 seconds

Implementation:
```tsx
type CortexStatus = 'idle' | 'thinking' | 'tool_calling' | 'streaming' | 'error'

// Derive status from event bus events:
// turn_start → thinking
// tool_called → tool_calling  
// token (streaming) → streaming
// turn_complete → idle
// error → error
```

### COMPONENT 3: Connection Badge

**Add to:** MetricsStrip or the top-right corner of the UI

A clear indicator of whether the browser is connected to ZBOT's SSE stream:

- **Connected:** Small green "Connected" badge (barely visible, not distracting)
- **Disconnected:** Red "Disconnected" badge that pulses (very visible)
- **Reconnecting:** Amber "Reconnecting..." with spinner

Implementation: Check the EventSource readyState in useEventBus hook. Expose a `connected` boolean. If SSE connection drops, show the badge prominently. Auto-reconnect with exponential backoff.

### COMPONENT 4: Typing Indicator in Chat

**Modify:** `internal/webui/frontend/src/components/ChatPane.tsx`

When ZBOT is generating a response (between `turn_start` and `turn_complete`), show a typing indicator at the bottom of the chat messages:

```tsx
// While cortexStatus !== 'idle', show:
<div className="flex items-center gap-2 px-4 py-2">
  <div className="flex gap-1">
    <span className="h-2 w-2 rounded-full bg-cyan-400 animate-bounce" style={{ animationDelay: '0ms' }} />
    <span className="h-2 w-2 rounded-full bg-cyan-400 animate-bounce" style={{ animationDelay: '150ms' }} />
    <span className="h-2 w-2 rounded-full bg-cyan-400 animate-bounce" style={{ animationDelay: '300ms' }} />
  </div>
  <span className="font-mono text-[10px] text-cyan-400/60">ZBOT is working...</span>
</div>
```

This should auto-scroll into view so the user always sees it.

### COMPONENT 5: Elapsed Time Counter

When ZBOT is working, show a live elapsed time counter somewhere visible (in the MetricsStrip or in the ActivityTimeline):

```
⏱ 3.2s
```

Starts counting on `turn_start`, stops on `turn_complete`. Shows seconds with one decimal. Gives the user a sense of "something is happening" even if no events are flowing for a moment.

---

## WIRING INTO EXISTING COMPONENTS

### ChatPane.tsx Changes
1. Import and render `ActivityTimeline` below the messages, above the input
2. Add typing indicator when `cortexStatus !== 'idle'`
3. Pass event bus events to ActivityTimeline

### PaneManager.tsx Changes
1. Lift the `cortexStatus` state up so ActivityTimeline and MetricsStrip can both access it
2. Or use a shared context/hook that derives status from the event bus

### MetricsStrip.tsx Changes
1. Replace the small status dot with the enhanced status indicator
2. Add the connection badge
3. Add elapsed time counter

### useEventBus.ts Changes
1. Export a `connected` boolean for the connection badge
2. Add reconnection logic with exponential backoff if not already present
3. Ensure all event types are properly typed and emitted

### App.tsx or Layout Changes
1. If needed, add a context provider for cortex status so all components can access it
2. The ActivityTimeline and MetricsStrip need to react to the same events

---

## STYLING GUIDELINES

Follow the existing ZBOT design system:
- Background: #0a0a1a (surface-950)
- Primary accent: #00d4ff (cyan) — for active/thinking states
- Secondary accent: #d97757 (anthropic amber) — for tool calling
- Success: emerald-400
- Error: red-400
- Auditor: #a78bfa (auditor purple)
- Font: JetBrains Mono for monospace, Inter for display
- Glass panels: bg-glass with border-white/[0.04]
- Animations: Use framer-motion (already installed) for entries, CSS for pulsing/breathing

---

## DEFINITION OF DONE

1. `npx vite build` passes clean
2. `go build ./...` still passes (no backend changes needed, but verify)
3. Start ZBOT locally (`cd ~/Desktop/Projects/zbot && set -a && source .env && set +a && ./zbot-run`)
4. Open http://localhost:18790
5. Send a message in the chat
6. **ActivityTimeline shows every step in real-time** — received, thinking, tool calls, results, done
7. **Status indicator is obvious** — large pulsing cyan orb when thinking, amber when calling tools
8. **Typing indicator visible** — bouncing dots in chat while ZBOT works
9. **Elapsed time counter** — shows seconds ticking while working
10. **Connection badge** — shows "Connected" (green) or "Disconnected" (red)
11. **The UI NEVER looks frozen** — there is always visual feedback within 500ms of any action
12. Commit all changes to `feat/heartbeat-ui` branch

---

## DO NOT

- Modify any Go backend files (the event pipeline already works perfectly)
- Change the event bus or SSE handler (data is already flowing correctly)
- Add new npm dependencies without checking bundle size (framer-motion is already installed)
- Break existing pane functionality (Chat, Auditor, Browser, CrawlLog)
- Remove or resize the input bar, command bar, or sidebar
- Make the ActivityTimeline take up too much vertical space (32-40px max)

## IMPORTANT: The Backend Already Works

I verified this by curling the streaming endpoint:
```bash
curl -s -X POST http://localhost:18790/api/chat/stream \
  -H "Content-Type: application/json" \
  -d '{"message":"ping"}'
```

Response includes: `turn_start` → `turn_complete` → `done` events with full metadata (cost, tokens, tools used). The SSE endpoint at `/api/events/{sessionID}` streams all events. The frontend just needs to DISPLAY them prominently.

## REPO LOCATION
~/Desktop/Projects/zbot
