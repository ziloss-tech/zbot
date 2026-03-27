# ZBOT Memory Overhaul — Implementation Plan + Benchmarks
## "Hippocampus v2" → Thought Package Architecture

**Date:** March 27, 2026
**Status:** Implementation plan — ready to execute
**Spec:** docs/sprints/MEMORY_CORTEX_SPEC.md (architecture reference)
**Current:** 1,519 lines across 11 files in internal/memory/

---

## Current State (Baseline Measurements)

What exists today:
- pgvector Store (Save/Search/Delete with BM25 + vector hybrid scoring)
- Vertex AI text-embedding-004 (768 dims)
- Time decay scoring
- Diversity re-ranker (0.92 cosine threshold)
- Context flusher (extract facts from conversation before compaction)
- Daily notes writer (timestamped markdown files)
- Curator (batch process recent memories)
- SQLite session history
- InMemory fallback store

### Baseline Benchmarks to Measure BEFORE Starting

Run these tests before touching any code. These are our "before" numbers.

| Metric | How to Measure | Target File |
|--------|---------------|-------------|
| B1. Memory search latency (p50/p95) | Time 100 searches, record distribution | memory_bench_test.go |
| B2. Memory count | SELECT count(*) FROM zbot_memories | manual query |
| B3. Retrieval relevance | 20 test queries, human-rate if top-5 results are relevant (0-5 score) | manual eval |
| B4. Context injection tokens | Average tokens injected per turn (log from 50 real turns) | agent telemetry |
| B5. Memory-related hallucinations | Review last 50 Thalamus rejections — how many were memory gaps? | audit log analysis |
| B6. Cost per turn (memory portion) | Embedding cost + search cost per turn | billing logs |
| B7. Duplicate rate | Run diversity check on all memories, count near-dupes (>0.92 cosine) | memory_bench_test.go |


---

## Lofty But Achievable Goals

These are our stretch targets — measurable, time-bound, and designed to put
ZBOT's memory ahead of every consumer-grade AI assistant.

### GOAL 1: Zero-Search Runtime (< 1ms memory injection)
**Benchmark:** Memory context available in < 1ms at turn start (vs current 50-200ms)
**How:** Pre-built Thought Packages selected by keyword match, not vector search
**Verification:** p99 latency test on 1000 turns

### GOAL 2: 95% Retrieval Relevance
**Benchmark:** On a 50-query eval set, top-5 injected memories score 4.5+/5.0 relevance
**How:** Thought Packages + Frontal Lobe memory routing (plan says "needs project X context")
**Verification:** Human-rated eval (you review 50 ZBOT responses for memory quality)

### GOAL 3: Experiential Learning (learn from mistakes)
**Benchmark:** When Thalamus rejects a response, the correction pattern is saved.
Next time a similar query comes in, ZBOT doesn't repeat the mistake.
**How:** "Lesson" memory type: {mistake, correction, context} → matched by similarity
**Verification:** Replay 10 historical Thalamus rejections, verify ZBOT avoids the same errors

### GOAL 4: Temporal Awareness (knows what's stale)
**Benchmark:** Facts older than 30 days with no refresh are auto-flagged.
ZBOT says "I remember X but that was 6 weeks ago — want me to verify?"
**How:** Freshness scoring + staleness detection in nightly batch
**Verification:** Plant 10 stale facts, verify ZBOT flags them within 3 turns of relevance

### GOAL 5: Contradiction Detection
**Benchmark:** When two memories contradict, ZBOT identifies the conflict and asks for resolution
instead of randomly picking one.
**How:** Nightly batch runs pairwise contradiction check across packages
**Verification:** Plant 5 contradictory facts, verify ZBOT catches all 5

### GOAL 6: Memory Cost Under $2/month
**Benchmark:** All memory operations (nightly batch + runtime) cost < $2/month total
**How:** DeepSeek V3.2 for batch ($0.04/night), zero LLM cost at runtime (keyword match only)
**Verification:** 30-day cost tracking dashboard

### GOAL 7: Working Memory (per-session context carryover)
**Benchmark:** Within a session, ZBOT remembers all context from earlier in the conversation.
Across sessions, ZBOT picks up where it left off on active projects.
**How:** Session history (already exists) + "last session summary" package (auto-generated)
**Verification:** Start conversation, discuss 3 topics, close session, reopen, verify ZBOT recalls all 3

### GOAL 8: Memory Capacity > 10,000 facts without degradation
**Benchmark:** Search quality and speed don't degrade as memory grows from 500 → 10,000 facts
**How:** Thought Packages compress N memories → 1 package. Search operates on ~50-100 packages, not 10K facts
**Verification:** Load test with synthetic memories at 1K, 5K, 10K — measure latency and relevance


---

## Phased Implementation

### PHASE 1: Baseline + Instrumentation (Day 1-2)
**Goal:** Know exactly where we are before changing anything.
**Risk:** Zero — read-only.

Tasks:
1. Write memory_bench_test.go — benchmark search latency, count, duplicate rate
2. Add telemetry to agent.go — log memory injection tokens per turn
3. Run 50 real queries through ZBOT, record retrieval relevance scores
4. Query audit logs for Thalamus rejections caused by memory gaps
5. Record all B1-B7 baseline numbers in a results file

Test:
```bash
go test -bench=. -benchtime=10s ./internal/memory/
```

**Exit criteria:** All 7 baseline metrics recorded. No code changes to production.

---

### PHASE 2: Thought Package Schema + Storage (Day 3-4)
**Goal:** Database schema and Go types for Thought Packages.

Tasks:
1. Define ThoughtPackage struct in internal/memory/packages.go
2. Create thought_packages table in Postgres (auto-migrate)
3. CRUD operations: SavePackage, GetPackage, ListPackages, DeletePackage
4. Package matching function: matchPackages(query, packages) → scored results
   - Keyword match (fast, zero-cost)
   - Label embedding similarity (fallback, still fast — operates on ~50-100 package embeddings, not 10K memories)
5. Unit tests for all operations + matching

```go
type ThoughtPackage struct {
    ID         string     `json:"id"`
    Label      string     `json:"label"`        // "ghl/esler-cst"
    Keywords   []string   `json:"keywords"`
    Embedding  []float32  `json:"-"`
    Content    string     `json:"content"`       // compressed, ready to inject
    TokenCount int        `json:"token_count"`
    MemoryIDs  []string   `json:"memory_ids"`
    Priority   int        `json:"priority"`      // 0=always, 1=auto, 2=on-demand
    Freshness  time.Time  `json:"freshness"`
    Version    int        `json:"version"`
}
```

Test:
```bash
go test ./internal/memory/ -run TestThoughtPackage
```

**Exit criteria:** Can CRUD packages in Postgres. matchPackages returns results in < 1ms.

---

### PHASE 3: Nightly Batch Builder (Day 5-7)
**Goal:** Background job that reads all memories, clusters them, and builds packages.

Tasks:
1. internal/memory/batch.go — the nightly pipeline
2. Step 1: Dump all facts from pgvector (paginated, memory-safe)
3. Step 2: Cluster by topic using DeepSeek V3.2 (cheap, $0.04/run)
   - Send memories in chunks of 100
   - Prompt: "Group these into logical topic clusters. Output JSON."
4. Step 3: Compress each cluster into a Thought Package
   - Prompt: "Compress these N memories into a dense 200-400 token block."
5. Step 4: Detect contradictions + staleness
6. Step 5: Store packages, archive old versions
7. Step 6: Generate memory health report (saved to workspace)
8. Wire into scheduler (run at 2 AM MST)

Test:
```bash
# Run batch manually
go run ./cmd/memcli batch --dry-run
# Verify packages
go run ./cmd/memcli packages --list
```

**Exit criteria:** Batch creates packages from real memories. Cost per run < $0.10.
Run it 3 nights in a row, verify packages improve each time.

---

### PHASE 4: Runtime Integration (Day 8-9)
**Goal:** Agent uses Thought Packages instead of raw vector search.

Tasks:
1. Modify agent.go memory injection:
   - Replace: memory.Search(ctx, userMessage, 8)
   - With: packages.Match(ctx, userMessage, tokenBudget)
2. Priority 0 packages always injected (identity, instructions, current priorities)
3. Priority 1 packages auto-matched by message keywords + Frontal Lobe plan
4. Priority 2 packages only when search_memory tool is explicitly called
5. Fallback: if no packages exist yet, fall back to current vector search
6. Token budget governor: max 2000 tokens for memory injection per turn
7. Log: which packages were injected, token count, match method

Test:
```bash
# Compare responses with and without packages
go test ./internal/agent/ -run TestMemoryInjection
# A/B: same 20 queries, old search vs new packages, compare quality
```

**Exit criteria:** Agent uses packages for memory. Latency drops from 50-200ms to < 1ms.
Retrieval relevance >= 4.0/5.0 on 50-query eval set.


---

### PHASE 5: Experiential Learning (Day 10-12)
**Goal:** ZBOT learns from its mistakes. Thalamus rejections become lessons.

Tasks:
1. New memory type: "lesson" — {mistake, correction, context, created_at}
2. When Thalamus rejects a response and revision succeeds:
   - Extract the pattern: what was wrong, what fixed it
   - Save as a lesson fact with high relevance weight
3. Lessons get their own Thought Package: "lessons/recent"
4. At runtime, lesson package is Priority 1 — auto-injected when context matches
5. Lesson deduplication: similar lessons merge (diversity reranker)
6. Lesson aging: lessons older than 90 days without re-trigger get demoted

```go
type Lesson struct {
    ID          string
    Mistake     string   // "Cited specific financial figures not in evidence"
    Correction  string   // "Only include figures directly from search results"
    Context     string   // "Research query about xAI financials"
    TurnID      string   // link to the turn that generated this lesson
    TriggerCount int     // how many times this lesson has been relevant
    CreatedAt   time.Time
}
```

Test:
- Trigger a Thalamus rejection manually (ask ZBOT to research something)
- Verify lesson is saved
- Ask a similar question — verify ZBOT avoids the same mistake
- Check lesson package content

**Exit criteria:** 10 historical Thalamus rejections replayed. ZBOT avoids the same
error on 8/10 (80% improvement rate).

---

### PHASE 6: Temporal Awareness + Contradiction Detection (Day 13-15)
**Goal:** ZBOT knows when facts are stale and catches contradictions.

Tasks:
1. Freshness scoring in nightly batch:
   - Each memory gets a freshness_score = 1.0 at creation, decays 0.03/day
   - Memories referenced in conversation get freshness reset to 1.0
   - Memories below 0.3 freshness (> 23 days) flagged as "potentially stale"
2. At runtime: if a stale memory is about to be injected, add caveat:
   "[Note: this information is from {date} and may be outdated]"
3. Contradiction detection in nightly batch:
   - Pairwise comparison of package contents
   - Prompt: "Do these two statements contradict? If so, which is newer/more authoritative?"
   - Flagged contradictions stored in memory_health table
4. At runtime: if contradicting memories exist for current context, ZBOT asks:
   "I have conflicting information about X. [Fact A] vs [Fact B]. Which is correct?"
5. User resolution saved as a "resolution" fact that supersedes both originals

Test:
- Plant a fact dated 40 days ago: "ZBOT runs on Haiku by default"
- Ask "what model does ZBOT use?" — verify ZBOT flags the staleness
- Plant two contradicting facts: "Jeremy uses Mac Studio" vs "Jeremy uses Mac Pro"
- Ask about Jeremy's computer — verify ZBOT catches the contradiction

**Exit criteria:** 10/10 stale facts flagged. 5/5 contradictions caught. Zero false positives.

---

### PHASE 7: Session Continuity + Working Memory (Day 16-17)
**Goal:** ZBOT picks up where you left off, every time.

Tasks:
1. End-of-session summary: when session ends (10 min idle or explicit close):
   - Haiku extracts: topics discussed, decisions made, open items, action items
   - Saved as "session_summary" fact with session_id
2. Start-of-session injection: when new session begins:
   - Load last 3 session summaries
   - Build "recent_sessions" Thought Package (Priority 0)
   - ZBOT knows: "Last time we worked on GHL workflow migration. Before that, heartbeat UI."
3. Active project tracker:
   - Monitor which topics appear most in last 7 days
   - Auto-build "current_priorities" package (Priority 0)
   - Updates after each session, not just nightly

Test:
- Session 1: discuss GHL workflows, end session
- Session 2: open fresh, ask "where were we?" — verify ZBOT recalls GHL workflows
- Session 3: discuss 3 different topics, end session
- Session 4: ask "what did we cover last time?" — verify all 3 topics recalled

**Exit criteria:** 3/3 session handoffs work correctly. "Where were we?" always produces
accurate, specific recall — not generic "we discussed various topics."


---

## Overall Timeline

| Phase | Days | What Ships | Goals Hit |
|-------|------|-----------|-----------|
| 1. Baseline | 1-2 | Benchmarks, telemetry | Measurement foundation |
| 2. Schema | 3-4 | ThoughtPackage CRUD + matching | Goal 1 (< 1ms matching) |
| 3. Batch Builder | 5-7 | Nightly clustering + compression | Goal 6 (< $2/mo), Goal 8 (10K capacity) |
| 4. Runtime | 8-9 | Agent uses packages | Goal 1 (zero-search), Goal 2 (95% relevance) |
| 5. Lessons | 10-12 | Learn from Thalamus rejections | Goal 3 (experiential learning) |
| 6. Temporal | 13-15 | Staleness + contradictions | Goal 4 (temporal), Goal 5 (contradictions) |
| 7. Sessions | 16-17 | Session continuity | Goal 7 (working memory) |

**Total: ~17 working days (~3.5 weeks)**

---

## Success Criteria (ship/no-ship decision)

ALL of these must pass to call the memory overhaul "done":

| # | Criteria | Measurement |
|---|----------|-------------|
| S1 | Memory injection latency < 5ms at p99 | memory_bench_test.go |
| S2 | Retrieval relevance >= 4.5/5.0 on 50-query eval | Human-rated |
| S3 | Lesson learning: 80%+ improvement on replayed rejections | Automated replay |
| S4 | Stale fact detection: 100% recall, 0% false positive | Planted test facts |
| S5 | Contradiction detection: 100% recall | Planted contradictions |
| S6 | Memory cost < $2/month | 30-day billing |
| S7 | Session recall: "where were we?" works 100% of the time | Manual test |
| S8 | 10K memory load test: no latency degradation vs 1K | memory_bench_test.go |
| S9 | All existing memory tests still pass | go test ./internal/memory/ |
| S10 | Zero regressions in agent behavior | Full test suite green |

---

## What This Puts ZBOT Ahead Of

**ChatGPT Memory:** Flat key-value pairs. No compression, no temporal awareness,
no contradiction detection, no experiential learning. Max ~100 memories.

**Claude Memory (this product):** Session-derived summaries, no real-time learning,
no temporal decay, no package pre-organization. Good at pattern extraction but
no structured retrieval optimization.

**Perplexity/Gemini:** No persistent memory at all.

**Custom agents (AutoGPT, CrewAI, etc.):** Vector search only. Same latency and
relevance problems ZBOT currently has. No thought packages, no batch organization.

ZBOT after this overhaul:
- Pre-organized memory (thought packages) — unique
- Experiential learning from mistakes — unique
- Temporal awareness with staleness flagging — unique among consumer agents
- Contradiction detection and resolution — unique
- Session continuity with structured handoffs — unique
- Sub-millisecond memory injection — 100-200x faster than vector search
- Scales to 10K+ memories without degradation — unique architecture

The closest comparison in research is MemGPT (now Letta), but ZBOT's approach
is more practical: batch organization instead of runtime paging, thought packages
instead of virtual context management, and the experiential learning loop
(Thalamus → lessons → prevention) has no equivalent in the open-source ecosystem.

---

## Files to Create

```
internal/memory/
  packages.go          — ThoughtPackage struct, CRUD, matching
  packages_test.go     — Unit tests
  batch.go             — Nightly pipeline (cluster, compress, detect)
  batch_test.go        — Pipeline tests with mock LLM
  lessons.go           — Lesson extraction from Thalamus rejections
  lessons_test.go      — Lesson tests
  temporal.go          — Freshness scoring, staleness detection
  temporal_test.go     — Temporal tests
  session_summary.go   — End-of-session extraction, start-of-session injection
  session_summary_test.go
  memory_bench_test.go — Performance benchmarks (baseline + ongoing)
```

Modifications:
```
internal/agent/agent.go    — Replace memory.Search with packages.Match
internal/agent/ports.go    — Add PackageStore interface
cmd/memcli/main.go         — CLI: batch --dry-run, packages --list, health --report
```

---

*This plan turns ZBOT's memory from "search and hope" into "organized, proactive,
self-improving context management." The result is an agent that gets smarter over
time, catches its own mistakes, knows what it doesn't know, and never forgets
where you left off.*
