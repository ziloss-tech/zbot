# ZBOT v2 — System Architecture Document

_Created: 2026-03-16 | Status: APPROVED_
_Supersedes: Sprint 11 Dual Brain architecture_

---

## 1. Executive Summary

ZBOT v2 is a fundamental architecture overhaul that eliminates the multi-model planner/executor/critic orchestration pattern in favor of a **single-brain architecture** powered by Anthropic's Claude model family. This document covers the system design for all six major subsystems: the single-brain agent core, deep research pipeline, credentialed site research, memory system, concurrent task execution, and the dynamic split-pane web UI.

### Design Principles

1. **Simplicity over cleverness.** One model doing one thing well beats three models losing context at every handoff.
2. **Safety by default.** Credentials encrypted at rest, code sandboxed in Docker, secrets never in logs or memory.
3. **Hexagonal architecture preserved.** Every external dependency remains behind a port interface (ADR-002). Swapping LLM providers, memory backends, or secret stores requires zero changes to core logic.
4. **Cost awareness.** Haiku for bulk work, Sonnet for default reasoning, Opus only when complexity demands it. Every token is tracked.
5. **Concurrency as a first-class citizen.** Background tasks never block interactive queries. Go goroutines make this natural.

---

## 2. High-Level Component Diagram

```
┌──────────────────────────────────────────────────────────────────┐
│                        CHANNELS (Gateways)                       │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌────────────────┐   │
│  │ Telegram  │  │  Web UI  │  │  Slack   │  │ Halo Glasses   │   │
│  │ Gateway   │  │ Gateway  │  │ Gateway  │  │ (future)       │   │
│  └─────┬─────┘  └─────┬────┘  └────┬─────┘  └───────┬────────┘   │
│        │              │            │                 │            │
└────────┼──────────────┼────────────┼─────────────────┼────────────┘
         │              │            │                 │
         ▼              ▼            ▼                 ▼
┌──────────────────────────────────────────────────────────────────┐
│                       AGENT CORE (internal/agent)                │
│                                                                  │
│  ┌──────────────────────────────────────────────────────────┐    │
│  │              Single-Brain Router                         │    │
│  │  ┌─────────┐  ┌──────────┐  ┌──────────┐                │    │
│  │  │  Haiku  │  │ Sonnet   │  │  Opus    │                │    │
│  │  │ (bulk)  │  │(default) │  │(escalate)│                │    │
│  │  └─────────┘  └──────────┘  └──────────┘                │    │
│  └──────────────────────────────────────────────────────────┘    │
│                                                                  │
│  ┌─────────────┐  ┌──────────────┐  ┌────────────────────┐      │
│  │  Task Queue  │  │  Tool Router  │  │  Memory Injector  │      │
│  │  (goroutine  │  │  (web_search, │  │  (pgvector top-k  │      │
│  │   pool)      │  │   run_code,   │  │   + daily notes)  │      │
│  │              │  │   fetch_url…) │  │                    │      │
│  └──────┬───────┘  └──────┬───────┘  └────────┬───────────┘      │
│         │                 │                    │                  │
└─────────┼─────────────────┼────────────────────┼──────────────────┘
          │                 │                    │
          ▼                 ▼                    ▼
┌──────────────────────────────────────────────────────────────────┐
│                      INFRASTRUCTURE LAYER                        │
│                                                                  │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌────────────────┐   │
│  │ Postgres │  │ pgvector │  │  Docker  │  │ Brave Search   │   │
│  │(workflows│  │ (memory) │  │(sandbox) │  │     API        │   │
│  │  tasks,  │  │          │  │          │  │                │   │
│  │  audit)  │  │          │  │          │  │                │   │
│  └──────────┘  └──────────┘  └──────────┘  └────────────────┘   │
│                                                                  │
│  ┌──────────┐  ┌──────────┐  ┌──────────────────────────────┐   │
│  │  macOS   │  │  GCloud  │  │  go-rod (headless Chromium)  │   │
│  │ Keychain │  │  Secret  │  │  for credentialed scraping   │   │
│  │          │  │  Manager │  │                              │   │
│  └──────────┘  └──────────┘  └──────────────────────────────┘   │
│                                                                  │
└──────────────────────────────────────────────────────────────────┘
```

---

## 3. Subsystem Design

### 3.1 Single-Brain Agent Core

**Decision:** ADR-004 — Kill multi-model orchestration
**Why:** Models have reached the capability threshold where a single model (Sonnet 4.6) can plan, execute, and self-critique in one pass. The planner/executor/critic pattern introduced 2024-era overhead: three context windows, information loss at each handoff, and orchestration complexity that produced more bugs than value.

#### Model Selection Strategy

| Model | Role | When Used | Cost (per 1M tokens) |
|-------|------|-----------|---------------------|
| Claude Haiku 4.5 | Bulk extraction, source gathering | Deep research Phase 1, lightweight classification | $0.25 in / $1.25 out |
| Claude Sonnet 4.6 | **Default brain** — planning, execution, self-critique | All standard user interactions, tool use, code generation | $3 in / $15 out |
| Claude Opus 4.6 | Complex reasoning escalation | Multi-step analysis, ambiguous problems, long chains of thought | $15 in / $75 out |

#### Complexity Escalation Logic

Sonnet handles everything by default. Escalation to Opus triggers when **any** of these conditions are met:

1. **User explicit:** User invokes `/think` or `/deep` command.
2. **Token budget exceeded:** Sonnet's response hits the extended thinking budget limit (suggesting the problem needs more reasoning depth).
3. **Tool chain depth:** Task requires 5+ sequential tool calls with dependencies between outputs.
4. **Self-assessed uncertainty:** Sonnet's response includes hedging language above a threshold (detected via a lightweight classifier pass).

Escalation is **one-shot** — Opus handles the full task, it does not hand back to Sonnet mid-task.

#### Agent Loop (simplified)

```
receive_message(user_input)
    │
    ├── inject_memories(semantic_search(user_input, top_k=5))
    ├── inject_system_prompt(persona + tools + context)
    │
    ├── call_llm(sonnet_4_6, messages)
    │       │
    │       ├── if tool_call → execute_tool() → append_result → loop
    │       ├── if escalation_needed → call_llm(opus_4_6, messages)
    │       └── if done → return response
    │
    ├── save_to_memory(user_input, response)  // automatic
    ├── log_to_audit(tokens, cost, tools_used)
    │
    └── send_response(gateway, response)
```

#### Port Interface Changes

```go
// Updated ports.go — simplified from v1
type LLMClient interface {
    Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
    ChatStream(ctx context.Context, req ChatRequest, tokens chan<- string) (*ChatResponse, error)
}

// REMOVED: PlannerClient, CriticClient, ExecutorClient
// These three interfaces collapse into the single LLMClient above.
// The model router selects Haiku/Sonnet/Opus based on the request's
// ModelHint field, not by calling different interfaces.

type ChatRequest struct {
    Messages  []Message
    Tools     []ToolDef
    ModelHint ModelTier  // "haiku" | "sonnet" | "opus" | "auto"
    Stream    bool
    MaxTokens int
}
```

---

### 3.2 Deep Research v2

**Decision:** ADR-005 — Two-phase research pipeline

The v1 research pipeline used a single expensive model for everything: generating queries, fetching sources, reading pages, and synthesizing. V2 splits this into a cheap bulk-gather phase and a smart synthesis phase.

#### Architecture

```
User: "Research AR display technology for Z-Glass"
                    │
                    ▼
        ┌───────────────────────┐
        │  Phase 1: GATHER      │  Model: Haiku 4.5
        │  (cheap, parallel)    │  Cost: ~$0.01-0.05
        │                       │
        │  1. Generate 20-50    │
        │     search queries    │
        │  2. Fire all via      │
        │     Brave Search API  │
        │     (parallel)        │
        │  3. Fetch top results │
        │     (parallel, 5 at   │
        │      a time)          │
        │  4. Extract relevant  │
        │     facts → JSON      │
        └───────────┬───────────┘
                    │
                    ▼
        ┌───────────────────────┐
        │  Intermediate Format  │
        │  []ResearchFact {     │
        │    Source: URL         │
        │    Title: string      │
        │    Facts: []string    │
        │    Quotes: []string   │
        │    Relevance: float   │
        │    FetchedAt: time    │
        │  }                    │
        └───────────┬───────────┘
                    │
                    ▼
        ┌───────────────────────┐
        │  Phase 2: SYNTHESIZE  │  Model: Sonnet 4.6
        │  (smart, one pass)    │  w/ extended thinking
        │                       │  Cost: ~$0.10-0.50
        │  1. Receive all       │
        │     extracted facts   │
        │  2. Reason through    │
        │     contradictions    │
        │  3. Weigh evidence    │
        │  4. Structure report  │
        │  5. Generate citations│
        └───────────────────────┘
                    │
                    ▼
            Final Report (Markdown)
            with inline citations
```

#### Cost Model

| Scenario | Sources | Gather Cost | Synthesis Cost | Total |
|----------|---------|-------------|----------------|-------|
| Quick research | 10-15 | $0.005 | $0.05 | ~$0.06 |
| Standard research | 30-40 | $0.02 | $0.15 | ~$0.17 |
| Deep dive | 40-50 | $0.05 | $0.40 | ~$0.45 |
| Old pipeline (comparison) | 10-20 | — | $0.25 | $0.25 (fewer sources, worse quality) |

#### Concurrency Design

Phase 1 uses a worker pool of 10 goroutines for parallel fetching. Each source is fetched independently. Brave Search API rate limit: 15 requests/second (free tier) — the pool respects this with a token bucket rate limiter.

Phase 2 is a single LLM call with extended thinking enabled. No parallelism needed — the model does all reasoning in one pass.

#### Failure Handling

- **Fetch failures:** Logged and skipped. If <50% of sources fail, synthesis proceeds with what's available. If >50% fail, the user is notified and offered a retry.
- **Synthesis failure:** Falls back to Opus 4.6 if Sonnet's synthesis is truncated or incoherent (detected by a post-synthesis quality check).
- **Rate limiting:** Exponential backoff on Brave Search 429s. Max 3 retries per query.

---

### 3.3 Credentialed Site Research

**Decision:** ADR-006 — Apple Keychain default, GCloud Secret Manager fallback

#### Threat Model

The core risk is credential exposure. Credentials must never appear in:
- LLM conversation history (prompt or response)
- pgvector memory store
- Postgres audit logs
- Application logs (stdout/stderr)
- Docker container environments
- Error messages returned to the user

#### Architecture

```
User: "Z, research this WSJ article"
            │
            ▼
    ┌────────────────────┐
    │  Credential Lookup  │
    │  domain: wsj.com    │
    │                     │
    │  config.yaml:       │
    │  secrets.backend:   │
    │    keychain | gcloud│
    └────────┬────────────┘
             │
     ┌───────┴────────┐
     ▼                ▼
┌──────────┐   ┌───────────┐
│  macOS   │   │  GCloud   │
│ Keychain │   │  Secret   │
│          │   │  Manager  │
│ security │   │           │
│ find-    │   │ secretmgr │
│ generic- │   │ .Access() │
│ password │   │           │
└────┬─────┘   └─────┬─────┘
     │               │
     └───────┬───────┘
             │ credentials (in-memory only)
             ▼
    ┌────────────────────┐
    │  go-rod Headless   │
    │  Browser Session   │
    │                    │
    │  1. Navigate to    │
    │     login URL      │
    │  2. Fill form      │
    │  3. Submit         │
    │  4. Navigate to    │
    │     target page    │
    │  5. Extract content│
    │  6. DESTROY session│
    │     (no cookies    │
    │      persist)      │
    └────────┬───────────┘
             │ clean text only
             ▼
    ┌────────────────────┐
    │  Content returned  │
    │  to agent core     │
    │  (no credentials   │
    │   in this payload) │
    └────────────────────┘
```

#### Security Controls

| Control | Implementation |
|---------|---------------|
| Encryption at rest | macOS Keychain: AES-256-GCM (OS-managed). GCloud: Google-managed AES-256. |
| Access control | Keychain: macOS permission popup (user must approve). GCloud: IAM service account. |
| Memory safety | Credentials stored in Go `[]byte`, zeroed after use via `memguard` or manual zeroing. Never converted to `string` (strings are immutable in Go, can't be wiped). |
| Log scrubbing | Credential fields excluded from all `slog` structured logging. Custom `LogValuer` implementation on credential types returns `[REDACTED]`. |
| Browser isolation | go-rod session created per-request, destroyed after content extraction. No persistent cookies, no shared browser profile. |
| Network isolation | Headless browser runs with a scoped proxy config — only the target domain is accessible. All other network requests blocked. |

#### Secrets Port Interface

```go
type SecretsManager interface {
    Store(ctx context.Context, key string, value []byte) error
    Retrieve(ctx context.Context, key string) ([]byte, error)
    Delete(ctx context.Context, key string) error
    List(ctx context.Context, prefix string) ([]string, error)
}

// Adapters:
// - secrets/keychain.go   (macOS `security` CLI)
// - secrets/gcloud.go     (existing, refactored to match interface)
```

---

### 3.4 Memory System Overhaul

#### Current State (v1)
- pgvector stores embeddings via Vertex AI `text-embedding-004`
- BM25 hybrid search (tsvector + vector fusion)
- Time decay scoring
- Single namespace

#### v2 Additions

```
┌──────────────────────────────────────────────────┐
│                 Memory System v2                  │
│                                                   │
│  ┌─────────────────┐    ┌──────────────────────┐  │
│  │   pgvector       │    │   Daily Notes        │  │
│  │   (semantic)     │    │   memory/YYYY-MM-DD  │  │
│  │                  │    │   .md                 │  │
│  │  - embeddings    │    │                      │  │
│  │  - BM25 hybrid   │    │  - human-readable    │  │
│  │  - time decay    │    │  - git-trackable     │  │
│  │  - diversity     │    │  - automatic daily   │  │
│  │    re-ranking    │    │    log of key events  │  │
│  └────────┬─────────┘    └──────────┬───────────┘  │
│           │                         │              │
│           └────────────┬────────────┘              │
│                        │                           │
│              ┌─────────▼──────────┐                │
│              │  Memory Curator    │                │
│              │  (periodic job)    │                │
│              │                    │                │
│              │  Promotes important│                │
│              │  daily note facts  │                │
│              │  → stable MEMORY.md│                │
│              │  or pgvector       │                │
│              │  "permanent"       │                │
│              │  namespace         │                │
│              └────────────────────┘                │
│                                                   │
│  ┌──────────────────────────────────────────────┐ │
│  │  Context Window Flush                         │ │
│  │                                               │ │
│  │  BEFORE compaction kicks in:                  │ │
│  │  1. Detect context usage > 80% threshold      │ │
│  │  2. Extract critical facts from conversation  │ │
│  │  3. Write to pgvector + daily notes           │ │
│  │  4. THEN compact context window               │ │
│  │                                               │ │
│  │  This prevents marathon tasks from losing     │ │
│  │  information during context compaction.       │ │
│  └──────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────┘
```

#### Diversity Re-ranking

When retrieving memories, the system currently can return near-duplicate snippets (e.g., the same fact stored across multiple sessions). V2 adds a post-retrieval diversity filter:

1. Retrieve top-k=20 candidates from pgvector.
2. Compute pairwise cosine similarity between all 20 results.
3. Greedily select results that are below a similarity threshold (0.92) from already-selected results.
4. Return top-5 diverse results.

This ensures the model sees 5 distinct pieces of context rather than 5 variations of the same memory.

---

### 3.5 Concurrent Task Execution

#### Design

```
┌──────────────────────────────────────────────────┐
│                 Task Scheduler                    │
│                                                   │
│  ┌────────────────┐    ┌────────────────────────┐ │
│  │  Priority Queue │    │  Worker Pool           │ │
│  │                 │    │  (configurable, max 5)  │ │
│  │  interactive    │    │                        │ │
│  │  ────────────── │    │  ┌──────┐ ┌──────┐    │ │
│  │  background     │    │  │ W-1  │ │ W-2  │    │ │
│  │  ────────────── │    │  │      │ │      │    │ │
│  │  scheduled      │    │  └──────┘ └──────┘    │ │
│  │                 │    │  ┌──────┐ ┌──────┐    │ │
│  └────────┬────────┘    │  │ W-3  │ │ W-4  │    │ │
│           │             │  │      │ │      │    │ │
│           ▼             │  └──────┘ └──────┘    │ │
│     dequeue next        │  ┌──────┐             │ │
│     → assign to         │  │ W-5  │             │ │
│       idle worker       │  │      │             │ │
│                         │  └──────┘             │ │
│                         └────────────────────────┘ │
└──────────────────────────────────────────────────┘
```

#### Priority Tiers

| Priority | Description | Example |
|----------|-------------|---------|
| 0 (highest) | Interactive — user just asked something | "What time is my meeting?" |
| 1 | Foreground task — user explicitly requested | "Research AR displays" |
| 2 | Background task — spawned by another task | Deep research source gathering |
| 3 (lowest) | Scheduled/heartbeat | "Check email every morning at 8am" |

Interactive queries always preempt background work. Each task gets its own goroutine, own `context.Context` (for cancellation), and its own conversation thread in memory.

#### Cancellation

```go
type Task struct {
    ID       string
    Priority int
    Cancel   context.CancelFunc  // calling this kills the goroutine
    Status   TaskStatus          // queued | running | done | failed | cancelled
}
```

User says "Z, cancel the research" → agent identifies the matching task by description → calls `task.Cancel()` → goroutine exits cleanly → status set to `cancelled`.

#### Rate Limiting

All tasks share a single Anthropic API key. Rate limits are global (TPM/RPM), not per-task. The task scheduler uses a shared `rate.Limiter` (Go's `golang.org/x/time/rate`) to ensure total API usage stays within limits. If a task is rate-limited, it backs off and retries — it does not block other tasks.

---

### 3.6 Dynamic Split-Pane Web UI

**Decision:** ADR-007 — Dynamic split-pane replacing fixed 3-panel layout

#### What's Being Removed

| Component | Status | Reason |
|-----------|--------|--------|
| PlannerPanel.tsx | DELETE | No separate planner. Chat IS the planner. |
| ExecutorPanel.tsx | DELETE | Execution happens in Code/Terminal/Research panes. |
| ObserverPanel.tsx | DELETE | No observer/critic model. Single brain. |
| HandoffAnimation.tsx | DELETE | No model-to-model handoff. |
| CriticBadge.tsx | DELETE | No critic. |

#### What's Being Added

The UI shifts from a fixed 3-panel layout to a dynamic pane system (think VS Code split editor + tmux).

```
┌─────────────────────────────────────────────────────────────────┐
│  ZBOT                                          [tasks] [settings]│
├─────────────────────────────────────────────────────────────────┤
│  ▶ [___________________________________________________] [⏎]   │
├───────────────────────────┬─────────────────────────────────────┤
│                           │                                     │
│   💬 Chat                 │   📝 Code                            │
│   (always present)        │   (opened on demand)                │
│                           │                                     │
│   User: Research AR       │   # ar_analysis.py                  │
│   displays for Z-Glass    │   import requests                   │
│                           │   from bs4 import...                │
│   ZBOT: I'll research     │                                     │
│   that. Opening a         │   [Run ▶]  [Save 💾]                │
│   research panel too...   │                                     │
│                           ├─────────────────────────────────────┤
│   Research: 23/50         │   🔬 Research                        │
│   sources gathered...     │   (opened on demand)                │
│                           │                                     │
│                           │   Sources: 23/50 gathered            │
│                           │   ██████████░░░░░ 46%               │
│                           │                                     │
├───────────────────────────┴─────────────────────────────────────┤
│  Active: research (46%) | code: idle | Cost: $0.12              │
└─────────────────────────────────────────────────────────────────┘
```

#### Pane Types

| Type | Description | Can Close? |
|------|-------------|------------|
| Chat | Main conversation, always present | No (minimize only) |
| Code | Syntax-highlighted editor + Docker sandbox run | Yes |
| Research | Deep research live progress + final report | Yes |
| Terminal | Direct shell access | Yes |
| File Viewer | Preview images, PDFs, markdown | Yes |
| Memory | Visual browser of pgvector contents | Yes |
| Web Preview | Rendered HTML preview | Yes |

#### Technical Stack

- **Split library:** `react-resizable-panels` or `allotment`
- **Pane state:** React Context + useReducer (shared event bus)
- **Communication:** Panes communicate through a `PaneManager` context. Chat can reference "the code in the code panel." Backend tracks which panes are open and routes output accordingly.
- **Persistence:** Layout saved to localStorage. Pane contents persisted to pgvector/filesystem.
- **Max visible panes:** 4 (beyond this becomes unusable on most screens).
- **Embedded:** Vite builds to `frontend/dist/`, Go embeds via `embed.FS`, served from the single binary.

---

## 4. Data Flow: End-to-End Request

```
1. User sends message via Telegram/Web UI/Slack
        │
2. Gateway adapter receives, authenticates (Telegram allowFrom whitelist)
        │
3. Agent core receives message
        │
4. Memory injector: semantic search pgvector → top-5 diverse memories injected
        │
5. System prompt assembled: persona + tools + pane context + injected memories
        │
6. LLM call (Sonnet 4.6 default)
        │
7. If tool_call in response:
   │   ├── web_search → Brave Search API → result appended → loop to step 6
   │   ├── run_code → Docker sandbox → stdout appended → loop to step 6
   │   ├── deep_research → spawn background task (Phase 1 Haiku, Phase 2 Sonnet)
   │   ├── fetch_url → HTTP client (SSRF prevention) → content appended → loop
   │   └── credentialed_fetch → Keychain/GCloud → go-rod → content appended → loop
        │
8. Response sent back through gateway to user
        │
9. Automatic memory save: (user_input, response) → pgvector + daily notes
        │
10. Audit log: tokens, cost, tools used, latency → Postgres
```

---

## 5. Security Architecture

### 5.1 Attack Surface Analysis

| Vector | Mitigation | Residual Risk |
|--------|-----------|---------------|
| Prompt injection via user input | Input sanitization layer (Sprint 9). Tool output never injected raw — structured JSON only. | Medium — LLM-level injection is an active research area. Defense in depth. |
| Credential exfiltration | Credentials never in LLM context. Fetched in Go, used in go-rod, zeroed immediately. Custom `LogValuer` redacts in all logs. | Low — credentials never touch the model. |
| Code execution escape | Docker sandbox: `--rm --network=none --memory=512m --cpus=1 --read-only --user=1000:1000` (ADR-003). | Low — standard Docker isolation. Not VM-level, but sufficient for personal agent. |
| SSRF via fetch_url | URL validation: block private IP ranges (10.x, 172.16.x, 192.168.x, 127.x, ::1). DNS rebinding prevention via pre-resolution. | Low. |
| Memory poisoning | Memory writes are agent-controlled (not raw user input). Memory search results are labeled as "recalled context" in the prompt, not presented as ground truth. | Medium — adversarial users could store misleading facts. |
| Web UI unauthorized access | Loopback only (127.0.0.1). No 0.0.0.0 binding. Remote access only via Tailscale. | Low. |

### 5.2 Credential Flow Security

```
NEVER IN:                      ALWAYS IN:
─────────                      ─────────
❌ LLM prompt/response         ✅ macOS Keychain (encrypted)
❌ pgvector memory              ✅ GCloud Secret Manager (encrypted)
❌ Postgres audit logs          ✅ Go process memory (zeroed after use)
❌ Application logs             ✅ go-rod browser session (destroyed after use)
❌ Error messages
❌ Docker container env vars
❌ Daily notes / MEMORY.md
```

---

## 6. Infrastructure

### 6.1 Deployment Target

| Component | Location |
|-----------|----------|
| ZBOT binary | Claudius Maximus (Mac Studio M3 Ultra) |
| PostgreSQL + pgvector | 34.28.163.109 (GCP, ziloss_memory DB) |
| Secrets (default) | macOS Keychain (local) |
| Secrets (alternative) | GCloud Secret Manager (ziloss project) |
| Search API | Brave Search API (remote) |
| LLM API | Anthropic API (remote) |
| Headless browser | go-rod / Chromium (local) |

### 6.2 Scaling Considerations

ZBOT is a **personal agent** — it serves one user (you). Scaling is not about serving more users, it's about:

1. **Concurrent tasks:** The goroutine pool (default 5 workers) handles this. Go handles thousands of goroutines natively.
2. **API rate limits:** Anthropic's rate limits are the bottleneck. The shared `rate.Limiter` ensures graceful degradation.
3. **Memory growth:** pgvector scales to millions of vectors. Daily notes are markdown files — trivially small. A curation job promotes important facts and can prune stale ones.
4. **Cost:** The tiered model strategy (Haiku → Sonnet → Opus) keeps daily costs under $5 for heavy usage.

---

## 7. Trade-Off Analysis

| Decision | Trade-off | Why We Accept It |
|----------|-----------|-----------------|
| Single brain (kill multi-model) | Lose specialized planning quality from GPT-4o | Sonnet 4.6 plans well enough. Eliminating handoff info loss is worth more than marginal planning quality. |
| Haiku for source gathering | Lower extraction quality vs. Sonnet | At 30-50 sources, Haiku's extraction is "good enough." Synthesis pass catches errors. 10-50x cheaper. |
| macOS Keychain default | Mac-only for default secrets | ZBOT runs on a Mac Studio. Cross-platform is a non-goal for v2. GCloud adapter covers cloud deploys. |
| Docker sandbox (not Firecracker) | Not VM-level isolation | Personal agent, single user. Docker constraints eliminate primary threat vectors. Revisit if threat model changes. |
| Max 4 visible panes | Can't show everything at once | Usability research shows >4 panes becomes counterproductive. Users can close/reopen panes. |
| pgvector (not Weaviate/Pinecone) | Less feature-rich than dedicated vector DBs | Already deployed, already works, runs in the same Postgres instance as workflow data. One fewer dependency. |

---

## 8. ADR Index

| ADR | Title | Status |
|-----|-------|--------|
| [ADR-001](adr/ADR-001-language-go.md) | Language: Go | Accepted |
| [ADR-002](adr/ADR-002-hexagonal-architecture.md) | Hexagonal Architecture (Ports & Adapters) | Accepted |
| [ADR-003](adr/ADR-003-docker-sandboxing.md) | Docker-per-Session Sandboxing | Accepted |
| [ADR-004](adr/ADR-004-single-brain.md) | Kill Multi-Model Orchestration → Single Brain | Accepted |
| [ADR-005](adr/ADR-005-deep-research-v2.md) | Deep Research v2: Haiku Gather → Sonnet Synthesize | Accepted |
| [ADR-006](adr/ADR-006-credential-storage.md) | Credential Storage: Keychain Default + GCloud Fallback | Accepted |
| [ADR-007](adr/ADR-007-dynamic-split-pane-ui.md) | Dynamic Split-Pane UI | Accepted |

---

## 9. Open Questions

1. **Opus escalation threshold tuning:** The complexity detection heuristics need real-world calibration. Plan to instrument Sonnet's performance on hard tasks for 2 weeks, then set thresholds based on data.
2. **Memory curation frequency:** Should the curator run daily, weekly, or on-demand? Starting with weekly, adjustable via config.
3. **go-rod browser pool:** Should credentialed scraping reuse browser instances per-domain (faster, session risk) or create fresh per-request (slower, safer)? Starting with fresh-per-request (safety first), may pool later if performance is a problem.
4. **Z-Glass channel integration:** The Halo glasses channel is specced but not scheduled. Likely Sprint G or later.

---

_Filed: ~/Desktop/Projects/zbot/docs/ZBOT_V2_ARCHITECTURE.md_
