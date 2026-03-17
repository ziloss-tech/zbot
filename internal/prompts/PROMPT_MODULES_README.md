# ZBOT Prompt Modules — Integration Guide

## What Was Built (Part 1 + Part 2 Research Integration)

### New Files Created
```
internal/prompts/
├── builder.go              — Composes modules into system prompts based on routing
├── claude_prompts.go       — UPDATED: base executor (identity + personality + tools + error recovery)
├── gpt_prompts.go          — UNCHANGED: planner + critic
├── memory_prompts.go       — NEW: when to read/write memory, structured write-back policy
├── reasoning_prompts.go    — NEW: Socratic gating (minimal/deep) + Aristotelian argument maps
├── research_prompts.go     — UNCHANGED: deep research pipeline
├── router_prompts.go       — NEW: request classification (for future Haiku router)
├── tool_control_prompts.go — NEW: execution modes, confirmation gates, drift prevention
└── verification_prompts.go — NEW: tiered self-check, evidence ledger, citation discipline
```

### What Each Module Does

| Module | Purpose | Token Cost | When Injected |
|--------|---------|------------|---------------|
| `ClaudeExecutorSystem` | Who ZBOT is, what tools it has, how it handles errors | ~800 | Always |
| `ReasoningModule` | Socratic questioning + Aristotelian argument structure | ~650 | Complex tasks, ambiguous requests |
| `MemoryPolicyModule` | When to search/save memory, write-back schema, hygiene | ~700 | Session start, memory-related tasks |
| `ToolControlModule` | CHAT/SAFE_AUTOPILOT/AUTOPILOT modes, confirmation gates | ~750 | Any task involving tools |
| `VerificationModule` | Tier 0-3 self-checks, evidence ledger, honest uncertainty | ~800 | High-stakes, low-confidence turns |
| `RouterSystem` | Classifies messages for the routing layer (runs on Haiku) | ~600 | Separate pre-processing step |

**Full load (all modules): ~3,700 tokens** — well within Claude's context budget.
**Typical turn (base + 2 modules): ~2,200 tokens.**
**Minimal turn (base only): ~800 tokens.**

## Integration — Three Phases

### Phase 1: Immediate (no code changes needed)
The existing line in `wire.go` still works:
```go
agentCfg.SystemPrompt = prompts.ClaudeExecutorSystem + skillRegistry.SystemPromptAddendum()
```
The updated `ClaudeExecutorSystem` already includes Part 1's anti-sycophancy, error recovery, and now has cleaner separation of concerns. The personality section is improved. This is live as soon as you rebuild.

### Phase 2: Use the Builder (one line change in wire.go)
Replace the system prompt assembly line with:
```go
profile := prompts.DefaultProfile()
// DefaultProfile: memory_policy + tool_control active, reasoning + verification on standby
agentCfg.SystemPrompt = prompts.BuildExecutorPrompt(profile, skillRegistry.SystemPromptAddendum())
```

For the chat paths (quick chat + persistent Claude chat), replace the system prompt assembly with:
```go
chatSystemPrompt := prompts.BuildChatPrompt(
    systemPrompt,                       // base chat prompt from wire.go
    skillRegistry.SystemPromptAddendum(),
    memContext,                          // injected memory facts
    false,                              // includeReasoning — set true for complex chats
)
```

### Phase 3: Wire the Router (requires new code)
Add a Haiku pre-processing step before the executor:
1. Send the user message + `RouterSystem` to Haiku
2. Parse the JSON response into a `PromptProfile` using `ProfileFromRouter()`
3. Build the executor prompt with that profile
4. Run the executor with the tailored prompt

This is the full state-machine pattern from the research. Router → compose prompt → execute → verify.

## Token Budget Summary

The research recommended 6,000-10,000 tokens across all prompt modules.
Current total across all files: ~4,550 tokens of active prompt content.
This is intentionally lean — every line earns its place.

## What's Different from Part 1

Part 1 gave us: anti-sycophancy, tool protocol (Search-Validate-Verify), error recovery.
These are now refined and integrated into the base executor prompt.

Part 2 added:
- **Modular composition** — prompts assembled per-turn, not monolithic
- **Socratic gating** — two concrete modes (minimal/deep) with decision rules
- **Argument maps** — structured claim validation, not just "chain of thought"
- **Memory as OS policy** — explicit triggers for read/write with structured categories
- **Execution modes** — CHAT/SAFE_AUTOPILOT/AUTOPILOT with deterministic gates
- **Adaptive verification** — 4 tiers matched to stakes, not one-size-fits-all
- **Evidence ledger** — machine-readable source tracking for every claim
- **Drift prevention** — explicit rules for staying on task during tool chains
- **Injection defense** — tool outputs are data, never instructions

## Rebuild Command
```bash
cd ~/Desktop/zbot && go build ./... && echo "BUILD OK"
```
