# ZBOT Brain Region: Memory Cortex (Hippocampus v2)
## Architecture Spec + Sprint Brief

**Date:** March 19, 2026
**Author:** Jeremy + Claude (architecture session)
**Status:** Design phase

---

## The Problem with Current Memory

Current Hippocampus does a pgvector semantic search on EVERY turn:
1. User sends message
2. Agent extracts query from message
3. Runs vector similarity search against all memories
4. Waits for results (latency: 50-200ms per search)
5. Injects top-K results into system prompt
6. If query was bad → misses relevant memories
7. If memory phrasing is different from query → misses relevant memories

**Issues:**
- Every turn pays the search latency tax even for simple chat
- Quality depends on query construction (garbage in, garbage out)
- No awareness of what the agent is ABOUT to need
- Memories are flat — no structure, no grouping, no compression
- Mid-task enrichment (Stage 4) is a band-aid for missed memories
- Over time, memory store grows → searches get slower and noisier

---

## The Solution: Proactive Memory via "Thought Packages"

### Core Concept

Instead of searching at runtime, a background process (Memory Cortex) 
continuously organizes all memories into pre-built, compressed context 
blocks called **Thought Packages**. Each package is:

- **Topic-scoped:** all memories about one project/domain/context
- **Pre-compressed:** ~200-400 tokens, no redundancy, dense facts only
- **Ready to inject:** can be pasted directly into system prompt
- **Self-updating:** batch job refreshes nightly, flags changes

At runtime, the agent doesn't search — it selects packages by label 
match (microseconds, zero LLM cost) and injects them whole.

### Analogy

Current: Agent walks into a library, writes a search query on a card, 
hands it to a librarian, waits for books, hopes the right ones come back.

New: A librarian already organized everything into labeled folders on 
the agent's desk before the conversation started. Agent glances at the 
folder labels, grabs the relevant ones, and starts working.

---

## Architecture

### Thought Package Schema

```go
type ThoughtPackage struct {
    ID          string    `json:"id"`
    Label       string    `json:"label"`       // "ghl/esler-cst", "zbot/architecture", "lead-certain/ops"
    Keywords    []string  `json:"keywords"`     // fast label matching: ["ghl","esler","workflow","trigger"]
    Embedding   []float32 `json:"-"`            // vector for label-level similarity (NOT per-memory)
    Content     string    `json:"content"`      // compressed context block, ready to inject
    TokenCount  int       `json:"token_count"`  // pre-counted for budget management
    MemoryIDs   []string  `json:"memory_ids"`   // source memories this package was built from
    Freshness   time.Time `json:"freshness"`    // when this package was last rebuilt
    Priority    int       `json:"priority"`     // 0=always inject, 1=inject if relevant, 2=inject on demand
}
```

### Priority Levels

**Priority 0 — Always Inject (~400 tokens total)**
These are ALWAYS in the system prompt, every turn, no matching needed:
- User identity (name, location, communication style)
- Standing instructions (do-not-dos, preferences)
- Current top priorities (what the user is focused on RIGHT NOW)

**Priority 1 — Auto-Select (~200-600 tokens, 1-3 packages)**
Selected by keyword/embedding match against the user's message:
- Active project context (active projects, CRM, etc.)
- Recent decisions and state
- Relevant people and relationships

**Priority 2 — On Demand (~200 tokens each)**
Only injected when explicitly referenced or when Frontal Lobe plan says "needs_memory":
- Historical project details
- Technical specifications
- Old decisions that might be superseded

### Token Budget Per Turn

| Component | Tokens | Source |
|-----------|--------|--------|
| Priority 0 (always) | ~400 | Identity + instructions + priorities |
| Priority 1 (auto-selected, 1-3 packages) | ~400-1200 | Topic packages matching current turn |
| Priority 2 (on demand) | 0-400 | Only when explicitly needed |
| **Total memory injection** | **~800-2000** | **vs current: 0-800 (unreliable)** |

This is actually MORE memory context than the current system provides,
but it's BETTER memory because it's pre-organized and compressed.

---

## Batch Processing Pipeline

### Nightly Batch Job (runs at 2 AM MST)

**Input:** All memories from pgvector (zbot_memories table)
**Model:** Anthropic Batch API (Haiku 4.5 at 50% off) OR DeepSeek V3.2 ($0.14/M)
**Output:** Updated thought packages in thought_packages table

```
Step 1: DUMP — Read all memories from pgvector
  → ~500-2000 memories, ~50K-200K tokens total

Step 2: CLUSTER — Group memories by topic
  Prompt: "Here are N memories. Group them into logical clusters.
  Output JSON: [{label, memory_ids, summary}]"
  → Uses Gemini 2.5 Flash (1M context window, reads everything at once)
  → OR batch through Haiku in chunks of 100 memories

Step 3: COMPRESS — For each cluster, create a thought package
  Prompt: "Compress these N memories into a dense context block.
  Max 400 tokens. Include: key facts, dates, IDs, decisions.
  Remove: redundancy, outdated info, filler.
  Format as a tight paragraph, not bullet points."
  → Can batch all clusters in parallel

Step 4: DETECT — Flag contradictions, staleness, drift
  Prompt: "Compare these packages. Find:
  - Contradictions between packages
  - Memories that supersede older ones
  - Stale facts (older than 30 days with no refresh)
  - Priority drift (top priorities don't match recent activity)"
  → This is the Hypothalamus function

Step 5: STORE — Write packages to thought_packages table
  → Each package: label, keywords, embedding, compressed content
  → Old packages archived, not deleted

Step 6: REPORT — Generate memory health report
  → Contradictions found, stale items flagged, packages created/updated
  → Saved to workspace for user review
```

### Cost Estimate

For 1,000 memories (~100K tokens input):
- Gemini 2.5 Flash: ~$0.01 input + ~$0.02 output = **$0.03 per nightly run**
- Anthropic Batch (Haiku, 50% off): ~$0.05 input + ~$0.25 output = **$0.30 per nightly run**  
- DeepSeek V3.2: ~$0.014 input + ~$0.03 output = **$0.04 per nightly run**

**Winner: Gemini 2.5 Flash or DeepSeek V3.2 — under $0.05/night = ~$1.50/month**


---

## Runtime Flow (What Changes in agent.go)

### Before (current — reactive search)

```
User message arrives
  → memory.Search(ctx, userMessage, 8)     // 50-200ms, unreliable
  → inject raw facts into system prompt
  → Cortex runs
```

### After (new — proactive packages)

```
User message arrives
  → matchPackages(userMessage, allPackages)  // <1ms, keyword + embedding match on LABELS only
  → inject Priority 0 packages (always)
  → inject Priority 1 packages (matched)
  → Cortex runs with pre-organized context
  
  (NO pgvector search on the hot path)
  (Fallback: if Cortex explicitly calls search_memory tool, do a search — but this should be rare)
```

### Package Matching Algorithm (zero LLM cost)

```go
func matchPackages(userMessage string, packages []ThoughtPackage) []ThoughtPackage {
    var result []ThoughtPackage
    
    // Always include Priority 0
    for _, p := range packages {
        if p.Priority == 0 {
            result = append(result, p)
        }
    }
    
    // Keyword match for Priority 1 (fast, deterministic)
    words := tokenize(strings.ToLower(userMessage))
    for _, p := range packages {
        if p.Priority != 1 { continue }
        for _, kw := range p.Keywords {
            if containsAny(words, kw) {
                result = append(result, p)
                break
            }
        }
    }
    
    // If keyword match found < 2 packages, do a quick embedding similarity
    // on package LABELS only (not full memory store) — ~20 comparisons max
    if countP1(result) < 2 {
        labelMatches := embeddingMatchLabels(userMessage, packages, topK=2)
        result = append(result, labelMatches...)
    }
    
    // Token budget check — don't exceed 2000 tokens for memory injection
    return trimToTokenBudget(result, 2000)
}
```

This runs in MICROSECONDS. No LLM call. No database query on the hot path.
The agent just... knows.

---

## Database Schema

```sql
CREATE TABLE thought_packages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    label TEXT NOT NULL,                    -- "ghl/esler-cst"
    keywords TEXT[] NOT NULL,               -- {"ghl","esler","workflow","trigger"}
    embedding vector(768),                  -- label-level embedding (text-embedding-004)
    content TEXT NOT NULL,                  -- compressed context block
    token_count INT NOT NULL,              -- pre-counted
    memory_ids TEXT[] NOT NULL,            -- source memory IDs
    priority INT NOT NULL DEFAULT 1,       -- 0=always, 1=auto, 2=on-demand
    freshness TIMESTAMPTZ NOT NULL,        -- last rebuilt
    created_at TIMESTAMPTZ DEFAULT now(),
    updated_at TIMESTAMPTZ DEFAULT now()
);

CREATE INDEX idx_packages_priority ON thought_packages(priority);
CREATE INDEX idx_packages_label ON thought_packages(label);
CREATE INDEX idx_packages_keywords ON thought_packages USING gin(keywords);
CREATE INDEX idx_packages_embedding ON thought_packages USING hnsw(embedding vector_cosine_ops);
```

---

## Sprint Brief: Memory Cortex (Coworker)

**Estimated effort:** 5-7 days
**Branch:** feature/memory-cortex
**Depends on:** Postgres connected (Sprint 2 from main roadmap)

### Tasks

| # | Task | Priority | Est |
|---|------|----------|-----|
| MC-1 | Create thought_packages table + migration | P0 | 1hr |
| MC-2 | ThoughtPackage Go types in ports.go | P0 | 30min |
| MC-3 | PackageStore interface (CRUD + match) | P0 | 1hr |
| MC-4 | Postgres PackageStore adapter | P0 | 3hr |
| MC-5 | matchPackages() — keyword + embedding label match | P0 | 2hr |
| MC-6 | Update agent.go Run() — replace memory.Search with package injection | P0 | 2hr |
| MC-7 | Batch job: dump memories → cluster → compress → store packages | P1 | 4hr |
| MC-8 | Batch job: Anthropic Batch API integration | P1 | 3hr |
| MC-9 | Batch job: DeepSeek V3.2 / Gemini 2.5 Flash alternative | P1 | 2hr |
| MC-10 | Hypothalamus: contradiction detection + staleness flagging | P2 | 3hr |
| MC-11 | Memory health report generator | P2 | 2hr |
| MC-12 | CLI: `zbot memory rebuild` — trigger batch job manually | P1 | 1hr |
| MC-13 | CLI: `zbot memory packages` — list all thought packages | P1 | 30min |
| MC-14 | Keep search_memory tool as fallback (Cortex can still search if needed) | P1 | 30min |

### Definition of Done

1. `go build ./...` passes clean
2. `go test ./internal/memory/...` all pass
3. Batch job runs, creates thought packages from test memories
4. agent.go injects packages instead of doing per-turn searches
5. matchPackages returns results in <1ms (benchmark this)
6. search_memory tool still works as fallback
7. Memory health report generated after batch run

---

## What This Enables

Once Memory Cortex is live:
- **Zero-latency memory:** Agent knows everything relevant instantly
- **No wasted API calls:** No per-turn search queries
- **Better context:** Pre-compressed packages are more informative than raw search results
- **Self-healing memory:** Contradictions and staleness caught by batch job
- **Scalability:** 10,000 memories costs the same as 100 at runtime (batch handles the growth)
- **Foundation for Hypothalamus:** The batch job IS the Hypothalamus — it's the background sentinel scanning memory health

This is the single most impactful architectural change ZBOT can make.
Everything else (planning committee, GHL skills, research pipeline) works better when memory is instant.

---

*Spec created March 19, 2026 — Ziloss Technologies*
