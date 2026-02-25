# SPRINT 2 — Memory System Live
**Week of Feb 25 – Mar 3, 2026**
**Goal:** Tell ZBOT your name in session 1. Close Slack. Open new session. It knows your name.

---

## ⚡ CRITICAL: What's Already Done (Don't Rebuild)

Sprint 1 shipped more than expected. The following Sprint 2 items are **fully implemented** — do not touch them:

| Item | File | Status |
|------|------|--------|
| Vertex AI `text-embedding-004` embedder | `internal/memory/embedder.go` | ✅ DONE |
| `zbot_memories` table + HNSW + FTS indexes | `internal/memory/store.go` → `migrate()` | ✅ DONE |
| `save_memory` tool | `internal/tools/filetools.go` → `MemorySaveTool` | ✅ DONE |
| Auto memory injection every turn | `internal/agent/agent.go` → `buildSystemPrompt()` injects top-k facts | ✅ DONE |
| Hybrid BM25 + vector search with time decay | `internal/memory/store.go` → `Search()` | ✅ DONE |
| In-memory fallback when Postgres is down | `internal/memory/fallback.go` | ✅ DONE |
| Postgres connected + schema migrated on boot | `cmd/zbot/wire.go` | ✅ DONE |

**The memory pipeline is live.** `save_memory` → pgvector → injected on next turn. The core loop works.

---

## 🔧 What Remains for Sprint 2

### S2-A: `search_memory` Tool
**File to create:** `internal/tools/memory.go`

Add a `SearchMemoryTool` that lets ZBOT explicitly query its own memory mid-conversation.
The agent should use this when the user asks "do you remember..." or "what do you know about...".

```go
type SearchMemoryTool struct{ store agent.MemoryStore }

func NewSearchMemoryTool(store agent.MemoryStore) *SearchMemoryTool
func (t *SearchMemoryTool) Name() string { return "search_memory" }
```

Tool schema:
```json
{
  "query": { "type": "string", "description": "What to search for in memory" },
  "limit": { "type": "integer", "description": "Max results (default 5)" }
}
```

Output: formatted list of matching facts with their source and age (e.g. "3 days ago").
Wire it into `cmd/zbot/wire.go` — add `memorySearch` alongside `memorySave` in the agent constructor.

---

### S2-B: Wire AutoSave in the Turn Handler
**File to modify:** `cmd/zbot/wire.go`

`internal/memory/store.go` already has `AutoSave()` implemented — it's just not called.
After `ag.Run()` returns successfully, call it:

```go
output, err := ag.Run(ctx, input)
if err != nil {
    return "", fmt.Errorf("agent.Run: %w", err)
}

// Auto-save substantial agent responses to memory.
if pgStore, ok := memStore.(*memory.Store); ok {
    pgStore.AutoSave(ctx, sessionID, output.Reply)
}
```

This gives ZBOT persistent memory of its own responses without requiring explicit `save_memory` calls.

**Important:** The current `AutoSave` heuristic (save if > 200 chars) is intentionally simple.
Don't replace it with LLM classification — that's a Phase 2 upgrade, not Sprint 2.

---

### S2-C: Memory Namespace Isolation
**File to modify:** `internal/memory/store.go`

The `Store` struct has a `namespace` field already — it's just hardcoded to `"zbot"` in `New()`.
Wire it as a constructor parameter so ZBOT's memories never collide with the existing `mem0_memories_vertex` table:

```go
// New now accepts a namespace string.
func New(ctx context.Context, db *pgxpool.Pool, embedder Embedder, logger *slog.Logger, namespace string) (*Store, error)
```

Update `wire.go` to pass `"zbot"` explicitly. The table name (`zbot_memories`) should be derived from the namespace field rather than hardcoded throughout `store.go`. Replace all literal `"zbot_memories"` with `s.tableName()` where:

```go
func (s *Store) tableName() string {
    return s.namespace + "_memories"
}
```

This means a future `mem0` namespace would use `mem0_memories` — coexisting cleanly.

---

### S2-D: Memory Viewer CLI
**File to create:** `cmd/memcli/main.go`

A simple terminal tool for inspecting, searching, and deleting ZBOT memories.
This is for Jeremy to use directly — not exposed to Slack.

Commands:
```
memcli list [--limit N]          # list most recent N memories (default 20)
memcli search <query>            # semantic search
memcli delete <id>               # delete by ID
memcli stats                     # count, oldest, newest, namespace breakdown
```

Implementation notes:
- Connect to the same Postgres as ZBOT (same `connectPostgres()` helper)
- Use the same `memory.Store` (reuse, don't duplicate)
- Use `memory.VertexEmbedder` for search (graceful fallback to BM25-only if Vertex fails)
- Output as clean plaintext table (no fancy TUI needed)
- Build target: `go build ./cmd/memcli` → produces `./memcli` binary

---

## Architecture Reference

### Database (already live on GCP Cloud SQL `34.28.163.109`)
```sql
-- Already created and running:
CREATE TABLE zbot_memories (
    id         TEXT PRIMARY KEY,
    content    TEXT NOT NULL,
    source     TEXT NOT NULL DEFAULT 'conversation',  -- 'user', 'agent', 'research'
    tags       TEXT[] NOT NULL DEFAULT '{}',
    embedding  vector(768),  -- Vertex AI text-embedding-004
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
-- HNSW index: zbot_memories_embedding_idx (cosine, m=16, ef_construction=64)
-- GIN FTS index: zbot_memories_fts_idx
-- created_at index: zbot_memories_created_idx
```

### Memory Interface (do not change)
```go
// internal/agent/ports.go — MemoryStore is already correct
type MemoryStore interface {
    Save(ctx context.Context, fact Fact) error
    Search(ctx context.Context, query string, limit int) ([]Fact, error)
    Delete(ctx context.Context, id string) error
}
```

### How memory flows through the agent (already wired)
```
User message → agent.Run()
    → memory.Search(userMsg.Content, 8)   // top-8 relevant facts
    → buildSystemPrompt(facts)             // injected into system prompt
    → LLM completes with memory context
    → output.Reply
    → AutoSave(reply)                      // [S2-B] wire this call
```

### Secrets (already in GCP Secret Manager — do not hardcode)
- `zbot-slack-token` — Slack bot token
- `zbot-slack-app-token` — Slack app-level token  
- `anthropic-api-key` — Claude API key
- `brave-api-key` — Brave Search API key
- Postgres: currently hardcoded in `connectPostgres()` — OK for now, move to Secret Manager in Sprint 9

---

## File Change Summary

| File | Action | Description |
|------|--------|-------------|
| `internal/tools/memory.go` | CREATE | `SearchMemoryTool` |
| `internal/memory/store.go` | MODIFY | `New()` takes namespace param, `tableName()` helper |
| `cmd/zbot/wire.go` | MODIFY | AutoSave wiring + memorySearch tool registration |
| `cmd/memcli/main.go` | CREATE | Memory viewer CLI |

---

## Definition of Done

Send ZBOT in Slack:
> "My name is Jeremy and I'm building a Go agent called ZBOT."

ZBOT calls `save_memory` and confirms it saved.

Open a **new Slack DM session** (or restart ZBOT) and ask:
> "What's my name?"

ZBOT responds with "Jeremy" — pulled from pgvector memory, not conversation history.

Additionally:
- [ ] `go run ./cmd/memcli search "Jeremy"` returns the saved fact from the terminal
- [ ] `go run ./cmd/memcli list` shows recent memories with IDs and timestamps
- [ ] `go build ./...` passes clean with zero errors

---

## Git Commit (after verification)

```bash
git add -A
git commit -m "Sprint 2: Memory system live — search_memory tool, AutoSave wired, namespace isolation, memcli

- internal/tools/memory.go: SearchMemoryTool — agent can explicitly query its own memory
- internal/memory/store.go: namespace param in New(), tableName() helper
- cmd/zbot/wire.go: AutoSave wired after agent.Run(), search_memory registered
- cmd/memcli/main.go: CLI for list/search/delete/stats on zbot_memories
- ZBOT now remembers facts across Slack sessions via pgvector"
git push origin main
```
