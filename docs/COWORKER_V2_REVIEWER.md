# COWORKER PROMPT — V2 Sprint A: Single Brain + Background Reviewer

You are working on ZBOT, an AI agent platform written in Go (backend) and React/TypeScript (frontend).

## Your Mission

Two objectives in priority order:

**Objective 1: V2 Architecture Simplify** — Kill the planner/executor/critic multi-model dance. Replace with Sonnet 4.6 as the single default brain. Integrate the benchmark-driven Router so Cortex automatically picks the optimal model per task.

**Objective 2: Background Reviewer Foundation** — Build `internal/reviewer/` module that lets a second model (different provider) continuously review Cortex's work by reading ZBOT's logs and memory.

## Read These Files First

```bash
cd ~/Desktop/Projects/zbot

# Architecture context
cat docs/ARCHITECTURE.md
cat docs/ZBOT_V2_PLAN.md
cat docs/ZBOT_V2_ARCHITECTURE.md

# Current agent loop (THIS IS WHAT YOU'RE SIMPLIFYING)
cat cmd/zbot/wire.go
cat internal/agent/ports.go

# Router you're integrating (ALREADY BUILT)
cat internal/router/model_scores.go
cat internal/router/classify.go

# Event bus (reviewer will emit events here)
cat internal/agent/eventbus.go

# Current LLM adapters
cat internal/llm/anthropic.go
cat internal/llm/openaicompat.go

# Existing prompts
cat internal/prompts/
```

## Branch

```bash
git checkout public-release
git checkout -b v2/single-brain-reviewer
```

## OBJECTIVE 1: Single Brain Architecture

### What to change

The current agent in `internal/agent/` has a multi-step loop: Planner → Executor → Critic. This is obsolete — modern models can plan + execute + self-critique in a single pass.

### Phase 1: Simplify the Agent Loop

**File: `internal/agent/agent.go`**
- The core `Run()` method should be: receive message → pick model via Router → send to LLM with tools → execute tools → return response
- Remove any multi-model orchestration (no separate planner/executor/critic passes)
- Keep the tool execution loop (Cortex calls tools, gets results, continues)
- Keep the event bus emissions (every action emits events)

**File: `cmd/zbot/wire.go`**
- Replace the `_ = modelRouter` placeholder with actual integration
- The Router is already initialized with `PreferAmerican: true`
- Wire it so the agent calls `router.ClassifyTask(goal)` then `router.BestModel(task, preferQuality)` to select which model to use
- Default to Sonnet 4.6 when Router has no strong opinion
- Escalate to Opus 4.6 only for tasks where Router's quality pick is Opus

**File: `internal/agent/config.go` or equivalent**
- Add a `ModelRouter` field to the agent config
- The agent should accept the Router and use it per-request

### Phase 2: Clean Up Dead Code

- Remove or deprecate: PlannerPanel references, ExecutorPanel references, HandoffAnimation, CriticBadge
- In the frontend, the UI already uses PaneManager with Chat + Auditor — verify there are no remaining planner/executor panel references
- Clean up any "workflow" code that assumed multi-step plan → execute → critique flow
- DO NOT remove the workflow/orchestrator module entirely — it's used for multi-task workflows. Just simplify single-turn chat.

## OBJECTIVE 2: Background Reviewer Module

### Architecture

```
internal/reviewer/
├── reviewer.go      # Main ReviewEngine — background goroutine
├── checker.go       # Sends chunks to reviewer model via OpenAI-compatible API
├── chunker.go       # Splits recent actions/code into review-sized chunks
├── events.go        # ReviewEvent types for the event bus
└── config.go        # ReviewConfig: model, interval, cost limits
```

### reviewer.go — The Engine

```go
type ReviewEngine struct {
    config     ReviewConfig
    llmClient  LLMClient       // OpenAI-compatible client for reviewer model
    memStore   agent.MemoryStore // READ-ONLY access to ZBOT's memory
    eventBus   agent.EventBus   // Emit ReviewEvents
    logger     *slog.Logger
    stopCh     chan struct{}
}

// Start begins the background review loop.
// Runs on a timer (default: every 60 seconds).
// Each cycle: read recent actions from memory/logs → chunk → send to reviewer → emit events.
func (r *ReviewEngine) Start()

// Stop halts the background reviewer.
func (r *ReviewEngine) Stop()

// ReviewOnce runs a single review cycle (useful for testing).
func (r *ReviewEngine) ReviewOnce(ctx context.Context) ([]ReviewFinding, error)
```

### checker.go — Model Integration

```go
// Check sends a chunk to the reviewer model and parses findings.
// Uses OpenAI-compatible API (works with GPT-5.4 mini, Grok, any OpenAI-compat endpoint).
func (c *Checker) Check(ctx context.Context, chunk ReviewChunk) ([]ReviewFinding, error)

// The system prompt for the reviewer:
// "You are a code and action reviewer. You receive logs of actions taken by an AI agent.
//  Your job: find bugs, security issues, logic errors, missed edge cases, and better approaches.
//  For each finding, specify: severity (critical/warning/info), the specific action or code,
//  and your suggested fix. Respond in JSON."
```

### chunker.go — Intelligent Splitting

```go
// ChunkRecentActions reads the last N actions from the event bus history
// and groups them into reviewable chunks.
// Strategy: group by session, then by file/topic, max 2000 tokens per chunk.
func ChunkRecentActions(eventBus agent.EventBus, sessionID string, maxChunks int) []ReviewChunk

type ReviewChunk struct {
    ID          string
    SessionID   string
    Actions     []string  // Formatted action summaries
    CodeContent string    // If actions involved code, include the code
    Context     string    // What was the user trying to do
    TokenEstimate int
}
```

### events.go — Review Events

```go
type ReviewFinding struct {
    ID          string    `json:"id"`
    Severity    string    `json:"severity"`    // "critical", "warning", "info"
    Category    string    `json:"category"`    // "bug", "security", "performance", "style", "logic"
    Description string    `json:"description"` // What's wrong
    Location    string    `json:"location"`    // Which action/file/line
    Suggestion  string    `json:"suggestion"`  // How to fix it
    Confidence  float64   `json:"confidence"`  // 0-1 how sure the reviewer is
    Timestamp   time.Time `json:"timestamp"`
}

// Event types for the ZBOT event bus
const (
    EventReviewFinding  EventType = "review_finding"
    EventReviewCycle    EventType = "review_cycle"
    EventReviewError    EventType = "review_error"
)
```

### config.go

```go
type ReviewConfig struct {
    Enabled        bool          // Master switch
    ReviewInterval time.Duration // How often to run (default: 60s)
    ModelEndpoint  string        // OpenAI-compatible API URL
    ModelName      string        // e.g. "gpt-5.4-mini"
    APIKey         string        // From GCP Secret Manager
    MaxCostPerDay  float64       // Daily spending cap (default: $2.00)
    MaxChunksPerCycle int        // Max chunks per review (default: 10)
    MinSeverity    string        // Only emit findings at this level or above
}
```

### Wire into wire.go

After building the module:
1. Create a `ReviewConfig` from env vars / config
2. Create the `ReviewEngine` with the existing `memStore` and `eventBus`
3. Call `reviewEngine.Start()` after the agent is created
4. Add a defer `reviewEngine.Stop()` for cleanup
5. Review findings flow through the existing event bus → SSE → Auditor pane shows them

## Definition of Done

1. `go build ./...` passes
2. `cd internal/webui/frontend && npx vite build` passes
3. `go test ./...` passes (including new reviewer tests)
4. Single-turn chat uses Router to pick model (verify with log output)
5. Background reviewer runs on timer and emits ReviewFinding events
6. No remaining planner/executor/critic orchestration in single-turn chat path
7. Commit all changes to `v2/single-brain-reviewer` branch

## DO NOT

- Delete the workflow/orchestrator module (still used for multi-task workflows)
- Modify the Hawkeye crawler module (internal/crawler/) — it's done
- Change the Router module (internal/router/) — it's done, just wire it
- Break the existing event bus or SSE streaming
- Add new npm dependencies without checking bundle size
- Push directly to main

## IMPORTANT: Test After Every Major Change

After simplifying the agent loop: `go build && go test ./...`
After building reviewer module: `go build && go test ./...`
After wiring into wire.go: `go build && go test ./...`

## Repo Location
~/Desktop/Projects/zbot
