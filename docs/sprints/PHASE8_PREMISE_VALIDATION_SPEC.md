# ZBOT Memory Overhaul — Phase 8 SPEC (NOT YET BUILT)
## Premise Validation + Deeper Self-Analysis
**Status:** Design notes — implement unless better path found through testing
**Date:** March 29, 2026
**Trigger:** Auditor caught Cortex overclaiming during timezone tool creation test

---

## The Problem

Current Thalamus/Auditor checks the ANSWER after Cortex generates it.
But it doesn't check the PREMISE before Cortex starts working.

Example failure modes:
- User: "What time did the Broncos win the Super Bowl this year?"
  (They didn't — Cortex shouldn't answer, it should challenge the premise)
- User: "Update the workflow we migrated yesterday"
  (Maybe it was 3 weeks ago — memory staleness could mislead Cortex)
- User: "Run the EnabledSync on the Phoenix tab"
  (Maybe the tab name changed — should verify before executing)

## Proposed Architecture: Pre-Flight Check

### Layer 0: Premise Validator (runs BEFORE Frontal Lobe)

Position in the Pantheon pipeline:
```
Router → PREMISE VALIDATOR → Frontal Lobe → Cortex → Auditor/Thalamus
```

The Premise Validator is a cheap LLM call (~$0.001) that asks:
1. Does this question assume something that may not be true?
2. Is the user referencing something from memory — is that memory current?
3. Does this request involve a destructive action on stale data?


### Decision Matrix

| Premise Check Result | Action |
|---|---|
| Premise is valid + current | Proceed normally |
| Premise assumes false fact | Challenge before answering: "Actually, X didn't happen..." |
| Premise references stale memory | Flag: "I remember X but that was N days ago — verifying..." |
| Premise involves destructive action on uncertain data | HALT + confirm with user |
| Premise is ambiguous | Clarify before proceeding |

### What "Source of Truth" Means

Jeremy's hierarchy (from conversation):
1. **Reality** — observable, verifiable facts (default source of truth)
2. **Small window for non-reality** — some questions are hypothetical,
   creative, or speculative and that's OK
3. **Memory** — what ZBOT remembers, but memory can be stale or wrong
4. **User's assertion** — trust but verify when it contradicts memory

The Premise Validator should identify which category the question falls
into and validate accordingly. A factual question gets reality-checked.
A hypothetical question gets a pass. A memory-dependent question gets
a freshness check.

---

## Self-Analysis Loop (the "Conscience")

### Current State (what works today)
- Auditor checks answers for logical soundness
- Phase 5 captures lessons from rejections
- Phase 6 flags stale information
- These are all POST-generation checks

### What's Missing: PRE-generation self-awareness
ZBOT should ask itself before every response:
1. "Am I about to use information I'm not confident about?"
2. "Is there a tool I should use to verify this before answering?"
3. "Have I made a similar mistake before?" (lesson lookup)
4. "Is this the kind of question where I tend to overclaim?"


### Implementation Sketch (if we build this)

```go
// PremiseCheck runs before Frontal Lobe planning.
type PremiseCheck struct {
    IsValid      bool     // premise appears factually sound
    Challenges   []string // specific assumptions to question
    StaleRefs    []string // memory references that may be outdated
    Confidence   float64  // 0-1, how confident in the premise
    SuggestVerify bool    // should Cortex verify before answering?
    Category     string   // "factual", "hypothetical", "memory-dependent", "action"
}

// In agent.go, between Router and Frontal Lobe:
func (a *Agent) validatePremise(ctx context.Context, query string,
    facts []Fact, pkgs []PackageMatch, lessons []Lesson) *PremiseCheck {
    // 1. Check if query assumes facts that contradict memory
    // 2. Check if referenced memories are stale (Phase 6 freshness)
    // 3. Check if similar premises were wrong before (Phase 5 lessons)
    // 4. Classify: factual vs hypothetical vs action
    // 5. If action + uncertain → flag for confirmation
}
```

Cost: One cheap LLM call ($0.001) using the same prompt pattern as
Router/Frontal Lobe. Could also be done as keyword heuristics for
zero cost (check if query contains past-tense verbs about events,
check if referenced entities exist in memory, etc.)

---

## The Bigger Picture: Why This Matters for the Axamy Pitch

Axamy/Composio: "Here's your data, trust us"
- No premise validation
- No self-analysis
- No lesson learning
- No staleness awareness
- No conscience

ZBOT after Phase 8:
- Checks its own assumptions before answering
- Flags when its memory might be wrong
- Learns from every mistake
- Knows what it doesn't know
- Asks for verification before destructive actions

This is the difference between a tool and an agent you can actually
trust with your business. The conscience is what makes it safe to
let ZBOT run overnight, check your QuickBooks, and text you a summary.
Without it, you'd need to babysit every action.

---

## Testing Plan (when we build this)

Plant these test cases:
1. "What time did the Broncos win the Super Bowl this year?" → should challenge
2. "Update the GHL workflow we migrated yesterday" → should check if "yesterday" is accurate
3. "Delete all contacts in the Esler account" → should HALT (destructive + uncertain)
4. "Write me a poem about dragons" → should pass through (hypothetical, creative)
5. "What's the current balance in QuickBooks?" → should use tool, not memory

---

## Decision: Build or Wait?

Arguments for building now:
- Auditor already caught an issue in first test (overclaiming)
- The pattern will only get more important as ZBOT takes more actions
- Cheap to implement (~200 lines, one more LLM call)

Arguments for waiting:
- Phases 1-7 just shipped, haven't been tested in production yet
- The Auditor/Thalamus may catch most issues post-generation
- Adding another LLM call per turn increases latency and cost
- Better data from real usage will inform a better design

**RECOMMENDATION:** Wait 1-2 weeks of real usage. Collect Thalamus
rejection patterns. If >20% of rejections are premise failures
(vs answer quality failures), build Phase 8. The lessons system
(Phase 5) will naturally surface whether this is needed.

*These notes saved as design spec. Revisit after testing window.*
