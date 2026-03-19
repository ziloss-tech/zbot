# ZBOT Architecture — Cognitive Loop

## Overview

ZBOT uses a brain-region naming convention where each component maps to a specific cognitive function. This isn't metaphor — each region has distinct execution characteristics, cost profiles, and backend prompts.

## Brain Regions

### Cortex (Primary Engine)
- **Role**: Planning, reasoning, executing, self-critiquing
- **Implementation**: `internal/agent/agent.go` → `Run()` method
- **Cost**: Main cost center — every user turn runs through Cortex
- **Model**: Claude Sonnet 4.6 (default), escalates to Opus for complex tasks
- **Backend prompt**: `internal/prompts/claude_prompts.go` → `ClaudeExecutorSystem`
- **Key behavior**: Agentic loop — calls LLM → if tool_use, execute tools → loop back → if end_turn, return reply. Up to 10 rounds per turn.

### Hippocampus (Memory)
- **Role**: Persistent memory across conversations
- **Implementation**: `internal/memory/pgvector.go`
- **Cost**: Zero LLM tokens (pure database query)
- **Backend prompt injection**: Memory facts are injected into Cortex's system prompt:
  ```
  ## Relevant Memory
  - User prefers concise output (saved: 2026-03-10)
  - ZBOT repo is at ziloss-tech/zbot (saved: 2026-03-17)
  ```
- **Key behavior**: Hybrid BM25 + vector search with time decay and diversity re-ranking (0.92 cosine threshold)

### Thalamus (Oversight Engine) — LIVE
- **Role**: Automatic Socratic verification of every Cortex reply + user-triggered Q&A about ongoing work
- **Implementation**: `internal/agent/cognitive.go` → `verifyReply()` (backend), `internal/webui/frontend/src/components/ThalamusPane.tsx` (frontend), `internal/webui/thalamus_handler.go` (manual Q&A endpoint)
- **Cost**: ~$0.001/turn (Haiku) for auto-verification + optional user-triggered Q&A (same cost model as before)
- **Key behavior**: Runs automatic Socratic verification on every Cortex reply as Stage 5 of the cognitive loop. Uses Haiku to check for hallucination, drift, and confidence. Rejects replies below confidence threshold and sends revision suggestions back to Cortex. Also supports manual Q&A — user types a question, Thalamus reads event bus metadata to answer.
- **Events emitted**: `verify_start`, `verify_complete` (with `approved`, `confidence`, `issues`, `suggestion` in detail)

### Amygdala (Safety/Failsafe)
- **Role**: Threat detection, cost monitoring, drift detection
- **Implementation**: `internal/security/` (validate.go, sanitize.go, confirm.go)
- **Cost**: Zero LLM tokens (pure rule-based Go code)
- **Key behavior**: Scans every event on the bus for dangerous patterns (destructive ops, cost overruns, SSRF attempts, prompt injection). Triggers Thalamus when something is flagged.

### Cerebellum (Tool Execution)
- **Role**: Mechanical execution of tools
- **Implementation**: `internal/tools/` (web_search, file ops, code runner, etc.)
- **Cost**: Zero LLM tokens (HTTP calls, file I/O, subprocess execution)
- **Key behavior**: Cortex decides what tools to call, Cerebellum does the work. Results flow back through the event bus.

### Frontal Lobe (Executive Planner) — LIVE
- **Role**: Decides WHAT to do before Cortex decides HOW to do it (Stage 1 of cognitive loop)
- **Implementation**: `internal/agent/cognitive.go` → `planTask()`
- **Cost**: ~$0.001/turn (Haiku classifier)
- **Key behavior**: Classifies every user message into a structured TaskPlan before Cortex executes. Outputs type (chat/task/research/code/clarify), complexity, step count, recommended model tier, and verification level. Separates planning from execution — Cortex receives the plan and focuses on HOW.
- **Events emitted**: `plan_start`, `plan_complete` (with `type`, `complexity`, `steps`, `verification` in detail)

## Event Bus

The event bus (`internal/agent/eventbus.go`) is the nervous system connecting all brain regions.

### Architecture
- **Interface**: `EventBus` in `ports.go` (Subscribe, Emit, Recent, Unsubscribe)
- **Implementation**: `MemEventBus` — in-memory ring buffer, per-session, thread-safe
- **Capacity**: 200 events per session, non-blocking fan-out
- **Consumers**: Thalamus (oversight), Amygdala (safety), WebUI (real-time display)

### Event Types
| Event | Source | Description |
|-------|--------|-------------|
| `turn_start` | Cortex | New user turn begins |
| `plan_start` | Frontal Lobe | Planning stage begins |
| `plan_complete` | Frontal Lobe | Plan ready (detail: type, complexity, steps, verification) |
| `memory_loaded` | Hippocampus | N facts injected into context (initial load or mid-task enrichment via `detail.stage`) |
| `tool_called` | Cortex | Tool execution starting |
| `tool_result` | Cerebellum | Tool completed successfully |
| `tool_error` | Cerebellum | Tool execution failed |
| `verify_start` | Thalamus | Automatic Socratic verification begins |
| `verify_complete` | Thalamus | Verification done (detail: approved, confidence, issues, suggestion) |
| `memory_enrich` | Hippocampus | Mid-task enrichment found related memories |
| `turn_complete` | Cortex | Turn finished (detail: tokens, cost_usd, tools used) |
| `cost_update` | Cortex | Running cost total for session |
| `file_read` | Cerebellum | File accessed (for code mode UI) |
| `file_write` | Cerebellum | File modified (for code mode UI) |
| `web_search` | Cerebellum | Web search executed (for source tracker) |
| `fetch_url` | Cerebellum | URL scraped (for source tracker) |
| `confirm_needed` | Amygdala | Destructive op requires user confirmation |
| `security_flag` | Amygdala | Security concern detected |

## The Agent Loop — 5-Stage Cognitive Loop (agent.go → Run())

```
Stage 1 — Frontal Lobe (planning):
  - planTask() classifies user message via Haiku (~$0.001)
  - Outputs TaskPlan: type, complexity, steps, model tier, verification level
  - Events: plan_start → plan_complete

Stage 2 — Hippocampus (initial memory load):
  - Search memory for relevant facts (0 LLM tokens)
  - Build system prompt: base + temporal awareness + memory + skills + plan
  - Event: memory_loaded

Stage 3 — Cortex (execution):
  - Attach tool definitions
  - LOOP (up to MaxToolRounds=10):
    a. Send messages[] to Claude Sonnet 4.6 (or tier from plan)
    b. If stop_reason == "end_turn" → break, return reply
    c. If stop_reason == "tool_use":
       - Amygdala: validate tool inputs (0 LLM tokens)
       - If destructive: require user confirmation
       - Cerebellum: execute tool(s)
       - Append tool results to messages[]
       - Event bus: emit tool_called + tool_result
       - Continue loop

Stage 4 — Hippocampus (mid-task enrichment):
  - enrichMemory() searches memory based on tool results (0 LLM tokens)
  - Injects additional relevant facts mid-task
  - Event: memory_loaded (detail.stage = "enrichment")

Stage 5 — Thalamus (verification):
  - verifyReply() runs Socratic check via Haiku (~$0.001)
  - If APPROVED → emit turn_complete, return reply
  - If REJECTED → emit verify_complete (with issues), loop back to Stage 3 for revision
  - Events: verify_start → verify_complete

Background: extract facts for Hippocampus (Haiku, ~100 tokens)
```

## Cost Model

| Component | LLM Cost | When |
|-----------|----------|------|
| Cortex | Main cost center | Every turn (Sonnet default, Opus for complex) |
| Hippocampus | 0 | Database query (initial + enrichment) |
| Thalamus (auto) | ~$0.001/turn | Every turn (Haiku verification) |
| Thalamus (manual Q&A) | 5-15% extra | Only when user asks |
| Amygdala | 0 | Rule-based Go code |
| Cerebellum | 0 | HTTP/file/subprocess |
| Frontal Lobe | ~$0.001/turn | Every turn (Haiku classifier) |
| Memory extraction | ~100 tokens | Background (Haiku) |

## File Map

```
cmd/zbot/wire.go               — Dependency injection, wires everything together (+ dotenv loader)
internal/agent/
  ports.go                      — All interfaces (LLMClient, MemoryStore, EventBus, etc.) + event types
  agent.go                      — 5-stage cognitive Run() loop
  cognitive.go                  — planTask (Frontal Lobe), enrichMemory (Hippocampus), verifyReply (Thalamus)
  eventbus.go                   — In-memory event bus implementation
  router.go                     — Model tier routing (Haiku/Sonnet/Opus) + ModelTierCost()
internal/prompts/
  claude_prompts.go             — System prompts (ClaudeExecutorSystem, etc.)
  reasoning_prompts.go          — Reasoning module (injected for complex tasks)
  memory_prompts.go             — Memory policy module
  verification_prompts.go       — Verification module (for high-stakes tasks)
internal/llm/
  anthropic.go                  — Claude API adapter
  openai.go                     — OpenAI-compatible adapter (Ollama, Together, Groq)
  haiku.go                      — Haiku client for cheap background tasks
internal/memory/
  pgvector.go                   — pgvector semantic search
  curator.go                    — Background memory promotion
  daily_notes.go                — Markdown daily notes
  flusher.go                    — Context window flush before compaction
internal/tools/                 — All tool implementations
internal/security/              — Amygdala: validation, sanitization, confirmation
internal/webui/
  api_handlers.go               — HTTP API handlers (metrics with in-memory counters)
  chat_stream_handler.go        — POST /api/chat/stream (relays event bus as SSE)
  events_handler.go             — GET /api/events/:sessionID (SSE endpoint)
  thalamus_handler.go           — POST /api/thalamus (manual Q&A)
  frontend/src/
    hooks/useEventBus.ts        — React hook consuming SSE events
    components/
      ChatPane.tsx              — Cortex chat (renders cognitive events in activity strip)
      ThalamusPane.tsx           — Thalamus pane (auto-verification display + event bus)
      PaneManager.tsx           — Adaptive split-pane layout
      MetricsStrip.tsx          — Top bar metrics (wired to in-memory counters)
      Sidebar.tsx               — Navigation
SPRINT_2.md                     — Current sprint onboarding doc
```

## Design Principles

1. **Hexagonal architecture** — Core agent depends only on interfaces in ports.go. All adapters are swappable.
2. **Event-driven oversight** — Thalamus reads metadata (~50 tokens/event), not raw context. This keeps oversight cost at 5-15%, not 100%.
3. **Lazy evaluation** — Components only consume LLM tokens when there's a reason. Amygdala, Hippocampus, and Cerebellum are zero-LLM-cost.
4. **Non-blocking** — Event bus drops events for slow subscribers rather than blocking Cortex. The agent loop is never slowed by observers.
5. **Graceful degradation** — Every component works without Postgres (memory returns empty, metrics return zeros, audit is no-op).
