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

### Thalamus (Oversight Engine)
- **Role**: Observes Cortex via event bus, answers user questions about ongoing work, suggests preparations, flags drift
- **Implementation**: `internal/webui/frontend/src/components/ThalamusPane.tsx` (frontend), backend endpoint TBD
- **Cost**: 5-15% overhead (lazy evaluation — only calls LLM when triggered)
- **Backend prompt** (injected when triggered):
  ```
  You are Thalamus, the oversight engine. You can see what
  Cortex is doing via its event log (below). You do NOT
  see raw tokens — only structured events.

  Your roles:
  1. Answer user questions about Cortex's work
  2. Flag drift: is Cortex still aligned with the plan?
  3. Suggest preparation: "while Cortex does X, should I prepare Y?"
  4. Intervene if Amygdala flags danger

  ## Cortex event log (last 20 events)
  [compact JSON array — ~500 tokens total]
  ```
- **Key behavior**: Reads event bus metadata (~50 tokens/event), NOT raw Cortex context

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

### Frontal Lobe (Executive Planner) — v0.2
- **Role**: Decides WHAT to do before Cortex decides HOW to do it
- **Implementation**: Not yet built
- **Cost**: ~200 tokens (cheap Haiku classifier)
- **Backend prompt** (planned):
  ```
  You are the executive planner. Given this user message,
  classify the intent and output a structured plan:
  {type: "chat" | "task" | "research" | "clarify"}
  {steps: [...]}
  {model_tier: "fast" | "default" | "advanced"}
  {needs_memory: true/false}
  {risk_level: "low" | "medium" | "high"}
  ```
- **Key behavior**: Separates planning from execution. Currently Cortex does both.

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
| `memory_loaded` | Hippocampus | N facts injected into context |
| `tool_called` | Cortex | Tool execution starting |
| `tool_result` | Cerebellum | Tool completed successfully |
| `tool_error` | Cerebellum | Tool execution failed |
| `turn_complete` | Cortex | Turn finished (tokens, cost, tools used) |
| `cost_update` | Cortex | Running cost total for session |
| `file_read` | Cerebellum | File accessed (for code mode UI) |
| `file_write` | Cerebellum | File modified (for code mode UI) |
| `web_search` | Cerebellum | Web search executed (for source tracker) |
| `fetch_url` | Cerebellum | URL scraped (for source tracker) |
| `confirm_needed` | Amygdala | Destructive op requires user confirmation |
| `security_flag` | Amygdala | Security concern detected |

## The Agent Loop (agent.go → Run())

```
1. User message arrives
2. Hippocampus: search memory for relevant facts (0 LLM tokens)
3. Build system prompt: base + temporal awareness + memory + skills
4. Attach 12 tool definitions
5. LOOP (up to MaxToolRounds=10):
   a. Send messages[] to Claude Sonnet 4.6
   b. If stop_reason == "end_turn" → break, return reply
   c. If stop_reason == "tool_use":
      - Amygdala: validate tool inputs (0 LLM tokens)
      - If destructive: require user confirmation
      - Cerebellum: execute tool(s)
      - Append tool results to messages[]
      - Event bus: emit tool_called + tool_result
      - Continue loop
6. Emit turn_complete event
7. Background: extract facts for Hippocampus (Haiku, ~100 tokens)
8. Return reply to user
```

## Cost Model

| Component | LLM Cost | When |
|-----------|----------|------|
| Cortex | 100% of cost | Every turn |
| Hippocampus | 0 | Database query |
| Thalamus | 5-15% extra | Only when triggered |
| Amygdala | 0 | Rule-based Go code |
| Cerebellum | 0 | HTTP/file/subprocess |
| Frontal Lobe (v0.2) | ~200 tokens | Per turn (Haiku) |
| Memory extraction | ~100 tokens | Background (Haiku) |

## File Map

```
cmd/zbot/wire.go               — Dependency injection, wires everything together
internal/agent/
  ports.go                      — All interfaces (LLMClient, MemoryStore, EventBus, etc.)
  agent.go                      — Core agent loop (Run method)
  eventbus.go                   — In-memory event bus implementation
  router.go                     — Model tier routing (Haiku/Sonnet/Opus)
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
  frontend/src/components/
    ChatPane.tsx                — Cortex chat interface
    ThalamusPane.tsx            — Thalamus oversight interface
    PaneManager.tsx             — Adaptive split-pane layout
    Sidebar.tsx                 — Navigation
```

## Design Principles

1. **Hexagonal architecture** — Core agent depends only on interfaces in ports.go. All adapters are swappable.
2. **Event-driven oversight** — Thalamus reads metadata (~50 tokens/event), not raw context. This keeps oversight cost at 5-15%, not 100%.
3. **Lazy evaluation** — Components only consume LLM tokens when there's a reason. Amygdala, Hippocampus, and Cerebellum are zero-LLM-cost.
4. **Non-blocking** — Event bus drops events for slow subscribers rather than blocking Cortex. The agent loop is never slowed by observers.
5. **Graceful degradation** — Every component works without Postgres (memory returns empty, metrics return zeros, audit is no-op).
