# ZBOT — Model Assignment Matrix
## Which model runs which brain region, and why

**Date:** March 19, 2026
**Updated after:** Model Arena testing + cost analysis session

---

## The Core Principle

Each brain region has different needs. Match the model to the job:
- **Speed-critical** → Groq (408 tok/sec on LPU hardware)
- **Cheap + smart** → DeepSeek V3.2 ($0.14/M, rivals GPT-4o quality)
- **Long autonomous runs** → Kimi K2 (300 tool calls no drift, auto-caching)
- **User-facing quality** → Claude Sonnet 4.6 (best reasoning + prose)
- **Bulk memory processing** → Gemini 2.5 Flash (1M context window)
- **Batch overnight** → Anthropic Batch API (50% off) or Kimi K2

---

## Model Assignment Table

| Brain Region | Model | Provider | Cost (in/out per M) | Why This Model |
|---|---|---|---|---|
| **Router** | Llama 4 Scout | Groq | $0.11 / $0.34 | Fastest classifier. 408 tok/sec. Classification is Scout's sweet spot. |
| **Frontal Lobe** (planning) | DeepSeek V3.2 | DeepInfra | $0.14 / $0.28 | Cheapest smart model. Great at JSON plan output. 90% cache discount. |
| **Planning Committee** (critic) | Gemini 2.0 Flash | Google/OR | $0.10 / $0.40 | Fast, good at catching edge cases. Different training = different blind spots. |
| **Cortex** (user-facing) | Claude Sonnet 4.6 | Anthropic | $3.00 / $15.00 | Best quality for what you actually see. Prompt caching = 90% off repeated context. |
| **Cortex** (overnight batch) | Kimi K2 | Moonshot/OR | $0.60 / $2.50 | 300 tool calls no drift. Auto-caching (75% off, zero config). Built for agents. |
| **Thalamus** (verification) | DeepSeek V3.2 | DeepInfra | $0.14 / $0.28 | Cheap enough to verify every non-trivial turn. Smart enough to catch logic errors. |
| **Hypothalamus** (sentinel) | DeepSeek V3.2 | DeepInfra | $0.14 / $0.28 | Background scan every 15 min. Not time-sensitive. Cheapest smart option. |
| **Memory Cortex** (batch) | Gemini 2.5 Flash | Google | $0.10 / $0.40 | 1M context window. Reads entire memory store in one call. No chunking needed. |
| **Research Planner** | DeepSeek V3.2 | DeepInfra | $0.14 / $0.28 | Structured decomposition into sub-questions. JSON output. |
| **Research Searcher** | Llama 4 Scout | Groq | $0.11 / $0.34 | Fast retrieval. Doesn't need deep reasoning, just search + tag sources. |
| **Research Extractor** | Kimi K2 | Moonshot/OR | $0.60 / $2.50 | Extract atomic claims from long docs. Good at structured extraction. |
| **Research Critic** | Gemini 2.0 Flash | Google/OR | $0.10 / $0.40 | Intentionally different provider from extractor. Independent verification. |
| **Research Synthesizer** | Claude Sonnet 4.6 | Anthropic | $3.00 / $15.00 | Best prose. Final user-facing output deserves the best model. |

---

## Cost Estimates

### Per-turn cost (typical interactive session)
| Component | Tokens | Cost |
|---|---|---|
| Router (Scout/Groq) | ~300 in, ~100 out | $0.00007 |
| Frontal Lobe (DS V3.2) | ~500 in, ~300 out | $0.00015 |
| Cortex (Sonnet, cached) | ~3000 in (90% cached), ~1000 out | ~$0.016 |
| Thalamus (DS V3.2) | ~800 in, ~200 out | $0.00017 |
| **Total per turn** | | **~$0.017** |

### Overnight autonomous run (e.g., GHL audit)
| Component | Calls | Cost |
|---|---|---|
| Planning committee | 1 | $0.001 |
| Kimi K2 Cortex (200 tool rounds) | 200 | ~$1.50 (with auto-cache) |
| Thalamus checks (every 5 rounds) | 40 | $0.02 |
| **Total overnight job** | | **~$1.52** |

### Background processes (monthly)
| Component | Frequency | Cost |
|---|---|---|
| Hypothalamus sentinel | Every 15 min, 12 hr/day | ~$0.60/month |
| Memory Cortex batch | Nightly | ~$1.20/month |
| **Total background** | | **~$1.80/month** |


---

## Why These Specific Models

### DeepSeek V3.2 — the utility player
Best quality-to-cost ratio in the market right now. $0.14/M input with 90% cache discounts.
Smart enough for planning, verification, and background analysis. Not the best at any one thing,
but the best VALUE for the supporting brain regions that run constantly.

### Kimi K2 — the overnight workhorse
The killer stat: 300 sequential tool calls without drift. No other model matches this for
autonomous long-running agent tasks. Auto-caching (75% off with zero config) means repeated
context is nearly free. MoE architecture (1T total, 32B active) keeps it efficient.
Built specifically for agentic use — trained on thousands of tool-use simulations across
hundreds of domains. This is the model you set loose at midnight.

### Claude Sonnet 4.6 — the quality brain
Best reasoning and prose for user-facing output. Prompt caching (90% off) makes it affordable
when the system prompt is stable (which it is — builder.go keeps the prefix consistent).
Batch API (50% additional off) for non-urgent work. Only used when output quality matters
to the human reading it.

### Groq Scout — the fast classifier
408 tok/sec. Sub-100ms time to first token. For routing and classification where the answer
is a small JSON object, speed matters more than depth. Scout on Groq hardware is unbeatable
for this specific job.

### Gemini 2.5 Flash — the bulk processor
1M token context window. Can read the entire memory store (hundreds of memories) in a single
call. Perfect for the Memory Cortex batch job where you need to see everything at once to
find patterns and contradictions. Also useful as the "different perspective" in the planning
committee — trained by Google, sees different patterns than Anthropic/DeepSeek models.

---

## API Keys Required

| Provider | Secret Name (GCP) | Endpoint |
|---|---|---|
| Anthropic | ANTHROPIC_API_KEY | api.anthropic.com/v1/messages |
| DeepInfra | (need to add) | api.deepinfra.com/v1/openai/chat/completions |
| Groq | groq-api-key | api.groq.com/openai/v1/chat/completions |
| OpenRouter | OPENROUTER_API_KEY | openrouter.ai/api/v1/chat/completions |
| Google | gemini-api-key | generativelanguage.googleapis.com/v1beta |
| Moonshot (Kimi) | (need to add) | api.moonshot.ai/v1 OR via OpenRouter |
| Together | together-api-key | api.together.xyz/v1/chat/completions |

### Keys already in GCP Secret Manager:
- ANTHROPIC_API_KEY ✅
- OPENROUTER_API_KEY ✅ (gives access to Kimi K2, DeepSeek, Gemini, Mistral, etc.)
- groq-api-key ✅
- gemini-api-key ✅
- together-api-key ✅
- OPENAI_API_KEY ✅

### Keys needed:
- DeepInfra API key (for direct DeepSeek V3.2 access — currently using via OpenRouter)
- Moonshot API key (for direct Kimi K2 access — currently using via OpenRouter)

Note: OpenRouter provides access to ALL of these models. For production, direct provider
keys are cheaper (no OpenRouter margin). For testing/arena, OpenRouter is convenient.

---

## Symbiotic Patterns (Triple Helix)

### Pattern 1: Draft → Critique → Refine
- Drafter: DeepSeek V3.2 or Kimi K2 (cheap, competent)
- Critic: Gemini Flash (different training, catches different blind spots)
- Refiner: Same as drafter (incorporates critique)
- **Cost:** ~$0.001 per cycle

### Pattern 2: Plan → Execute → Verify
- Planner: DeepSeek V3.2 (structured JSON decomposition)
- Executor: Claude Sonnet or Kimi K2 (depending on user-facing vs autonomous)
- Verifier: DeepSeek V3.2 (cheap, compares output vs plan)
- **Cost:** ~$0.02-0.05 per cycle (depending on executor)

### Pattern 3: Cheap Scout → Smart Finish
- Scout: Groq Scout or Flash-Lite (fast rough draft)
- Finisher: Claude Sonnet (polish to production quality)
- **Cost:** ~$0.01-0.02 per cycle

### Pattern 4: Dual Draft → Merge
- Draft A: DeepSeek V3.2
- Draft B: Kimi K2
- Merger: Claude Sonnet (best at synthesis)
- **Cost:** ~$0.02-0.03 per cycle

---

*Model assignments created March 19, 2026 — Ziloss Technologies*
*To be updated as new models release and arena testing reveals better combinations*
