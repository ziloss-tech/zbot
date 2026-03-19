# ZBOT Sprint 3 — Stall Recovery, Persistence, Search, Polish

## Date: 2026-03-19
## Assignee: Coworker
## Branch: public-release → push to public (ziloss-tech/zbot main)

---

## CURRENT STATE (verified 2026-03-19)

ZBOT is running at localhost:18790 with:
- 5-stage cognitive loop (Frontal Lobe → Hippocampus → Cortex → Hippocampus enrichment → Thalamus verification)
- SSE event streaming with cognitive stage events visible in UI
- Thalamus auto-shows verification results
- Dotenv loader (no manual source .env needed)
- In-memory metrics (cost + turns track, tokens has a bug)
- 69 commits on public repo, Apache 2.0

### .env file (~/Desktop/Projects/zbot/.env)
```
ZBOT_ANTHROPIC_API_KEY=sk-ant-...  (set)
ZBOT_DATABASE_URL=postgres://...   (set but Postgres unreachable — that's fine)
ZBOT_BRAVE_API_KEY=...             (set)
ZBOT_ENV=development               (set)
```

Missing keys (web search disabled):
- ZBOT_SERPER_API_KEY — NOT SET. Serper is the preferred cheap search ($0.30/1K). Sign up at https://serper.dev (2,500 free queries). Add to .env.
- ZBOT_LLM_BASE_URL / ZBOT_LLM_MODEL / ZBOT_LLM_API_KEY — NOT SET. These are for Grok/Ollama budget stack. Not needed yet.

---

## TASKS (6 total, ordered by priority)

### TASK 1: Fix tokens_today metrics bug
**Priority: QUICK WIN (5 min fix)**
**File:** internal/webui/server.go

**The bug:** The metrics collector in `StartMetricsCollector()` type-asserts `evt.Detail["input_tokens"]` as `float64`, but the agent emits it as `int` in the `map[string]any`. Go does NOT auto-convert `int` to `float64` in type assertions — the assertion silently returns 0.

**The fix (line ~30 in StartMetricsCollector):**
```go
// BEFORE (broken — int asserted as float64 → 0):
inputTokens, _ := evt.Detail["input_tokens"].(float64)
outputTokens, _ := evt.Detail["output_tokens"].(float64)
costUSD, _ := evt.Detail["cost_usd"].(float64)

// AFTER (handle both int and float64):
var inputTokens, outputTokens int
if v, ok := evt.Detail["input_tokens"].(int); ok {
    inputTokens = v
} else if v, ok := evt.Detail["input_tokens"].(float64); ok {
    inputTokens = int(v)
}
if v, ok := evt.Detail["output_tokens"].(int); ok {
    outputTokens = v
} else if v, ok := evt.Detail["output_tokens"].(float64); ok {
    outputTokens = int(v)
}
var costUSD float64
if v, ok := evt.Detail["cost_usd"].(float64); ok {
    costUSD = v
} else if v, ok := evt.Detail["cost_usd"].(int); ok {
    costUSD = float64(v)
}
RecordTurn(inputTokens, outputTokens, costUSD)
```

**Verify:** After fix, send a message, then `curl http://localhost:18790/api/metrics` — `tokens_today` should be >0.

---

### TASK 2: Stall Recovery (Frontal Lobe override)
**Priority: HIGH — core differentiator**
**Files:** internal/agent/agent.go, internal/agent/cognitive.go

**The problem:** When Cortex (Claude) is asked to write a file or run code, it sometimes asks for permission instead of executing. This is Claude's safety training — not a bug. Example: "Write a Python script..." → Cortex says "I'll write a script that does X. Shall I proceed?" instead of calling write_file.

**The pattern (already designed, see ~/Desktop/IDEAS/NEURAL_ARCHITECTURE_AI.md):**

Detection: In `agent.go Run()`, after the LLM agentic loop finishes, check if:
1. Cortex's reply contains permission-asking patterns ("shall I proceed", "would you like me to", "I can write", "let me know if", "should I go ahead")
2. AND the Frontal Lobe plan says the task type is "code" or the plan has steps involving file writes
3. AND no tool calls were actually made (tools_used is empty)

If all 3 are true → Cortex stalled. Dispatch cheapLLM with a focused prompt:

```go
const stallRecoveryPrompt = `The user asked for a specific action and the primary AI hesitated instead of executing.

User's original request: %s
Frontal Lobe plan: %s (steps: %s)
Primary AI's response (stalled): %s

The user clearly wants this executed. Generate the appropriate tool call(s) to fulfill the request.
Do NOT ask for permission. Do NOT describe what you would do. Execute the tool call directly.`
```

Implementation location: Add a `recoverFromStall()` function in cognitive.go. Call it from agent.go Run() right after the main LLM loop, before the Thalamus verification stage:

```go
// After Cortex's main loop, before Thalamus verification:
if plan != nil && len(invokedTools) == 0 && output.Reply != "" {
    if isStalled(output.Reply, plan) {
        recovered := a.recoverFromStall(ctx, input, plan, output.Reply)
        if recovered != nil {
            output = recovered // replace stalled output with recovered output
        }
    }
}
```

`isStalled()` checks for permission-asking patterns + plan says code/write + no tools invoked.

**Important constraints:**
- Only attempt recovery ONCE — if the backup also stalls, return the original reply
- Only for "code" and task-type plans that explicitly need file writes
- Emit events: `EventStallDetected` and `EventStallRecovered` (add to ports.go)
- Log everything: the stall, the recovery prompt, the result
- If cheapLLM is nil, skip recovery (same as other cognitive stages)

---

### TASK 3: Persistent conversation history (SQLite)
**Priority: HIGH — conversations reset on restart is a bad UX**
**Files:** NEW: internal/memory/sqlite_history.go, cmd/zbot/wire.go

**Current state:** Conversation history is in-memory in wire.go:
```go
convHistory := make(map[string]*convEntry)  // line ~470
```
This resets on every restart. Sessions are keyed by "web-chat" (single session).

**Implementation:**
1. Create `internal/memory/sqlite_history.go`:
   - Uses `modernc.org/sqlite` (pure Go, no CGO) — already available as an indirect dependency
   - Table: `conversations (id TEXT PRIMARY KEY, session_id TEXT, role TEXT, content TEXT, created_at DATETIME)`
   - Methods: `SaveMessage(sessionID, role, content)`, `LoadHistory(sessionID, limit int) []agent.Message`, `ClearHistory(sessionID)`
   - DB file: `{workspaceRoot}/.cache/history.db`

2. Wire in cmd/zbot/wire.go:
   - Replace the in-memory `convHistory` map with SQLite calls
   - In the quickChat func: load history from SQLite instead of `history.msgs`
   - After agent.Run: save both user message and assistant reply to SQLite
   - Keep the maxHistory=40 limit (load last 40 messages)

3. Add endpoint: `DELETE /api/chat/history` to clear conversation (new message in UI can call this)

**Important:** Check if `modernc.org/sqlite` is already in go.sum. If not, `go get modernc.org/sqlite` first. Do NOT use `mattn/go-sqlite3` (requires CGO).

---

### TASK 4: Enable web search (Serper key)
**Priority: HIGH — search is disabled right now**

**Step 1:** Sign up at https://serper.dev — get a free API key (2,500 queries free)
**Step 2:** Add to .env: `ZBOT_SERPER_API_KEY=your_key_here`
**Step 3:** Restart ZBOT (dotenv loader will pick it up)
**Step 4:** Verify: `curl -s -X POST http://localhost:18790/api/chat/stream -H "Content-Type: application/json" -d '{"message": "Search the web for latest AI news"}'` — should show `tool_called: web_search` in the events

If serper.dev signup requires a credit card or is unavailable, the Brave key is already in .env as fallback — just verify web_search works. Check logs for "search provider: Serper" or "search provider: Brave".

---

### TASK 5: Show HN post
**Priority: MEDIUM — marketing**
**File:** NEW: docs/SHOW_HN.md

The previous draft was at /mnt/user-data/outputs/show_hn_post_v2.md but may not be accessible. Write a fresh one based on the CURRENT state (not the v0.1 state when it was drafted).

**Title:** Show HN: ZBOT — Self-hosted AI agent with brain-region cognitive architecture

**Key angles:**
1. The multi-stage cognitive loop is the differentiator — not "another AI wrapper"
2. Thalamus catches hallucinations before the user sees them (verified: caught fabricated casualty figures, invented Rust features, fake benchmarks)
3. Cost model: cognitive stages add ~$0.002/query (two Haiku calls) on top of the main Sonnet call
4. Budget stack: Grok 4.1 Fast ($0.20/M) + Serper ($0.30/1K) = ~$0.004/query total
5. Tagline: "Pro athlete performance for pickup game prices"
6. Apache 2.0, self-hosted, no vendor lock-in
7. Go backend (single binary, no Python deps), React frontend

**Format:**
```
Show HN: ZBOT — Self-hosted AI agent with brain-region cognitive architecture

[2-3 paragraph description]

Key points:
- [3-5 bullet points]

Repo: https://github.com/ziloss-tech/zbot
```

Save as docs/SHOW_HN.md and also prepare a comment with architecture details to post as the first reply.

---

### TASK 6: Code mode panes (file tree + code preview)
**Priority: LOW — nice to have after search + persistence work**
**Files:** See docs/SPRINT_NEXT.md Tasks 1-4 for full spec

Short version:
1. In agent.go `executeTools()`, emit `EventFileRead`/`EventFileWrite` events when read_file or write_file tools run. Include the file path in the event detail.
2. Create `FileTreePane.tsx` — shows workspace files, highlights on read/write events
3. Create `CodePreviewPane.tsx` — shows file content with line numbers, auto-updates on write
4. In `PaneManager.tsx` — auto-split when file events arrive (file tree 18% + code 40% + chat 42%)

The full spec is in docs/SPRINT_NEXT.md Tasks 1-4. Read that file for design details.

---

## BUILD + TEST + PUSH

```bash
cd ~/Desktop/Projects/zbot

# Build
go build -o zbot-bin ./cmd/zbot

# Frontend (if you touched .tsx files)
cd internal/webui/frontend && npx vite build && cd ../../..
go build -o zbot-bin ./cmd/zbot  # rebuild with new frontend

# Restart
pkill -f "zbot-bin"; sleep 2
nohup ./zbot-bin > /tmp/zbot.log 2>&1 &
sleep 3

# Test each task:

# Task 1 — metrics tokens
curl -s -X POST http://localhost:18790/api/chat/stream -H "Content-Type: application/json" -d '{"message":"hi"}'
curl -s http://localhost:18790/api/metrics  # tokens_today should be >0

# Task 2 — stall recovery
curl -s -X POST http://localhost:18790/api/chat/stream -H "Content-Type: application/json" -d '{"message":"Write a Python script to my workspace that calculates pi to 100 digits"}'
grep "stall\|recovery" /tmp/zbot.log  # may or may not trigger depending on Cortex behavior

# Task 3 — persistent history
curl -s -X POST http://localhost:18790/api/chat/stream -H "Content-Type: application/json" -d '{"message":"My name is Jeremy"}'
pkill -f "zbot-bin"; sleep 2; nohup ./zbot-bin > /tmp/zbot.log 2>&1 &; sleep 3
curl -s -X POST http://localhost:18790/api/chat/stream -H "Content-Type: application/json" -d '{"message":"What is my name?"}'
# Should answer "Jeremy" after restart

# Task 4 — web search
curl -s -X POST http://localhost:18790/api/chat/stream -H "Content-Type: application/json" -d '{"message":"Search the web for latest Rust news"}'
# Should show tool_called: web_search

# Task 5 — Show HN
cat docs/SHOW_HN.md  # should exist

# Task 6 — code mode (if implemented)
# Send a write_file task and check if file tree pane appears

# Sanity check
git diff --stat
git add -A && git commit -m "Sprint 3: stall recovery, SQLite history, search, metrics fix, Show HN, code mode"
git push public public-release:main
```

## DO NOT

- Change agent.go Run() cognitive stage ORDER (plan → memory → execute → enrich → verify)
- Change cognitive.go planTask or verifyReply prompts (they work)
- Change thalamus_handler.go (manual Thalamus Q&A works)
- Change ports.go event type definitions (except adding new stall events)
- Change the streaming chat handler structure
- Use mattn/go-sqlite3 (requires CGO) — use modernc.org/sqlite
- Add npm dependencies >50KB without justification
- Override env vars that are already set (dotenv loader respects existing vars)

## KEY FILES REFERENCE

```
cmd/zbot/wire.go                    — DI wiring, quickChat func with conversation history
internal/agent/agent.go             — 5-stage Run() loop
internal/agent/cognitive.go         — planTask, enrichMemory, verifyReply (ADD: recoverFromStall)
internal/agent/ports.go             — Event types (ADD: EventStallDetected, EventStallRecovered)
internal/webui/server.go            — StartMetricsCollector (FIX: type assertion bug)
internal/webui/api_handlers.go      — /api/metrics, RecordTurn()
internal/webui/chat_stream_handler.go — POST /api/chat/stream
internal/webui/thalamus_handler.go  — POST /api/thalamus
internal/tools/search_serper.go     — Serper search tool
internal/tools/filesystem.go        — read_file, write_file tools
docs/SPRINT_NEXT.md                 — Code mode pane specs (Task 6 reference)
~/Desktop/IDEAS/NEURAL_ARCHITECTURE_AI.md — Stall recovery pattern design
.env                                — Environment variables (ADD: ZBOT_SERPER_API_KEY)
```
