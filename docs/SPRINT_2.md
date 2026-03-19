# ZBOT Sprint 2 — Cognitive UI + Architecture Docs

## Date: 2026-03-19
## Assignee: Coworker
## Branch: public-release → push to public (ziloss-tech/zbot main)

---

## WHAT CHANGED SINCE LAST SPRINT

The multi-stage cognitive loop is now LIVE and VERIFIED. This is the biggest change:

### Before (Sprint 1)
```
User message → one big LLM call → reply
```

### After (Sprint 2 — NOW WORKING)
```
User message
  → Frontal Lobe plans (Haiku, ~$0.001)
  → Hippocampus loads memory (zero LLM)
  → Cortex executes with plan (Sonnet, main cost)
  → Hippocampus enriches mid-task (zero LLM)
  → Thalamus verifies reply (Haiku, ~$0.001)
  → Reply sent (or revision loop if Thalamus rejects)
```

Verified test results:
- Simple chat ("What is 2+2?"): plan=chat, verification=none, instant reply "4."
- Research query: plan=research, verification=basic, Thalamus REJECTED at 35% confidence (caught hallucination)
- Code task: plan=code, verification=basic, Thalamus APPROVED at 92% confidence
- Complex multi-step: plan=research/complex, verification=thorough, Thalamus REJECTED at 30% (caught fabricated architecture details)

### New files you need to know about
```
internal/agent/cognitive.go     — planTask, enrichMemory, verifyReply implementations
COWORKER_START_HERE.md          — Previous sprint onboarding (now outdated — use THIS file)
```

### New event types (defined in ports.go)
```
plan_start      — Frontal Lobe begins planning
plan_complete   — Plan ready (includes type, complexity, steps, verification level)
verify_start    — Thalamus begins verification
verify_complete — Verification done (includes confidence, issues, approved/rejected)
memory_enrich   — Mid-task Hippocampus found related memories
```

---

## SPRINT 2 TASKS

### TASK 1: Show cognitive stages in the chat activity indicator
**Priority: HIGH — this is the main user-visible change**
**Files:** internal/webui/frontend/src/components/ChatPane.tsx

The ChatPane already shows tool events (web_search, write_file) in a live activity strip during chat. Now it needs to show cognitive stage events too.

When events stream in:
- `plan_start` → show "Frontal Lobe planning..."
- `plan_complete` → show "Plan: {type} ({complexity}, {steps} steps)" for 2 seconds
- `verify_start` → show "Thalamus verifying..."
- `verify_complete` + approved → show "✓ Verified ({confidence}%)" in green
- `verify_complete` + rejected → show "Revision needed" in amber, then tool events continue
- `memory_loaded` with `detail.stage === "enrichment"` → show "Memory: found {count} related facts" (NOTE: same event type as initial memory load, distinguished by the `stage` field in detail)

The event data is already streaming via the useEventBus hook. You just need to render these new event types in the activity indicator.

**Example UI flow for a research query:**
```
[Frontal Lobe planning...] → [Plan: research (3 steps)] →
[web_search] → [scrape_page] →
[Thalamus verifying...] → [Revision needed — 35% confidence] →
[Cortex revising...] → [Turn complete]
```

### TASK 2: Show cognitive summary in the Thalamus pane
**Priority: HIGH**
**Files:** internal/webui/frontend/src/components/ThalamusPane.tsx

The ThalamusPane currently only works when the user types a question. It should ALSO show the automatic verification results from the cognitive loop.

When verify_complete events come through the event bus:
- If approved: show a green card "Verified ✓ (confidence: {n}%)"
- If rejected: show an amber card with the issues list and the suggestion sent back to Cortex
- Always show the plan_complete event at the top: "Plan: {type}, {complexity}, verification: {level}"

This makes Thalamus proactive — the user sees verification results without asking.

Wire ThalamusPane to the real event bus via the useEventBus hook. Remove the fake event generation that currently watches `workflowState.toolCalls` and `workflowState.phase` (lines ~80-118 in ThalamusPane.tsx). This fake event logic is ONLY consumed by ThalamusPane — removing it won't break anything else. The ThalamusPane still receives `workflowState` as a prop from PaneManager but should stop generating synthetic events from it. The manual Thalamus Q&A (user types a question → /api/thalamus) should continue working — that uses `workflowState.agentTokens` and `workflowState.goal` which are separate from the fake events.

### TASK 3: Update ARCHITECTURE.md
**Priority: MEDIUM**
**File:** docs/ARCHITECTURE.md

The architecture doc still says "Frontal Lobe — v0.2, not yet built." It needs to reflect the current state:

Updates needed:
- Frontal Lobe section: update from "planned" to "LIVE". Reference cognitive.go.
- Thalamus section: add that it now runs automatic Socratic verification (verifyReply in cognitive.go), not just user-triggered oversight
- Agent loop section: update to show the 5-stage cognitive loop, not the old single-call loop
- Event types table: add plan_start, plan_complete, verify_start, verify_complete, memory_enrich
- Cost model: Frontal Lobe is now ~$0.001/turn (Haiku), Thalamus verification is ~$0.001/turn (Haiku)
- File map: add cognitive.go, update COWORKER_START_HERE.md reference

### TASK 4: Dotenv loader
**Priority: LOW**
**File:** cmd/zbot/wire.go

Currently requires `set -a && source .env && set +a` before running. Add a simple dotenv loader at startup:
- Read `.env` file if it exists
- Parse KEY=VALUE lines (skip comments, blank lines)
- Set as env vars only if not already set (don't override explicit env vars)
- No external dependency — just Go stdlib file I/O

### TASK 5: Local metrics tracking (no Postgres)
**Priority: LOW**
**Files:** internal/webui/api_handlers.go, cmd/zbot/wire.go

The metrics strip shows all zeros because there's no Postgres. Add simple in-memory counters:
- Total tokens (input + output) across all turns
- Total cost USD
- Turn count
- These reset on restart (that's fine for now)

Cost is ALREADY calculated in agent.go (line ~319): `costUSD = float64(output.InputTokens)*inputCost + float64(output.OutputTokens)*outputCost` using the ModelTierCost() lookup table in router.go. It's also emitted in the turn_complete event detail (`cost_usd` field). The coworker does NOT need to derive cost from a price table — just accumulate the `cost_usd` value from each turn_complete event.

Implementation: add an in-memory struct in api_handlers.go (or a new metrics.go) with atomic counters. Increment on each turn_complete. Serve via the existing /api/metrics endpoint.

---

## DEFINITION OF DONE

1. `go build ./...` passes
2. `npx vite build` passes (in internal/webui/frontend)
3. Send "What is 2+2?" — activity strip shows "Frontal Lobe planning..." → "Plan: chat (simple)"
4. Send "Search the web for latest AI news" — activity strip shows full cognitive flow including "Thalamus verifying..."
5. Thalamus pane auto-shows verification results without user interaction
6. ARCHITECTURE.md reflects the live 5-stage cognitive loop
7. Commit and push: `git diff --stat  # sanity check — make sure only expected files changed
git push public public-release:main`

## DO NOT

- Change agent.go Run() loop (it works — cognitive loop is verified)
- Change cognitive.go (planning + verification logic is working)
- Change thalamus_handler.go (the manual Thalamus Q&A still works alongside auto-verification)
- Change the system prompt in wire.go
- Modify the event bus or SSE handler
- Change event type definitions in ports.go (Tasks 1 and 2 depend on the existing event shape)
- Add npm dependencies without checking bundle size

## KEY FILES REFERENCE

```
internal/agent/agent.go         — 5-stage Run() loop
internal/agent/cognitive.go     — planTask, enrichMemory, verifyReply
internal/agent/ports.go         — EventBus interface + all event types
internal/agent/eventbus.go      — In-memory ring buffer implementation
internal/webui/events_handler.go — SSE endpoint /api/events/:sessionID
internal/webui/chat_stream_handler.go — POST /api/chat/stream
internal/webui/thalamus_handler.go — POST /api/thalamus (manual Q&A)
internal/webui/frontend/src/
  hooks/useEventBus.ts          — React hook consuming SSE events
  components/ChatPane.tsx       — Cortex chat (EDIT: add cognitive event rendering)
  components/ThalamusPane.tsx   — Thalamus pane (EDIT: add auto-verification display)
  components/PaneManager.tsx    — Split pane layout
  components/MetricsStrip.tsx   — Top bar metrics (EDIT: wire in-memory counters)
docs/ARCHITECTURE.md            — System architecture doc (EDIT: update to current state)
cmd/zbot/wire.go                — DI wiring (EDIT: add dotenv loader)
```

## GIT

```bash
cd ~/Desktop/Projects/zbot
git checkout public-release
# ... make changes ...
git add -A && git commit -m "feat: cognitive UI + architecture update"
git diff --stat  # sanity check — make sure only expected files changed
git push public public-release:main
```
