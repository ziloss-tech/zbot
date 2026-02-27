# ZBOT Sprint 12 — Coworker Mission Brief
## Objective: Memory Wired Into Dual-Brain System

You are working on ZBOT, a personal AI agent for Jeremy Lerwick (CEO, Ziloss Technologies).
The codebase is at: ~/Desktop/zbot
GitHub: https://github.com/jeremylerwick-max/zbot (private)
GCP Project: ziloss (project number: 203743871797)

Sprint 11 is complete. The Dual Brain Command Center UI is live at http://localhost:18790.
GPT-4o plans on the left, Claude executes on the right, SSE streaming works end to end.
Your job is to wire cross-session memory into the dual-brain system so ZBOT remembers things forever.

---

## Current State (Sprint 11 Complete)

- Dual Brain Command Center UI live at http://localhost:18790 ✅
- GPT-4o planner (left panel) + Claude executor (right panel) ✅
- GPT-4o critic reviews each completed task ✅
- SSE token-by-token streaming for both models ✅
- Multi-workflow support ✅
- Skills: search, GHL, GitHub, Google Sheets, Email ✅
- Background service (launchd) — survives restarts ✅
- Grafana observability at https://grafana.ziloss.com ✅
- Memory package exists in internal/memory/ (pgvector + Vertex AI embeddings) ✅
- BUT: memory is NOT wired into the planner or orchestrator ❌
- ZBOT forgets everything between sessions ❌

---

## Architecture Overview

```
internal/memory/
  embedder.go     — Vertex AI text-embedding-004 (768 dims)
  store.go        — pgvector Store (table: zbot_memories, namespace: zbot)
  fallback.go     — InMemoryStore (used when Postgres unavailable)

Target integration:
  planner.go      — inject top-5 relevant memories into GPT-4o system prompt
  orchestrator.go — Claude has save_memory + search_memory tools
  webui/          — Memory panel in right sidebar showing recent memories
```

DB: 34.28.163.109, DB: ziloss_memory, Table: zbot_memories (already exists from Sprint 9)
GCP Secret: anthropic-api-key, vertex-ai-project = ziloss

---

## Sprint 12 Tasks — Complete ALL in Order

### PHASE 1: Wire Memory Into the Planner

File: `internal/planner/planner.go`

The planner creates GPT-4o system prompt to decompose goals into tasks.
Before calling GPT-4o, search memory for context relevant to the goal and inject it.

```go
// In planner.go, add MemoryStore field:
type Planner struct {
    llm    agent.LLMClient
    logger *slog.Logger
    memory agent.MemoryStore  // ADD THIS
}

// In Plan() and PlanStream(), before building the system prompt:
// 1. Search memory for top-5 facts relevant to the goal
// 2. If found, prepend them to the system prompt as:
//    "## Relevant Context From Memory\n{fact1}\n{fact2}..."
// 3. If memory unavailable, log a warning and continue without it
```

The `agent.MemoryStore` interface is already defined in `internal/agent/ports.go`.
Pass the memory store through from `wire.go` when constructing the planner.

**Checkpoint:** Build passes. Planner logs "memory: injected N facts" on each plan call.

---

### PHASE 2: Wire save_memory and search_memory Tools Into Orchestrator

File: `internal/workflow/orchestrator.go`
File: `internal/skills/memory/skill.go` (NEW)
File: `internal/skills/memory/tools.go` (NEW)

Create a memory skill with two tools:

**save_memory tool:**
```go
Name: "save_memory"
Description: "Save an important fact to long-term memory. Use this when you learn something important about Jeremy, his business, preferences, or discover a useful insight during a task."
Input: { "fact": string, "category": string (optional: "preference", "business", "technical", "personal") }
Execute: call memoryStore.Save() with the fact text and current timestamp
Returns: "Memory saved: {fact}"
```

**search_memory tool:**
```go
Name: "search_memory"
Description: "Search long-term memory for facts relevant to a topic. Use this when Jeremy references something from the past or when context from previous sessions would help."
Input: { "query": string, "limit": int (default 5) }
Execute: call memoryStore.Search() with semantic similarity
Returns: formatted list of matching facts with timestamps
```

Register the memory skill in `wire.go` — it always registers (no secret required).
Wire the memory store into the orchestrator so Claude has access to these tools on every task.

**Checkpoint:** Build passes. Logs show "skill registered: memory".

---

### PHASE 3: Auto-Save on Workflow Completion

File: `internal/workflow/orchestrator.go`

When a workflow completes successfully, automatically extract and save key insights:

```go
// After all tasks complete:
// 1. Build a summary of what was accomplished
// 2. Call Claude (haiku — cheap) with:
//    "Extract 1-3 important facts from this workflow result worth remembering long-term.
//     Return JSON array of strings. Only include genuinely useful persistent facts.
//     If nothing worth saving, return empty array."
// 3. Save each extracted fact to memory with category "workflow_insight"
// 4. Log: "auto-saved N facts from workflow {id}"
```

This makes ZBOT progressively smarter — every completed workflow adds to its knowledge.

**Checkpoint:** Run a test plan. Check DB for new rows in zbot_memories after completion.

---

### PHASE 4: Memory Panel in the Web UI

File: `internal/webui/api_handlers.go` — add memory API endpoints
File: `internal/webui/frontend/src/components/MemoryPanel.tsx` (NEW)
File: `internal/webui/frontend/src/App.tsx` — add memory panel toggle

**New API endpoints:**
```
GET  /api/memories?q={query}&limit=20   — search/list memories
DELETE /api/memory/:id                  — delete a memory
```

**MemoryPanel component:**
- Slide-in panel from the right side (triggered by a brain icon button in the top nav)
- Search box at top — live search as you type (debounced 300ms)
- Each memory card shows: fact text, category badge, timestamp, delete button
- "Total memories: N" count at top
- Empty state: "No memories yet — ZBOT will start remembering things as you work"
- Dark ops theme matching the rest of the UI (#0a0b0d background, amber accent for memory)
- Animate in with Framer Motion (slide from right)

**Checkpoint:** Click brain icon → memory panel slides in. Memories visible and searchable.

---

### PHASE 5: Memory-Aware Quick Chat

File: `internal/webui/api_handlers.go` — update quick chat handler

The command bar already supports non-plan messages (quick chat mode).
Wire memory into quick chat so Claude has context from past sessions:

```go
// Before sending quick chat message to Claude:
// 1. Search memory for top-5 facts relevant to the message
// 2. Inject into system prompt: "## What You Remember About Jeremy\n{facts}"
// 3. After Claude responds, check if response contains a saveable fact
//    (use haiku to decide: "Does this conversation contain a fact worth saving? Y/N + fact")
// 4. If yes, auto-save it
```

**Checkpoint:** Tell ZBOT your favorite color in quick chat. Close browser. Reopen. Ask "what's my favorite color?" — it should know.

---

## Definition of Done

1. `go build ./...` passes clean
2. Memory skill registered at startup: "skill registered: memory"
3. Planner injects relevant memories into GPT-4o context
4. Claude can call save_memory and search_memory during task execution
5. Completed workflows auto-save key insights to memory
6. Memory panel opens in UI — memories visible and searchable
7. Quick chat is memory-aware across sessions
8. Manual test: tell ZBOT something in quick chat → close browser → reopen → ask about it → it knows

---

## Final Git Commit

```bash
cd ~/Desktop/zbot
git add -A
git commit -m "Sprint 12: Memory wired into dual-brain system

- internal/skills/memory/: save_memory + search_memory tools
- planner.go: memory context injection into GPT-4o system prompt
- orchestrator.go: memory tools available to Claude on every task
- orchestrator.go: auto-save workflow insights on completion
- webui: GET /api/memories, DELETE /api/memory/:id endpoints
- MemoryPanel.tsx: slide-in memory browser with search and delete
- Quick chat: memory-aware context injection + auto-save
- wire.go: memory skill registered"

git tag -a v1.12.0 -m "Sprint 12: Cross-session memory wired into dual-brain"
git push origin main
git push origin v1.12.0
```

---

## Important Notes

- Memory DB is at 34.28.163.109, table: zbot_memories, namespace must be "zbot"
- Vertex AI embedder uses text-embedding-004, 768 dimensions — already implemented in internal/memory/embedder.go
- If Vertex AI is unavailable, fall back to InMemoryStore (already implemented in internal/memory/fallback.go)
- Use claude-haiku-4-5-20251001 for the auto-save extraction — it's cheap and fast
- go build ./... must pass after every phase before moving to the next
- All secrets come from GCP Secret Manager — never hardcode credentials
