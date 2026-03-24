# COWORKER PROMPT — ZBOT Grand Sprint: V2 Architecture + Reviewer + Polish

**Date:** 2026-03-23
**Repo:** ~/Desktop/Projects/zbot
**Branch:** Create `v2/grand-sprint` from `public-release`
**Priority:** Execute in the order listed. Each phase builds on the previous.

---

## WHO YOU ARE

You are a senior Go + React/TypeScript engineer working on ZBOT, an AI agent platform. The codebase is real, tested, and deployed. You have access to the full filesystem, terminal, git, and all build tools. Read before writing. Test after every change.

---

## FIRST: READ THE CODEBASE

Before writing ANY code, read these files to understand the architecture:

```bash
cd ~/Desktop/Projects/zbot

# Core architecture
cat CLAUDE.md
cat PRD.md
cat docs/ARCHITECTURE.md
cat docs/ZBOT_V2_PLAN.md

# Agent loop (THIS IS WHAT YOU'RE CHANGING)
cat internal/agent/ports.go
cat internal/agent/agent.go
cat internal/agent/eventbus.go

# Wire file (main DI and setup)
cat cmd/zbot/wire.go

# Router (ALREADY BUILT — integrate, don't rewrite)
cat internal/router/model_scores.go
cat internal/router/classify.go

# Crawler (ALREADY BUILT — wire eventBus, don't rewrite)
cat internal/crawler/crawler.go
cat internal/crawler/session.go
cat internal/crawler/events.go

# Frontend
cat internal/webui/frontend/src/components/PaneManager.tsx
cat internal/webui/frontend/src/lib/types.ts
cat internal/webui/frontend/src/lib/crawler-types.ts
cat internal/webui/frontend/src/hooks/useEventBus.ts
cat internal/webui/frontend/src/hooks/useCrawler.ts

# LLM adapters
cat internal/llm/anthropic.go
cat internal/llm/openaicompat.go

# Web server
cat internal/webui/server.go
cat internal/webui/thalamus_handler.go
cat internal/webui/crawler_handler.go

# Prompts
ls internal/prompts/
cat internal/prompts/*.go
```

After reading, run the build to confirm your baseline:
```bash
go build ./...
cd internal/webui/frontend && npx vite build && cd ../../..
go test ./...
```
All must pass before you start any work.

---

## PHASE 1: V2 Single Brain Architecture (HIGHEST PRIORITY)

### What This Is

The current agent loop has a multi-model orchestration: Planner (GPT-4o) → Executor (Claude) → Critic (GPT-4o). This is obsolete. Modern models plan + execute + self-critique in a single pass.

Replace this with: **Sonnet 4.6 as the single default brain**, with the Router (already built in `internal/router/`) selecting the optimal model per task.

### 1A. Simplify the Agent Loop

**Read first:** `internal/agent/agent.go` — understand the current `Run()` method.

The simplified loop should be:
1. Receive user message
2. Call `router.ClassifyTask(message)` to determine task category
3. Call `router.BestModel(category, preferQuality)` to pick model
4. Send message to selected model with tools available
5. If model calls tools → execute tools → send results back → continue
6. When model returns final text → emit events → return response

**Do NOT remove:**
- Tool execution loop (Cortex calls tools, gets results, continues — this stays)
- Event bus emissions (every action must emit events)
- Memory injection (relevant memories prepended to context)
- The workflow/orchestrator module (`internal/workflow/`) — it handles multi-task workflows separately

**DO remove or bypass:**
- Separate planner pass
- Separate critic/review pass
- Multi-model handoff logic for single-turn chat
- Any code that routes to different models for plan vs execute vs critique

### 1B. Wire the Router into wire.go

**File:** `cmd/zbot/wire.go`

Currently there's a placeholder:
```go
modelRouter := router.NewRouter(router.Preferences{
    PreferAmerican: true,
})
_ = modelRouter // TODO: wire into agent model selection
```

Wire it so:
- The agent accepts the Router as a dependency
- Before each LLM call, the agent calls `router.ClassifyTask()` + `router.BestModel()`
- Add a `ModelRouter` field to agent config or the agent struct
- Default to Sonnet 4.6 when Router has no strong opinion
- Log which model was selected and why (task category, score, cost)

The Router has 12 models, 11 benchmarks, and a cost-efficiency scoring engine. It returns `Recommendation{Model, TaskScore, CostEfficiency, Reason}`. Use the `Model.ID` field to select which LLM adapter to call.

### 1C. Clean Up Dead Frontend Code

After the backend simplification:
- Search the frontend for any remaining `planner`, `executor`, `critic`, `handoff` references
- Remove or deprecate components: PlannerPanel, ExecutorPanel, HandoffAnimation, CriticBadge
- The UI already uses PaneManager with Chat + Auditor panes — those stay
- Verify the Sidebar doesn't reference old panel types
- Run `npx vite build` after cleanup to confirm

### PHASE 1 CHECKPOINT
```bash
go build ./...
go test ./...
cd internal/webui/frontend && npx vite build && cd ../../..
git add -A && git commit -m "feat: V2 single brain — Router-driven model selection, simplified agent loop"
```

---

## PHASE 2: Wire Crawler EventBus

### What This Is

The Hawkeye visual crawler (`internal/crawler/`) is built but its SessionManager was created with `nil` eventBus. Crawler events need to flow through ZBOT's event bus so the BrowserPane and CrawlLogPane in the frontend can show live updates.

### 2A. Wire EventBus into SessionManager

**File:** `internal/crawler/session.go`

The `NewSessionManager(eventBus agent.EventBus)` already accepts an eventBus parameter. In `cmd/zbot/wire.go`, the SessionManager is created before the eventBus exists:

```go
crawlerSessions := crawler.NewSessionManager(nil)
```

Fix this by either:
- Moving the SessionManager creation after the eventBus creation
- Or adding a `SetEventBus(eb agent.EventBus)` method to SessionManager and calling it after eventBus is created

Then verify: when the crawler navigates/clicks/types, events flow through the event bus → SSE → frontend.

### 2B. Verify Frontend SSE Connection

The frontend hooks (`useCrawler.ts`) connect to `/api/events/{sessionID}` for SSE. Verify that:
- Crawl events (`crawl_screenshot`, `crawl_action`, `crawl_status`) are emitted as AgentEvents
- The `crawler_handler.go` `agentEventFromCrawl()` function correctly bridges crawl events to agent events
- The SSE endpoint in `events_handler.go` streams these to the frontend

### PHASE 2 CHECKPOINT
```bash
go build ./...
go test ./...
git add -A && git commit -m "wire: crawler eventBus integration — live screenshots + action events to frontend"
```

---

## PHASE 3: Background Reviewer Module

### What This Is

A second model (different provider than Cortex) runs in the background, continuously reviewing ZBOT's work. It reads ZBOT's action logs and memory store, finds bugs/issues, and emits ReviewFinding events to the Auditor pane. This is the automated version of manually pasting code between Claude and ChatGPT.

### Key Design Principle

The reviewer does NOT maintain its own conversation. It has READ access to ZBOT's memory store and event bus history. That's how context stays synchronized — both models read the same ground truth.

### 3A. Create `internal/reviewer/` Module

```
internal/reviewer/
├── reviewer.go      # ReviewEngine — background goroutine
├── checker.go       # Sends chunks to reviewer model (OpenAI-compatible API)
├── chunker.go       # Splits recent actions/code into review-sized chunks
├── events.go        # ReviewFinding struct + event types
├── config.go        # ReviewConfig struct
```

**reviewer.go — The Engine**
```go
type ReviewEngine struct {
    config     ReviewConfig
    checker    *Checker
    eventBus   agent.EventBus
    logger     *slog.Logger
    stopCh     chan struct{}
    mu         sync.Mutex
    running    bool
    todayCost  float64
}

func NewReviewEngine(cfg ReviewConfig, eventBus agent.EventBus, logger *slog.Logger) *ReviewEngine

// Start begins the background review loop on a timer (default 60s).
func (r *ReviewEngine) Start()

// Stop halts the background reviewer.
func (r *ReviewEngine) Stop()

// ReviewOnce runs a single review cycle (for testing and on-demand).
func (r *ReviewEngine) ReviewOnce(ctx context.Context) ([]ReviewFinding, error)
```

The review loop:
1. Read last N events from the event bus history (via `MemEventBus` — it keeps a ring buffer per session)
2. Chunk them into review-sized batches (max 2000 tokens per chunk, max 10 chunks per cycle)
3. Send each chunk to the reviewer model with a system prompt
4. Parse structured JSON findings from the response
5. Emit ReviewFinding events to the event bus
6. Track daily cost, stop if `MaxCostPerDay` exceeded

**checker.go — Model Integration**
```go
type Checker struct {
    client    *http.Client
    endpoint  string // OpenAI-compatible API URL
    model     string // e.g. "gpt-5.4-mini"
    apiKey    string
}

func NewChecker(endpoint, model, apiKey string) *Checker

// Check sends a chunk to the reviewer and returns findings.
func (c *Checker) Check(ctx context.Context, chunk ReviewChunk) ([]ReviewFinding, error)
```

The reviewer system prompt:
```
You are a code and action reviewer for an AI agent called ZBOT. You receive logs of actions 
taken by the agent (tool calls, code output, navigation, etc.). Your job:
1. Find bugs, security issues, logic errors, missed edge cases, and better approaches
2. For each finding, respond with JSON: severity (critical/warning/info), category 
   (bug/security/performance/style/logic), description, location, suggestion, confidence (0-1)
3. If everything looks good, return an empty array
4. Be concise. Only flag real issues, not style preferences.

Respond ONLY with a JSON array of findings. No preamble, no markdown.
```

**chunker.go — Intelligent Splitting**
```go
type ReviewChunk struct {
    ID          string
    SessionID   string
    Summary     string   // What the user was trying to do
    Actions     []string // Formatted action summaries from event bus
    CodeContent string   // If actions involved code, include it
    TokenEst    int
}

// ChunkRecentEvents reads the last N events from the event bus
// and groups them into reviewable chunks.
func ChunkRecentEvents(eventBus agent.EventBus, sessionID string, maxChunks int) []ReviewChunk
```

Chunking strategy:
- Read the last 50 events from the active session
- Group consecutive events that relate to the same task
- For each group, create a ReviewChunk with formatted summaries
- Include any code content from tool results (file writes, code runner output)
- Estimate tokens (rough: 4 chars = 1 token)
- Cap at 2000 tokens per chunk, 10 chunks per cycle

**events.go — Review Events**
```go
type ReviewFinding struct {
    ID          string    `json:"id"`
    Severity    string    `json:"severity"`     // "critical", "warning", "info"
    Category    string    `json:"category"`     // "bug", "security", "performance", "style", "logic"
    Description string    `json:"description"`
    Location    string    `json:"location"`     // Which action/file/line
    Suggestion  string    `json:"suggestion"`
    Confidence  float64   `json:"confidence"`   // 0.0-1.0
    ReviewerModel string  `json:"reviewer_model"`
    Timestamp   time.Time `json:"timestamp"`
}

// Event type constants (add to existing event types in ports.go or here)
const (
    EventReviewFinding = "review_finding"
    EventReviewCycle   = "review_cycle"
    EventReviewError   = "review_error"
)
```

**config.go**
```go
type ReviewConfig struct {
    Enabled          bool          `yaml:"enabled"`
    ReviewInterval   time.Duration `yaml:"review_interval"`    // default: 60s
    ModelEndpoint    string        `yaml:"model_endpoint"`     // OpenAI-compatible URL
    ModelName        string        `yaml:"model_name"`         // e.g. "gpt-5.4-mini"
    APIKey           string        `yaml:"-"`                  // From env/secrets
    MaxCostPerDay    float64       `yaml:"max_cost_per_day"`   // default: $2.00
    MaxChunksPerCycle int          `yaml:"max_chunks_per_cycle"` // default: 10
    MinSeverity      string        `yaml:"min_severity"`       // default: "warning"
    InputCostPer1M   float64       `yaml:"input_cost_per_1m"`  // for cost tracking
    OutputCostPer1M  float64       `yaml:"output_cost_per_1m"` // for cost tracking
}

func DefaultReviewConfig() ReviewConfig {
    return ReviewConfig{
        Enabled:          false, // Off by default, user opts in
        ReviewInterval:   60 * time.Second,
        ModelEndpoint:    "https://api.openai.com/v1/chat/completions",
        ModelName:        "gpt-4o-mini", // Cheap, different provider = different blind spots
        MaxCostPerDay:    2.00,
        MaxChunksPerCycle: 10,
        MinSeverity:      "warning",
        InputCostPer1M:   0.15,
        OutputCostPer1M:  0.60,
    }
}
```

### 3B. Wire Reviewer into wire.go

After building the module:
```go
// In wire.go, after eventBus is created:
reviewCfg := reviewer.DefaultReviewConfig()
if openaiKey := os.Getenv("OPENAI_API_KEY"); openaiKey != "" {
    reviewCfg.Enabled = true
    reviewCfg.APIKey = openaiKey
}
reviewEngine := reviewer.NewReviewEngine(reviewCfg, eventBus, logger)
if reviewCfg.Enabled {
    reviewEngine.Start()
    defer reviewEngine.Stop()
    logger.Info("background reviewer started", "model", reviewCfg.ModelName, "interval", reviewCfg.ReviewInterval)
}
```

### 3C. Tests

Write tests in `internal/reviewer/reviewer_test.go`:
- `TestChunkRecentEvents` — verify chunking logic
- `TestReviewOnce` — mock the checker, verify findings are emitted
- `TestCostTracking` — verify daily cost cap works
- `TestDefaultConfig` — verify defaults are sane

### PHASE 3 CHECKPOINT
```bash
go build ./...
go test ./...
git add -A && git commit -m "feat: background reviewer — multi-model code checking with cost tracking"
```

---

## PHASE 4: Code Mode UI

### What This Is

When Cortex reads or writes files, the UI should auto-split to show a file tree and code preview alongside chat. This was specified in `docs/SPRINT_NEXT.md`.

### 4A. Emit file_read/file_write Events from Tools

**Files:** `internal/tools/filesystem.go` or wherever read_file/write_file tools are implemented.

After each file tool execution, emit an event:
```go
eventBus.Emit(ctx, agent.AgentEvent{
    SessionID: sessionID,
    Type:      "file_read", // or "file_write"
    Summary:   "Read " + filePath,
    Detail:    map[string]any{"path": filePath, "size": len(content)},
    Timestamp: time.Now(),
})
```

The tools don't currently have event bus access. Options:
- Pass the event bus to tools at construction time
- Have the agent emit the events in its tool execution loop after each result (RECOMMENDED — least invasive, check tool name and emit accordingly)

### 4B. FileTreePane Already Exists

Check if `internal/webui/frontend/src/components/FileTreePane.tsx` is complete. If it needs work:
- Show the workspace file structure (from `/api/workspace` endpoint)
- Highlight files being read (cyan) or written (amber) using file_read/file_write events
- Click a file to preview it

### 4C. CodePreviewPane Already Exists

Check `CodePreviewPane.tsx`. If it needs work:
- Show file content with line numbers, monospace, dark theme
- Auto-update when Cortex writes the file

### 4D. Auto-Split for Code Tasks

In `PaneManager.tsx`, when file_read or file_write events are detected:
- If FileTreePane not open → auto-split: Chat (42%) + FileTree (18%) + CodePreview (40%)
- Similar to how the Auditor auto-opens when Cortex starts working

### 4E. Fix Auditor to Use Real Event Bus

**File:** `internal/webui/frontend/src/components/ThalamusPane.tsx`

Currently the Auditor (formerly Observer/Thalamus) fakes events by watching `workflowState.toolCalls`. Update it to read from the real event bus via the `useEventBus` hook. Show actual events: tool calls, file operations, crawl actions, review findings.

### PHASE 4 CHECKPOINT
```bash
go build ./...
cd internal/webui/frontend && npx vite build && cd ../../..
go test ./...
git add -A && git commit -m "feat: code mode UI — file events, auto-split, Auditor uses real event bus"
```

---

## PHASE 5: Sprint 10 Completion (v1.0 Release Prep)

### 5A. Integration Tests

14 of 32 packages have tests. Add tests for the untested packages. Priority:
1. `internal/webui/` — test HTTP handlers (crawler, chat, events)
2. `internal/workflow/` — test orchestrator
3. `internal/scheduler/` — test cron parsing
4. `internal/skills/` — test each skill's tool registration
5. `internal/scraper/` — test rate limiter, cache

Each test file should have at least 2-3 test functions covering happy path and error cases.

### 5B. Trigger GHCR Docker Build

The GitHub Actions workflow exists at `.github/workflows/`. Trigger it by pushing to the right branch or manually via `gh workflow run`.

### 5C. Tag v1.0

After all tests pass:
```bash
git tag -a v1.0.0 -m "ZBOT v1.0.0 — single brain, visual crawler, benchmark router, background reviewer"
git push origin v1.0.0
git push public v1.0.0
```

### PHASE 5 CHECKPOINT
```bash
go test ./... # ALL packages should now have tests
git add -A && git commit -m "test: integration tests for remaining packages, v1.0 prep"
```

---

## DEFINITION OF DONE (ALL PHASES)

1. `go build ./...` passes clean
2. `cd internal/webui/frontend && npx vite build` passes clean
3. `go test ./...` — ALL packages pass, 0 failures
4. Single-turn chat uses Router to select model (verify via log output)
5. Crawler events flow through event bus to frontend (BrowserPane updates live)
6. Background reviewer runs on timer, emits ReviewFinding events
7. File events trigger code mode auto-split in UI
8. Auditor pane shows real event bus events
9. No remaining planner/executor/critic references in single-turn chat path
10. Integration tests exist for at least 20 of 32 packages
11. All changes committed to `v2/grand-sprint` branch with descriptive messages

---

## DO NOT

- Rewrite `internal/crawler/` — it's done and tested, just wire the eventBus
- Rewrite `internal/router/` — it's done and tested, just integrate it
- Delete the workflow/orchestrator module — multi-task workflows still need it
- Modify `.env` or config.yaml with real secrets
- Break existing SSE streaming or event bus functionality
- Add npm dependencies without checking bundle size
- Push directly to main or public-release

## COMMIT STRATEGY

Commit after each phase checkpoint. Use descriptive messages:
- Phase 1: `feat: V2 single brain — Router-driven model selection, simplified agent loop`
- Phase 2: `wire: crawler eventBus integration — live updates to frontend`
- Phase 3: `feat: background reviewer — multi-model checking with cost tracking`
- Phase 4: `feat: code mode UI — file events, auto-split, Auditor real events`
- Phase 5: `test: integration tests, v1.0 prep`

## EXECUTION ORDER

Phase 1 → Phase 2 → Phase 3 → Phase 4 → Phase 5. Each builds on the previous.

If a phase is blocked (e.g., missing dependency, API key needed), skip to the next phase and come back. Document what was skipped and why in a comment in the code.

## REPO LOCATION
~/Desktop/Projects/zbot
