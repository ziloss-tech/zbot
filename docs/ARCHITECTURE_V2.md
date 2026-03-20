# ZBOT — Complete Cognitive Architecture
## Master Reference Document

**Last Updated:** March 19, 2026
**Status:** Design complete, partial implementation
**Sprint docs:** GHL_AGENT_SPRINTS.md, MEMORY_CORTEX_SPEC.md, ROADMAP.md

---

## Philosophy

**Socratic Method** — Before committing to any conclusion, identify ambiguity. Two modes:
- Minimal: identify the single most important ambiguity, state assumption, proceed
- Deep: 3-7 subquestion decomposition, each tagged with resolution path

**Aristotelian Logic** — Every conclusion traces to premises. Premises tagged by source
(memory, tool, user, assumed). If a premise is unverified, the conclusion is flagged.
No vibes-based reasoning. No confident nonsense. No assumption laundering.

**Triple Helix** — Multiple cheap models spiral upward in quality. Drafter → Critic → Verifier.
Each model sees different blind spots. The output improves with each pass.

---

## Brain Regions + Model Assignments

| Region | Role | Model | Provider | Cost | Status |
|--------|------|-------|----------|------|--------|
| Router | Classify task, set profile | Llama 4 Scout | Groq | $0.11/M | Prompt written, not called from agent.go |
| Frontal Lobe | Plan task approach | DeepSeek V3.2 | DeepInfra | $0.14/M | Code exists, needs cheapLLM wired |
| Planning Committee | Multi-model deliberation | DS V3.2 + Gemini Flash | DeepInfra + Google | ~$0.0005/plan | Demonstrated, not formalized |
| Memory Cortex | Proactive thought packages | Gemini 2.5 Flash (batch) | Google | ~$0.04/night | Spec written, not built |
| Hippocampus | Runtime package selection | pgvector (keyword match) | Local DB | $0/call | Interface exists, needs packages |
| Cortex (realtime) | Main reasoning + tools | Claude Sonnet 4.6 | Anthropic | $3/$15M (90% cache) | Fully wired, working |
| Cortex (overnight) | Autonomous long-run tasks | Kimi K2 | Moonshot | $0.60/$2.50M (75% auto-cache) | Not wired yet |
| Thalamus | Verify logic before respond | DeepSeek V3.2 | DeepInfra | $0.14/M | Code exists, needs cheapLLM |
| Hypothalamus | Background sentinel | DeepSeek V3.2 | DeepInfra | $0.14/M (every 15 min) | Conceptual design only |
| Stall Recovery | Override hesitant Cortex | Main LLM + tools | Anthropic | Same as Cortex | Code wired, needs plan to trigger |

### Monthly Cost Estimate (12 hrs/day autonomous)
- Planning + verification: ~$2/month
- Background sentinel + batch: ~$2/month
- Memory Cortex nightly batch: ~$1.50/month
- Cortex (varies by usage, cached): usage-dependent
- **Total overhead: ~$5.50/month for the entire supporting brain**

---

## Cognitive Pipeline (per-turn flow)

```
User message arrives
  │
  ├─ Stage 0: ROUTER (Groq Scout, ~$0.001)
  │  Classify → set socratic_mode, model_tier, tool_subset, execution_mode
  │
  ├─ Stage 1: FRONTAL LOBE / PLANNING COMMITTEE (~$0.0005)
  │  Option A: Single cheap model produces TaskPlan JSON
  │  Option B: DeepSeek drafts plan → Gemini critiques → deterministic merge
  │
  ├─ Stage 2: MEMORY CORTEX (zero cost at runtime)
  │  matchPackages(userMessage, allPackages) — keyword/embedding on LABELS only
  │  Inject Priority 0 (always) + Priority 1 (matched) thought packages
  │  No database query. No LLM call. <1ms.
  │
