# ZBOT Sprint — Deep Research Architecture
## Objective: ZBOT Investigates Until It's Confident — Not Just Until It's Done

You are working on ZBOT, a personal AI agent for Jeremy Lerwick (CEO, Ziloss Technologies).
The codebase is at: ~/Desktop/zbot
GitHub: https://github.com/jeremylerwick-max/zbot (private)
GCP Project: ziloss (project number: 203743871797)

This sprint upgrades ZBOT from a "task executor" into a true **deep research system**.
The core principle: **deep research is created by iteration, not context window size.**

Triggered by: research published on multi-model deep research pipeline architecture (OpenAI/Anthropic/Gemini).

---

## Product Requirements (from Jeremy — non-negotiable)

1. **Background execution** — Research runs in the background. Jeremy can close the panel, go do other work, and receive a Slack notification when research is complete. Exactly like Gemini Deep Research.

2. **Real model names everywhere** — No "Planner" / "Executor" abstractions in the UI. Show **GPT-4o**, **Claude Sonnet 4.6**, **Llama 3.3 405B**, **Mistral Large** etc. Be transparent about which companies are actually doing the work. This is a feature, not a liability.

3. **Full source citations** — Final report must include numbered citations [1], [2], [3] with real URLs. Every non-trivial claim links to a source. Render as footnotes at bottom of report.

4. **Cost management — $200/month hard cap** — Daily budget = $200 / 30 = **$6.67/day**. Track token spend per research session. If daily budget is exceeded, queue the request for next day. Show cost breakdown per session in UI.

5. **OpenRouter for cost-effective models** — Integrate OpenRouter API (`https://openrouter.ai/api/v1`) to access cheap high-quality models. OpenRouter uses the same OpenAI-compatible API format. No Chinese models under any circumstances.

6. **Models communicate visibly** — The ResearchPanel should show a "conversation" view where each model speaks with its real name and company, like a team of specialists collaborating. Not a black box.

---

## Approved Model Roster

### BLOCKED — Never Use (Chinese origin — hard-coded exclusion list)
```go
var blockedModels = []string{
    "deepseek", "qwen", "yi-", "ernie", "baidu", "minimax",
    "moonshot", "zhipu", "intern", "01-ai",
}
```

### Premium — Direct API (use for tasks requiring best-in-class)
| Model | Display Name | API | Use In Pipeline | ~Cost/1M tokens |
|-------|-------------|-----|-----------------|-----------------|
| gpt-4o | GPT-4o · OpenAI | OpenAI | Critic (must be diff provider) | $5 |
| claude-sonnet-4-6 | Claude Sonnet 4.6 · Anthropic | Anthropic | Synthesizer (best prose) | $3 |

### Cost-Effective — via OpenRouter (preferred for volume)
| Model | Display Name | OpenRouter ID | Use In Pipeline | ~Cost/1M tokens |
|-------|-------------|--------------|-----------------|-----------------|
| Mistral Large 2 | Mistral Large 2 · Mistral AI | `mistralai/mistral-large` | Planner | $2 |
| Llama 4 Scout | Llama 4 Scout · Meta | `meta-llama/llama-4-scout` | Searcher | $0.11 |
| Llama 3.1 405B | Llama 3.1 405B · Meta | `meta-llama/llama-3.1-405b-instruct` | Extractor | $1.79 |
| Llama 4 Maverick | Llama 4 Maverick · Meta | `meta-llama/llama-4-maverick` | Vision/Extraction | $0.40 |
| Mixtral 8x22B | Mixtral 8x22B · Mistral AI | `mistralai/mixtral-8x22b-instruct` | Bulk extraction | $0.65 |
| Mistral Small 3.1 | Mistral Small 3.1 · Mistral AI | `mistralai/mistral-small-3.1` | Fast tasks | $0.10 |
| Llama 3.3 70B | Llama 3.3 70B · Meta | `meta-llama/llama-3.3-70b-instruct` | Fast classification | $0.07 |

### Default Research Pipeline Config (cost-optimized default)
```
Planner:     Mistral Large 2 · Mistral AI     (OpenRouter ~$2/1M)
Searcher:    Llama 4 Scout · Meta             (OpenRouter ~$0.11/1M)  
Extractor:   Llama 3.1 405B · Meta            (OpenRouter ~$1.79/1M)
Critic:      GPT-4o · OpenAI                  (Direct ~$5/1M — must differ from extractor)
Synthesizer: Claude Sonnet 4.6 · Anthropic    (Direct ~$3/1M — best prose)
```
**Estimated cost per deep research session: ~$0.05–$0.25**
vs. all-premium: ~$1.50–$3.00 per session

---

## Current State

- Dual Brain: GPT-4o planner + Claude executor + GPT-4o critic ✅
- Cross-session memory via pgvector ✅
- Skills: search, scrape, memory, GHL, GitHub, Sheets, Email ✅
- Single-pass execution: plan → execute → done ❌
- Claude both searches AND extracts (should be split) ❌
- Critic is Claude (same provider as executor — conflict of interest) ❌
- No schema-validated intermediate artifacts ❌
- No loop controller (research stops after 1 pass, not when "sufficient") ❌

---

## Target Architecture

```
User Query
    ↓
[PLANNER] GPT-4o
    → Outputs: ResearchPlan JSON
    ↓
[SEARCHER] Claude Haiku (fast, cheap)
    → Outputs: SourcesBlock JSON (ID-tagged URLs + snippets)
    ↓
[EXTRACTOR] Claude Sonnet (long context)
    → Outputs: ClaimSet JSON (atomic claims + evidence IDs)
    ↓
[CRITIC] GPT-4o (different provider than extractor — by design)
    → Outputs: CritiqueReport JSON (flagged claims, gaps, contradictions)
    ↓
[LOOP CONTROLLER] — orchestrator decides:
    → If critique passes threshold → proceed to synthesizer
    → If gaps remain → loop back to planner with new sub-questions
    ↓
[SYNTHESIZER] Claude Sonnet
    → Outputs: FinalReport (markdown + citations)
    ↓
[MEMORY] Auto-saves key facts + report to pgvector
    ↓
User sees final report in UI
```

Key rule from research: **A model never researches and verifies itself.**
- Planner = GPT-4o
- Extractor = Claude  
- Critic = GPT-4o (different provider validates Claude's extraction)
- Synthesizer = Claude (writes final prose)

---

## What You're Building

### Phase 1 — JSON Schemas for All Intermediate Artifacts

Create `internal/research/schemas.go` with Go structs for:

```go
// ResearchPlan — output of Planner
type ResearchPlan struct {
    Goal          string          `json:"goal"`
    SubQuestions  []string        `json:"sub_questions"`
    SearchTerms   []string        `json:"search_terms"`
    Depth         string          `json:"depth"` // "shallow" | "deep" | "exhaustive"
    AcceptanceCriteria string     `json:"acceptance_criteria"`
}

// Source — one retrieved source
type Source struct {
    ID      string `json:"id"`   // "SRC_001", "SRC_002" etc
    URL     string `json:"url"`
    Title   string `json:"title"`
    Snippet string `json:"snippet"`
}

// SourcesBlock — output of Searcher
type SourcesBlock struct {
    Query   string   `json:"query"`
    Sources []Source `json:"sources"`
}

// Claim — one atomic fact
type Claim struct {
    ID          string   `json:"id"`         // "CLM_001" etc
    Statement   string   `json:"statement"`
    EvidenceIDs []string `json:"evidence_ids"` // source IDs that support this
    Confidence  float64  `json:"confidence"`   // 0.0 - 1.0
}

// ClaimSet — output of Extractor
type ClaimSet struct {
    Claims    []Claim  `json:"claims"`
    Gaps      []string `json:"gaps"`      // things not found in sources
    SourceIDs []string `json:"source_ids"` // which sources were used
}

// CritiqueReport — output of Critic
type CritiqueReport struct {
    Passed           bool     `json:"passed"`
    UnsupportedClaims []string `json:"unsupported_claims"` // claim IDs with no evidence
    Contradictions   []string `json:"contradictions"`
    Gaps             []string `json:"gaps"`              // topics still not covered
    NewSubQuestions  []string `json:"new_sub_questions"` // follow-up research needed
    ConfidenceScore  float64  `json:"confidence_score"`  // 0.0 - 1.0
}

// ResearchState — full state tracked across loop iterations
type ResearchState struct {
    WorkflowID   string       `json:"workflow_id"`
    Goal         string       `json:"goal"`
    Iteration    int          `json:"iteration"`
    MaxIter      int          `json:"max_iterations"`
    Plan         ResearchPlan `json:"plan"`
    Sources      []Source     `json:"sources"`
    Claims       []Claim      `json:"claims"`
    Critique     CritiqueReport `json:"critique"`
    FinalReport  string       `json:"final_report"` // markdown
    Complete     bool         `json:"complete"`
}
```

### Phase 2 — Agent Prompts (4 new prompts in internal/prompts/)

Create `internal/prompts/research_prompts.go` with 4 agent system prompts:

**ResearchPlannerSystem** (GPT-4o)
```
You are the Planner in a deep research pipeline.
Your ONLY job: decompose the user's research goal into a structured plan.

Rules:
- Output ONLY valid JSON matching ResearchPlan schema. No prose. No markdown.
- sub_questions: 3-7 specific questions that fully cover the goal
- search_terms: 5-10 varied search queries to find diverse sources
- depth: "shallow" for quick facts, "deep" for analysis, "exhaustive" for comprehensive
- acceptance_criteria: one sentence describing what "done" looks like

Never attempt to answer the research question. Only plan.
```

**ResearchSearcherSystem** (Claude Haiku — fast/cheap)
```
You are the Searcher in a deep research pipeline.
Your ONLY job: retrieve sources for the given search terms using web_search and scrape_page tools.

Rules:
- Call web_search for each search term provided
- For each result, scrape the page if it looks relevant
- Assign each source a unique ID: SRC_001, SRC_002, etc.
- Output ONLY valid JSON matching SourcesBlock schema. No prose.
- Do NOT analyze or interpret sources. Only retrieve and ID them.
- Target: 8-15 high-quality sources minimum
```

**ResearchExtractorSystem** (Claude Sonnet)
```
<role>
You are the Extractor in a deep research pipeline.
Your ONLY job: extract atomic, verifiable claims from the provided sources.
</role>

<rules>
- Extract ONLY claims that are directly supported by the sources provided
- Assign each claim a unique ID: CLM_001, CLM_002, etc.
- Every claim MUST have at least one evidence_id linking it to a source
- confidence: 1.0 = explicitly stated in source, 0.5 = implied, 0.3 = inferred
- gaps: list topics from the research plan NOT covered by these sources
- Do NOT invent claims. Do NOT use prior knowledge. Sources only.
- Output ONLY valid JSON matching ClaimSet schema.
</rules>

<thought>
```

**ResearchCriticSystem** (GPT-4o — intentionally different provider)
```
You are the Critic in a deep research pipeline.
Your job: challenge the extracted claims. Be adversarial. Be thorough.

Rules:
- unsupported_claims: claim IDs where the evidence_ids don't actually support the statement
- contradictions: pairs of claims that conflict with each other
- gaps: important aspects of the research goal not covered by any claim
- new_sub_questions: follow-up questions needed to fill gaps
- confidence_score: 0.0-1.0 overall confidence in the claim set
  - 0.9+ = pass (proceed to synthesis)
  - 0.7-0.9 = weak pass (synthesize but note limitations)
  - below 0.7 = fail (loop back, research more)
- passed: true if confidence_score >= 0.7
- Output ONLY valid JSON matching CritiqueReport schema.

Be skeptical. Your job is to find holes, not validate.
```

**ResearchSynthesizerSystem** (Claude Sonnet)
```
<role>
You are the Synthesizer — the final writer in a deep research pipeline.
You write clear, authoritative research reports from verified facts only.
</role>

<rules>
- Use ONLY the claims provided. Never add facts from your own knowledge.
- Every paragraph must reference at least one claim ID
- Format: markdown with headers, bullet points where appropriate
- Include a "Sources" section at the end listing all source IDs used
- Include a "Confidence" section noting any gaps or limitations flagged by the Critic
- Tone: analytical, direct, professional
</rules>

<thought>
```

### Phase 3 — Research Orchestrator (new file: internal/research/orchestrator.go)

This is the loop controller. It manages the full research lifecycle:

```go
package research

import (
    // standard imports
)

const MaxIterations = 4
const PassThreshold = 0.7

type ResearchOrchestrator struct {
    planner    LLMClient  // GPT-4o
    searcher   LLMClient  // Claude Haiku  
    extractor  LLMClient  // Claude Sonnet
    critic     LLMClient  // GPT-4o
    synthesizer LLMClient // Claude Sonnet
    memory     MemoryStore
    webSearch  SearchTool
    scraper    ScrapeTool
    events     chan ResearchEvent // streams progress to UI
}

// RunDeepResearch is the main loop
func (ro *ResearchOrchestrator) RunDeepResearch(ctx context.Context, goal string, workflowID string) (*ResearchState, error) {
    state := &ResearchState{
        WorkflowID: workflowID,
        Goal:       goal,
        MaxIter:    MaxIterations,
        Iteration:  0,
    }

    for state.Iteration < MaxIterations {
        state.Iteration++
        ro.emit(ResearchEvent{Stage: "planning", Iteration: state.Iteration})

        // Step 1: Plan (or re-plan with gaps from previous critique)
        plan, err := ro.runPlanner(ctx, goal, state.Critique.NewSubQuestions)
        if err != nil { return nil, err }
        state.Plan = plan

        // Step 2: Search
        ro.emit(ResearchEvent{Stage: "searching", Iteration: state.Iteration})
        sources, err := ro.runSearcher(ctx, plan.SearchTerms)
        if err != nil { return nil, err }
        // Merge with existing sources (dedup by URL)
        state.Sources = mergeSources(state.Sources, sources)

        // Step 3: Extract
        ro.emit(ResearchEvent{Stage: "extracting", Iteration: state.Iteration})
        claimSet, err := ro.runExtractor(ctx, state.Sources, goal)
        if err != nil { return nil, err }
        // Merge with existing claims (dedup by statement similarity)
        state.Claims = mergeClaims(state.Claims, claimSet.Claims)

        // Step 4: Critique
        ro.emit(ResearchEvent{Stage: "critiquing", Iteration: state.Iteration})
        critique, err := ro.runCritic(ctx, state.Claims, state.Sources, goal)
        if err != nil { return nil, err }
        state.Critique = critique

        ro.emit(ResearchEvent{
            Stage: "evaluated",
            Iteration: state.Iteration,
            Confidence: critique.ConfidenceScore,
            Passed: critique.Passed,
        })

        // Loop controller decision
        if critique.ConfidenceScore >= PassThreshold || len(critique.NewSubQuestions) == 0 {
            break // Sufficient — proceed to synthesis
        }
        // else: loop with new sub-questions
    }

    // Step 5: Synthesize
    ro.emit(ResearchEvent{Stage: "synthesizing"})
    report, err := ro.runSynthesizer(ctx, state.Claims, state.Sources, state.Critique, goal)
    if err != nil { return nil, err }
    state.FinalReport = report
    state.Complete = true

    // Step 6: Auto-save to memory
    ro.saveToMemory(ctx, goal, state)

    ro.emit(ResearchEvent{Stage: "complete", Report: report})
    return state, nil
}
```

Key helper methods to implement:
- `runPlanner(ctx, goal, followUpQuestions []string) (ResearchPlan, error)` — calls GPT-4o, parses JSON
- `runSearcher(ctx, searchTerms []string) ([]Source, error)` — calls Haiku with web_search tool
- `runExtractor(ctx, sources []Source, goal string) (ClaimSet, error)` — calls Claude with sources in context
- `runCritic(ctx, claims []Claim, sources []Source, goal string) (CritiqueReport, error)` — calls GPT-4o
- `runSynthesizer(ctx, claims []Claim, sources []Source, critique CritiqueReport, goal string) (string, error)` — calls Claude
- `mergeSources(existing, new []Source) []Source` — dedup by URL
- `mergeClaims(existing, new []Claim) []Claim` — dedup by statement (string contains check is fine)
- `saveToMemory(ctx, goal string, state *ResearchState)` — saves report summary + key claims

### Phase 4 — Wire Deep Research into the Gateway

In `internal/gateway/` (or wherever the chat handler lives), detect the trigger prefix `research:` and route to the deep research orchestrator instead of the normal dual-brain workflow.

Example:
- `plan: find me a good CRM` → dual-brain workflow (existing)
- `research: what are the best CRM alternatives to GoHighLevel in 2025` → deep research loop (new)

Add to the command bar placeholder in App.tsx:
```
plan: your goal... | research: deep dive topic... | or just type to chat
```

### Phase 5 — Research Progress UI

Add `ResearchPanel.tsx` — a dedicated view for watching deep research unfold in real time.

The panel streams `ResearchEvent` updates via SSE or websocket showing:

```
🔬 Deep Research: "what are the best CRM alternatives to GoHighLevel in 2025"
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Iteration 1/4
  ✅ Planning       — 6 sub-questions, 10 search terms
  🔍 Searching...   — 0/10 sources
  ✅ Searching      — 12 sources retrieved
  ✅ Extracting     — 34 claims extracted
  ✅ Critiquing     — Confidence: 0.61 ❌ (needs more research)
  → 3 gaps found, looping...

Iteration 2/4
  ✅ Planning       — 4 follow-up questions
  ✅ Searching      — 8 new sources (20 total)
  ✅ Extracting     — 19 new claims (53 total)
  ✅ Critiquing     — Confidence: 0.84 ✅ PASSED

Synthesizing final report...
✅ Complete — 2,400 word report ready
```

Component structure:
- Stage indicators with status icons (spinner → ✅ or ❌)
- Iteration accordion (expand to see details per iteration)
- Confidence meter bar (red → yellow → green)
- Claims counter and sources counter updating live
- Final report rendered in markdown when complete
- Download button for the report (saves to workspace)

### Phase 6 — Database Migration

Create `migrations/007_sprint_deepresearch.sql`:

```sql
-- Research sessions
CREATE TABLE IF NOT EXISTS zbot_research_sessions (
    id TEXT PRIMARY KEY,
    workflow_id TEXT REFERENCES zbot_workflows(id),
    goal TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'running', -- running | complete | failed
    iterations INT DEFAULT 0,
    confidence_score FLOAT DEFAULT 0,
    final_report TEXT,
    state_json JSONB,  -- full ResearchState for inspection
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_research_sessions_workflow ON zbot_research_sessions(workflow_id);
CREATE INDEX idx_research_sessions_status ON zbot_research_sessions(status);
```

---

## File Map — Everything You Need to Create or Modify

### New Files
```
internal/research/
  schemas.go         — Go structs: ResearchPlan, Source, Claim, CritiqueReport, ResearchState
  orchestrator.go    — Main loop controller (plan→search→extract→critique→loop→synthesize)
  planner.go         — runPlanner() — calls GPT-4o, parses ResearchPlan JSON
  searcher.go        — runSearcher() — calls Haiku with web_search tool, returns []Source
  extractor.go       — runExtractor() — calls Claude Sonnet, returns ClaimSet
  critic.go          — runCritic() — calls GPT-4o, returns CritiqueReport
  synthesizer.go     — runSynthesizer() — calls Claude Sonnet, returns markdown report
  events.go          — ResearchEvent struct, emit helpers for streaming progress
  merge.go           — mergeSources(), mergeClaims() dedup helpers

internal/prompts/research_prompts.go  — 5 agent system prompts (Planner/Searcher/Extractor/Critic/Synthesizer)

internal/webui/frontend/src/components/ResearchPanel.tsx  — Live research progress UI

migrations/007_sprint_deepresearch.sql  — zbot_research_sessions table
```

### Modified Files
```
internal/gateway/           — Add "research:" prefix routing → ResearchOrchestrator
internal/webui/api_handlers.go  — POST /api/research, GET /api/research/:id (SSE stream)
internal/webui/server.go        — Register new research routes
internal/webui/frontend/src/App.tsx  — Add ResearchPanel, update command bar placeholder
```

---

## Phase 7 — Background Execution + Slack Notification

Research MUST run in the background. Jeremy should be able to fire off a research request and immediately go back to work.

### How it works:
1. User types `research: topic` → POST /api/research → returns `{session_id}` immediately (202 Accepted)
2. Goroutine starts in background running the full research loop
3. ResearchPanel shows initial "Research queued — you'll be notified when done" message
4. Jeremy can close the panel, navigate to other tabs, do other work
5. When complete → Slack message sent via existing Slack skill:

```
🔬 Research Complete: "what are the best CRM alternatives to GoHighLevel in 2025"

✅ 3 iterations • 47 claims • 18 sources • Confidence: 0.87
💰 Cost: $0.12 | ⏱ Time: 4m 32s

📄 View report: http://localhost:18790/research/{session_id}
```

### Background goroutine pattern:
```go
func (h *Handler) handleResearch(w http.ResponseWriter, r *http.Request) {
    // Parse request
    sessionID := uuid.New().String()
    
    // Store session as "queued" immediately
    h.researchStore.CreateSession(sessionID, goal)
    
    // Return immediately
    w.WriteHeader(http.StatusAccepted)
    json.NewEncoder(w).Encode(map[string]string{"session_id": sessionID})
    
    // Run in background
    go func() {
        state, err := h.researchOrchestrator.RunDeepResearch(context.Background(), goal, sessionID)
        if err != nil {
            h.researchStore.SetFailed(sessionID, err.Error())
            h.notifySlack(fmt.Sprintf("❌ Research failed: %s\nError: %s", goal, err))
            return
        }
        h.notifySlack(formatResearchComplete(state))
    }()
}
```

---

## Phase 8 — OpenRouter Client

Create `internal/llm/openrouter.go` — OpenRouter uses the OpenAI-compatible API format.

```go
package llm

// OpenRouterClient wraps the OpenAI SDK pointed at OpenRouter
// OpenRouter base URL: https://openrouter.ai/api/v1
// Auth: Bearer {OPENROUTER_API_KEY} (stored in Secret Manager as "openrouter-api-key")
// Extra headers required:
//   HTTP-Referer: https://ziloss.com
//   X-Title: ZBOT Deep Research

type OpenRouterClient struct {
    client *openai.Client  // reuse openai SDK, just change base URL
    model  string
    displayName string  // e.g. "Mistral Large 2 · Mistral AI"
}

// ModelDisplayNames maps OpenRouter model IDs to human-readable names for UI
var ModelDisplayNames = map[string]string{
    "mistralai/mistral-large":                   "Mistral Large 2 · Mistral AI",
    "meta-llama/llama-4-scout":                  "Llama 4 Scout · Meta",
    "meta-llama/llama-3.1-405b-instruct":        "Llama 3.1 405B · Meta",
    "meta-llama/llama-4-maverick":               "Llama 4 Maverick · Meta",
    "meta-llama/llama-3.3-70b-instruct":         "Llama 3.3 70B · Meta",
    "mistralai/mixtral-8x22b-instruct":          "Mixtral 8x22B · Mistral AI",
    "mistralai/mistral-small-3.1":               "Mistral Small 3.1 · Mistral AI",
    "gpt-4o":                                    "GPT-4o · OpenAI",
    "claude-sonnet-4-6":                         "Claude Sonnet 4.6 · Anthropic",
}

// BlockedModels — never allow these regardless of user config
var BlockedModelPrefixes = []string{
    "deepseek", "qwen", "yi-", "ernie", "baidu",
    "minimax", "moonshot", "zhipu", "internlm", "01-ai",
}

func IsModelBlocked(modelID string) bool {
    lower := strings.ToLower(modelID)
    for _, prefix := range BlockedModelPrefixes {
        if strings.Contains(lower, prefix) {
            return true
        }
    }
    return false
}
```

Store `OPENROUTER_API_KEY` in Google Cloud Secret Manager as `openrouter-api-key`.

---

## Phase 9 — Budget Tracker

Create `internal/research/budget.go`:

```go
const DailyBudgetUSD = 6.67  // $200/month / 30 days

type BudgetTracker struct {
    store BudgetStore  // reads/writes to DB
}

// CheckAndReserve returns error if daily budget exceeded
func (bt *BudgetTracker) CheckAndReserve(estimatedCost float64) error {
    spent := bt.store.GetTodaySpend()
    if spent + estimatedCost > DailyBudgetUSD {
        return fmt.Errorf("daily budget exceeded: spent $%.2f of $%.2f today", spent, DailyBudgetUSD)
    }
    return nil
}

// RecordSpend adds actual cost after session completes
func (bt *BudgetTracker) RecordSpend(sessionID string, cost float64, modelID string) {
    bt.store.AddSpend(sessionID, cost, modelID, time.Now())
}
```

Add to migration `007`:
```sql
CREATE TABLE IF NOT EXISTS zbot_model_spend (
    id SERIAL PRIMARY KEY,
    session_id TEXT,
    model_id TEXT,
    prompt_tokens INT,
    completion_tokens INT,
    cost_usd FLOAT,
    recorded_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_spend_date ON zbot_model_spend(recorded_at);
```

Show in UI: today's spend vs. daily budget as a progress bar in the ResearchPanel header.

---

## Phase 10 — Citation Rendering

Every `Source` in the SourcesBlock gets a citation number [1], [2], etc.
Every `Claim` references source IDs like `SRC_001`.

The **Synthesizer prompt** must map these to numbered footnotes:

```
In your final report:
- Reference claims inline as [1], [2], [3] matching source numbers
- End the report with a ## Sources section:

## Sources
[1] Title of Article — https://example.com
[2] Another Source — https://another.com
```

In `ResearchPanel.tsx`, render the final report markdown with:
- Inline citation numbers as superscript links
- Sources section at bottom as a clean numbered list with clickable URLs
- Each URL opens in a new tab

---

## Phase 11 — "The Team" Conversation UI

This is the key UX differentiator. Instead of a generic progress bar, show the models as a team having a visible conversation.

### ResearchPanel conversation view:

```
🔬 Deep Research: "GoHighLevel CRM alternatives 2025"
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

💰 Budget: $0.08 / $6.67 today  ████░░░░░░░░░░░░  1.2%

┌─────────────────────────────────────────────────────┐
│ 🟣 Mistral Large 2 · Mistral AI           [Planner] │
│                                                      │
│ "I've identified 6 research questions and 10         │
│  search terms. Depth: deep. I'll focus on pricing,   │
│  feature comparison, and market positioning."        │
│                                    Iteration 1 ✅    │
└─────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────┐
│ 🦙 Llama 4 Scout · Meta                [Searcher]   │
│                                                      │
│ "Searching 10 queries... Found 14 sources.           │
│  Top results: HubSpot, Salesforce, Pipedrive,        │
│  Keap, ActiveCampaign, Monday CRM..."                │
│                                    14 sources ✅     │
└─────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────┐
│ 🦙 Llama 3.1 405B · Meta              [Extractor]   │
│                                                      │
│ "Extracted 38 claims from 14 sources.                │
│  Key gaps: SMS automation pricing not found          │
│  for 3 platforms."                                   │
│                                    38 claims ✅      │
└─────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────┐
│ 🤖 GPT-4o · OpenAI                       [Critic]   │
│                                                      │
│ "Confidence: 0.61. Issues found:                     │
│  - 4 claims lack sufficient evidence                 │
│  - SMS pricing gap for Keap and Monday               │
│  → Need 3 more searches. Looping..."                 │
│                                   0.61 ❌ looping    │
└─────────────────────────────────────────────────────┘

  ↻ Iteration 2 starting...

┌─────────────────────────────────────────────────────┐
│ 🤖 GPT-4o · OpenAI                       [Critic]   │
│                                                      │
│ "Confidence: 0.86. Evidence is solid.                │
│  Minor gap: enterprise pricing not public            │
│  for HubSpot Enterprise. Proceeding."                │
│                                    0.86 ✅ passed    │
└─────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────┐
│ 🔷 Claude Sonnet 4.6 · Anthropic      [Synthesizer] │
│                                                      │
│ "Writing final report from 53 verified claims..."   │
│                                       Synthesizing   │
└─────────────────────────────────────────────────────┘
```

Each message card has:
- Model emoji + real name + company + role badge
- Short natural language summary of what it did/found
- Status badge (spinner → ✅ or ❌)
- Metrics (sources found, claims extracted, confidence score)

Model color coding:
- OpenAI (GPT-4o): green `#10a37f`
- Anthropic (Claude): orange-amber `#d97706`  
- Meta (Llama): blue `#3b82f6`
- Mistral AI: purple `#7c3aed`

---

## LLM Client Requirements

The research orchestrator needs 3 LLM client types:

1. **OpenAI direct** (`internal/llm/openai.go` — already exists) — GPT-4o for Critic
2. **Anthropic direct** (`internal/llm/anthropic.go` — already exists) — Claude Sonnet for Synthesizer
3. **OpenRouter** (`internal/llm/openrouter.go` — NEW) — All other models via OpenRouter

OpenRouter secret key: already exists in Secret Manager as `OPENROUTER_API_KEY`. Do NOT create a new one.
All secrets already exist — do not create any new ones.

---

## JSON Parsing Rules

All 5 agents MUST output valid JSON. Use this pattern for all agent calls:

```go
func parseJSON[T any](content string) (T, error) {
    // Strip markdown fences if present
    content = strings.TrimPrefix(content, "```json")
    content = strings.TrimPrefix(content, "```")
    content = strings.TrimSuffix(content, "```")
    content = strings.TrimSpace(content)
    
    var result T
    if err := json.Unmarshal([]byte(content), &result); err != nil {
        return result, fmt.Errorf("JSON parse failed: %w\ncontent: %s", err, content[:min(200, len(content))])
    }
    return result, nil
}
```

If JSON parse fails on first attempt, retry once with the same prompt + error message appended: `"Your previous output failed JSON validation: {err}. Output ONLY valid JSON."`

---

## Build & Deploy Steps

After all phases are complete:

```bash
# Run migration
PGPASSWORD="ZilossMemory2024!" psql \
  -h 34.28.163.109 \
  -U postgres \
  -d ziloss_memory \
  -f ~/Desktop/zbot/migrations/007_sprint_deepresearch.sql

# Build frontend
cd ~/Desktop/zbot/internal/webui/frontend
npm run build

# Build binary
cd ~/Desktop/zbot
go build -o ~/bin/zbot-bin ./cmd/zbot

# Restart service
launchctl stop com.ziloss.zbot
sleep 2
launchctl start com.ziloss.zbot

# Verify logs
tail -f ~/Library/Logs/zbot/zbot.log | grep -E "research|plan|extract|critic|synth"
```

---

## Acceptance Criteria

After this sprint, Jeremy should be able to:

1. Type `research: what are the best sales automation tools in 2025` in the command bar
2. See the ResearchPanel open automatically showing live iteration progress
3. Watch it loop 1-4 times until confidence ≥ 0.7
4. Receive a final markdown report with citations to actual sources
5. See the report saved to ~/zbot-workspace as a .md file
6. See key facts auto-saved to memory (searchable via 🧠 panel)
7. See the research session in the /workflows history page

---

## Git Commit Target

```
Sprint Deep Research: multi-agent iterative research loop

- internal/research/: 8 new files — orchestrator, 5 agents, schemas, merge
- internal/prompts/research_prompts.go: Planner/Searcher/Extractor/Critic/Synthesizer prompts
- gateway: "research:" prefix routes to ResearchOrchestrator
- webui: POST /api/research, GET /api/research/:id SSE stream
- ResearchPanel.tsx: live iteration progress with confidence meter
- migrations/007: zbot_research_sessions table
- Key principle: a model never researches and verifies itself

Tag: v1.14.0 (deep-research)
```

---

## Architecture Principles (from research — keep these in mind)

1. **A model never researches AND verifies itself** — Extractor=Claude, Critic=GPT-4o by design
2. **The database is the real conversation** — models don't talk to each other, they write to shared state
3. **Deep research = iteration, not context window size** — loop until confident, not until token limit
4. **Schema-first outputs** — every agent output is validated JSON, never free-form prose between agents
5. **The orchestrator owns control flow** — models only respond, never initiate
6. **Synthesis is the last step** — only the Synthesizer writes human-readable prose

Good luck. This is the biggest architectural upgrade in ZBOT's history.

## File Map — Everything You Need to Create or Modify

### New Files
```
internal/research/
  schemas.go         — Go structs: ResearchPlan, Source, Claim, CritiqueReport, ResearchState
  orchestrator.go    — Main loop controller (background goroutine)
  planner.go         — runPlanner() — Mistral Large 2 via OpenRouter
  searcher.go        — runSearcher() — Llama 4 Scout via OpenRouter + web_search tool
  extractor.go       — runExtractor() — Llama 3.1 405B via OpenRouter
  critic.go          — runCritic() — GPT-4o direct (different provider by design)
  synthesizer.go     — runSynthesizer() — Claude Sonnet 4.6 direct (best prose)
  events.go          — ResearchEvent struct, SSE streaming helpers
  merge.go           — mergeSources(), mergeClaims() dedup helpers
  budget.go          — BudgetTracker: $6.67/day cap, CheckAndReserve, RecordSpend
  pgstore.go         — DB store for research sessions + model spend

internal/llm/openrouter.go              — OpenRouter client, blocked model list, display names

internal/prompts/research_prompts.go    — 5 agent system prompts

internal/webui/frontend/src/components/ResearchPanel.tsx
    — "The Team" conversation UI with real model names + company colors
    — Background-aware: shows "running in background" notification state
    — Citation rendering with numbered footnotes + clickable URLs
    — Budget progress bar ($X.XX of $6.67 today)
    — Download report button (saves to ~/zbot-workspace)

migrations/007_sprint_deepresearch.sql
    — zbot_research_sessions table
    — zbot_model_spend table (budget tracking)
```

### Modified Files
```
internal/gateway/               — Add "research:" prefix → ResearchOrchestrator (background goroutine)
internal/webui/api_handlers.go  — POST /api/research (202 immediate), GET /api/research/:id (SSE)
internal/webui/server.go        — Register research routes
internal/webui/frontend/src/App.tsx
    — Add ResearchPanel
    — Update command bar: "plan: goal | research: deep dive | or just chat"
    — Notification badge when background research completes
```

---

## Build & Deploy Steps

```bash
# NOTE: All secrets already exist in Secret Manager. Do not create any.
# OPENROUTER_API_KEY, OPENAI_API_KEY, ANTHROPIC_API_KEY are all present.

# Run migration
PGPASSWORD="ZilossMemory2024!" psql \
  -h 34.28.163.109 -U postgres -d ziloss_memory \
  -f ~/Desktop/zbot/migrations/007_sprint_deepresearch.sql

# Build
cd ~/Desktop/zbot/internal/webui/frontend && npm run build
cd ~/Desktop/zbot && go build -o ~/bin/zbot-bin ./cmd/zbot

# Restart
launchctl stop com.ziloss.zbot && sleep 2 && launchctl start com.ziloss.zbot

# Verify
tail -f ~/Library/Logs/zbot/zbot.log | grep -E "research|mistral|llama|critic|synth"
```

---

## Acceptance Criteria

After this sprint, Jeremy should be able to:

1. Type `research: GoHighLevel top competitors 2025` in the command bar
2. See ResearchPanel open with "The Team" conversation view showing real model names
3. Close the panel and go do other work — research runs in background
4. Receive a Slack notification when done: "🔬 Research Complete: ... Cost: $0.09"
5. Click the link → see the full report with numbered citations [1][2][3] and source URLs
6. See today's spend vs $6.67 daily budget in the UI
7. Report auto-saved to ~/zbot-workspace as .md file
8. Key facts auto-saved to memory (searchable via 🧠)

---

## Git Commit Target

```
Sprint Deep Research: background multi-agent research with real model names

- internal/research/: orchestrator, 5 agents, schemas, budget, merge (11 files)
- internal/llm/openrouter.go: OpenRouter client + blocked model list (no Chinese models)
- internal/prompts/research_prompts.go: 5 agent prompts (Mistral/Llama/GPT-4o/Claude)
- Background execution: 202 immediate response, Slack notification on complete
- ResearchPanel: "The Team" UI — real model names, company colors, conversation style
- Citations: [1][2][3] footnotes with real URLs in final report
- Budget: $6.67/day cap, per-model spend tracking, progress bar in UI
- migrations/007: research_sessions + model_spend tables
- Blocked: deepseek, qwen, yi, ernie, baidu, minimax, moonshot, zhipu (hard-coded)
- Default pipeline cost: ~$0.05-$0.25/session (vs $1.50-$3.00 all-premium)

Tag: v1.14.0 (deep-research)
```

---

## Architecture Principles

1. **A model never researches AND verifies itself** — Llama 405B extracts, GPT-4o critiques
2. **Show real model names** — transparency is a feature, not a liability
3. **Background by default** — fire and forget, Slack when done (like Gemini Deep Research)
4. **The database is the real conversation** — models write to shared state, not to each other
5. **Deep research = iteration, not context window size** — loop until confident
6. **Schema-first** — every agent output is validated JSON
7. **Cost-conscious** — OpenRouter cheap models do volume work, premium models close the deal
8. **No Chinese models** — hard-coded blocked list checked before every API call

Good luck. This is the biggest architectural upgrade in ZBOT's history.
