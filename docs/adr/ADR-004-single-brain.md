# ADR-004: Kill Multi-Model Orchestration → Single Brain

**Status:** Accepted
**Date:** 2026-03-16
**Supersedes:** Sprint 11 Dual Brain architecture

## Context

ZBOT v1 used a three-model orchestration pattern inherited from 2024-era agent design:

1. **GPT-4o Planner** — decomposes goals into task graphs
2. **Claude Sonnet Executor** — executes each task using tools
3. **GPT-4o Critic** — reviews executor output, approves or requests retry

This created three separate context windows with information lost at each handoff. The planner couldn't see executor tool output. The critic couldn't see the planner's reasoning. Orchestration code (handoff serialization, retry loops, status tracking) became the largest source of bugs.

Meanwhile, models improved. Claude Sonnet 4.6 can plan, execute, and self-critique in a single pass with quality comparable to the three-model pipeline — and it does so with full context visibility.

## Decision

**Replace the three-model pipeline with a single-brain architecture.**

- **Default model:** Claude Sonnet 4.6 for all tasks (planning, execution, self-critique).
- **Escalation model:** Claude Opus 4.6 for genuinely complex tasks (user-requested, or auto-detected via complexity heuristics).
- **Bulk model:** Claude Haiku 4.5 for cheap parallel work (source gathering in deep research).

The model router selects the appropriate tier based on a `ModelHint` field in the request, not by calling separate Planner/Executor/Critic interfaces.

## Consequences

### Positive

- **No handoff information loss.** One model sees the full context: the goal, its own plan, tool outputs, and previous attempts.
- **Simpler codebase.** Remove `PlannerClient`, `CriticClient`, `ExecutorClient` interfaces. Remove orchestration state machine. Remove handoff serialization. ~2,000 lines of code deleted.
- **Lower latency.** One LLM call instead of three sequential calls per task step.
- **Lower cost.** Sonnet is cheaper than GPT-4o + Sonnet + GPT-4o combined.
- **Easier debugging.** One conversation thread to inspect, not three interleaved streams.

### Negative

- **Loss of specialized planning.** GPT-4o's planning may have been marginally better for certain task decomposition patterns. Mitigated by Sonnet 4.6's improved reasoning capabilities.
- **No built-in quality gate.** The critic loop provided automatic retry on bad output. Mitigated by Sonnet's self-critique capability and the option for user-initiated retry.
- **Vendor concentration.** All three tiers are now Anthropic. Mitigated by the hexagonal architecture — the `LLMClient` port can be re-implemented for any provider without touching core logic.

## Code Changes

### Delete

- `internal/planner/planner.go` — GPT-4o planning logic
- `internal/prompts/gpt_prompts.go` — GPT planner/critic system prompts
- `internal/webui/frontend/src/components/PlannerPanel.tsx`
- `internal/webui/frontend/src/components/ExecutorPanel.tsx`
- `internal/webui/frontend/src/components/ObserverPanel.tsx`
- `internal/webui/frontend/src/components/HandoffAnimation.tsx`
- `internal/webui/frontend/src/components/CriticBadge.tsx`

### Modify

- `internal/agent/ports.go` — Collapse three LLM interfaces into one `LLMClient`
- `internal/agent/agent.go` — Simplify agent loop to single-model pattern
- `internal/workflow/orchestrator.go` — Remove critic review step, simplify task execution
- `cmd/zbot/main.go` — Update dependency injection wiring

### Add

- Model router logic in the Anthropic adapter: select Haiku/Sonnet/Opus based on `ModelHint`
- Complexity detection heuristic (for auto-escalation to Opus)
