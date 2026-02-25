# ZBOT Sprint 2 — Coworker Mission Brief
## Objective: Memory That Persists Across Sessions

You are working on ZBOT, a personal AI agent for Jeremy Lerwick (CEO, Ziloss Technologies).
The codebase is at: ~/Desktop/zbot
GitHub: https://github.com/jeremylerwick-max/zbot (private)
GCP Project: ziloss (project number: 203743871797)

ZBOT is live on Slack. Claude responds intelligently. Sprint 1 is done.
Your job is to make ZBOT remember things across sessions using pgvector.

---

## Current State (Sprint 1 Complete)

### What exists and WORKS:
- Slack Socket Mode gateway: internal/gateway/slack.go ✅
- Real Claude LLM client: internal/llm/anthropic.go ✅
- save_memory tool: internal/tools/filetools.go → MemorySaveTool ✅
- Memory store (pgvector): internal/memory/store.go ✅
- Vertex AI embedder: internal/memory/embedder.go ✅
- In-memory fallback: internal/memory/fallback.go ✅
- Auto memory injection in system prompt: internal/agent/agent.go → buildSystemPrompt() ✅
- Hybrid BM25 + vector search with time decay: internal/memory/store.go → Search() ✅
- Postgres connected at 34.28.163.109, zbot_memories table created on boot ✅
- go build ./... passes clean ✅

### What's MISSING (your 4 tasks):
1. search_memory tool — agent can't explicitly query its own memory yet
2. AutoSave not wired — store.go has the method but wire.go never calls it
3. Namespace isolation — table name hardcoded, needs to derive from namespace field
4. memcli — no CLI to inspect memories from terminal

---

## Sprint 2 Tasks — Complete ALL of These

### TASK 1: Add search_memory Tool

Create ~/Desktop/zbot/internal/tools/memory.go

```go
package tools

import (
    "context"
    "fmt"
    "time"
    "github.com/jeremylerwick-max/zbot/internal/agent"
)

type SearchMemoryTool struct{ store agent.MemoryStore }

func NewSearchMemoryTool(store agent.MemoryStore) *SearchMemoryTool {
    return &SearchMemoryTool{store}
}

func (t *SearchMemoryTool) Name() string { return "search_memory" }

func (t *SearchMemoryTool) Definition() agent.ToolDefinition {
    return agent.ToolDefinition{
        Name:        "search_memory",
        Description: "Search your long-term memory for facts you've previously saved. Use when the user asks 'do you remember...' or 'what do you know about...'",
        InputSchema: map[string]any{
            "type":     "object",
            "required": []string{"query"},
            "properties": map[string]any{
                "query": map[string]any{"type": "string", "description": "What to search for"},
                "limit": map[string]any{"type": "integer", "description": "Max results (default 5)"},
            },
        },
    }
}

func (t *SearchMemoryTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
    query, _ := input["query"].(string)
    if query == "" {
        return &agent.ToolResult{Content: "error: query required", IsError: true}, nil
    }
    limit := 5
    if l, ok := input["limit"].(float64); ok && l > 0 {
        limit = int(l)
    }
    facts, err := t.store.Search(ctx, query, limit)
    if err != nil {
        return &agent.ToolResult{Content: fmt.Sprintf("search error: %v", err), IsError: true}, nil
    }
    if len(facts) == 0 {
        return &agent.ToolResult{Content: "No memories found for that query."}, nil
    }
    out := fmt.Sprintf("## Memory Search: %q\n\n", query)
    for i, f := range facts {
        age := time.Since(f.CreatedAt)
        ageStr := fmt.Sprintf("%.0f days ago", age.Hours()/24)
        if age.Hours() < 24 {
            ageStr = fmt.Sprintf("%.0f hours ago", age.Hours())
        }
        out += fmt.Sprintf("%d. [%s | %s] %s\n", i+1, f.Source, ageStr, f.Content)
    }
    return &agent.ToolResult{Content: out}, nil
}
```

Then in cmd/zbot/wire.go, add it alongside memorySave:
```go
memorySearch := tools.NewSearchMemoryTool(memStore)
// add memorySearch to agent.New(...) call
```

### TASK 2: Wire AutoSave After Every Agent Turn

In cmd/zbot/wire.go, find the handler function. After ag.Run() succeeds, add:

```go
output, err := ag.Run(ctx, input)
if err != nil {
    return "", fmt.Errorf("agent.Run: %w", err)
}

// Auto-save substantial replies to long-term memory.
if pgStore, ok := memStore.(*memory.Store); ok {
    pgStore.AutoSave(ctx, sessionID, output.Reply)
}
```

The AutoSave method already exists in internal/memory/store.go — just wire the call.
It saves responses > 200 chars automatically. Don't change the heuristic.

### TASK 3: Namespace Isolation in Memory Store

In internal/memory/store.go:

1. Add a tableName() method:
```go
func (s *Store) tableName() string {
    return s.namespace + "_memories"
}
```

2. Replace every literal "zbot_memories" string in store.go with s.tableName()
   (there are occurrences in Save, Search, Delete, and migrate)

3. Update New() to accept namespace as a parameter:
```go
func New(ctx context.Context, db *pgxpool.Pool, embedder Embedder, logger *slog.Logger, namespace string) (*Store, error) {
```

4. Update wire.go to pass "zbot":
```go
store, storeErr := memory.New(ctx, pgDB, embedder, logger, "zbot")
```

This ensures zbot_memories and mem0_memories_vertex coexist cleanly on the same DB.

### TASK 4: memcli — Memory Viewer CLI

Create ~/Desktop/zbot/cmd/memcli/main.go

A terminal tool for Jeremy to inspect, search, and delete ZBOT memories directly.

```go
package main

// Commands:
// memcli list [--limit N]     → list most recent N memories (default 20)
// memcli search <query>       → semantic + BM25 search
// memcli delete <id>          → delete by ID
// memcli stats                → count, oldest, newest

// Implementation:
// - Use same connectPostgres() from wire.go (copy the function or move to platform/)
// - Use memory.New() with namespace "zbot"
// - Use memory.NewVertexEmbedder() for real search (fallback to noop if unavailable)
// - Output clean plaintext table, no fancy TUI
// - list output format:
//   ID              CREATED          SOURCE      CONTENT (truncated to 80 chars)
//   a1b2c3d4e5f6    2 days ago       agent       ZBOT is Jeremy's personal AI agent...
```

Build target: go build -o memcli ./cmd/memcli

---

## Definition of Done

1. DM ZBOT in Slack: "My name is Jeremy and I'm the founder of Ziloss Technologies."
2. ZBOT calls save_memory and confirms.
3. Restart ZBOT (Ctrl+C, go run ./cmd/zbot again).
4. DM ZBOT in a new session: "What's my name?"
5. ZBOT responds "Jeremy" — from pgvector, NOT from conversation history.
6. Run: go run ./cmd/memcli search "Jeremy" → see the saved fact in terminal.
7. go build ./... passes clean.

---

## Git Commit

```bash
cd ~/Desktop/zbot
git add -A
git commit -m "Sprint 2: Memory live — search_memory tool, AutoSave wired, namespace isolation, memcli

- internal/tools/memory.go: SearchMemoryTool — explicit memory query tool
- internal/memory/store.go: namespace param, tableName() helper, no hardcoded table names
- cmd/zbot/wire.go: AutoSave wired after agent.Run(), search_memory registered
- cmd/memcli/main.go: CLI for list/search/delete/stats on zbot_memories
- ZBOT remembers facts across Slack sessions via pgvector"
git push origin main
```

## Important Notes

- Never put secrets in code. All secrets via GCP Secret Manager only.
- go build ./... must pass after every change.
- Hexagonal architecture — agent package never imports adapters directly.
- If Vertex AI embedder fails during memcli, fall back to NoopEmbedder (BM25-only search still works).
- The zbot_memories table already exists — do NOT drop and recreate it. Use IF NOT EXISTS everywhere.
