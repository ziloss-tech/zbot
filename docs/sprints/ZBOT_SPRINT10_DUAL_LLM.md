# ZBOT Sprint 10 — Dual LLM: Planner + Executor
## Vision: Two frontier models working symbiotically

GPT-4o (or o3) as the **Planner** — big picture, task decomposition, structured reasoning
Claude Sonnet as the **Executor** — takes each task, picks tools, adapts, gets it done

Neither model knows the other exists at the API level. ZBOT is the substrate connecting them.
The user can interrupt either independently via Slack.

---

## Architecture

```
User → Slack → ZBOT
                 │
                 ├─ /plan <goal> ──→ PlannerAgent (GPT-4o)
                 │                      │
                 │                      └─ returns TaskGraph (JSON)
                 │                              │
                 │                              └─ written to workflow DB
                 │
                 └─ WorkflowOrchestrator (already built)
                         │
                         └─ picks up tasks → ExecutorAgent (Claude)
                                                │
                                                └─ runs tools, reports back
```

User sees progress in Slack. Can type to interrupt, redirect, or cancel at any point.

---

## New Files to Create

### 1. `internal/llm/openai.go`
OpenAI client — mirrors the existing `internal/llm/client.go` interface.

```go
type OpenAIClient struct {
    apiKey string
    model  string  // "gpt-4o" or "o3"
    logger *slog.Logger
}

// Chat sends messages to OpenAI and returns the response text.
// No tool calling needed for planner — just structured JSON output.
func (c *OpenAIClient) Chat(ctx context.Context, systemPrompt, userMsg string) (string, error)
```

Uses: `github.com/sashabaranov/go-openai` (add to go.mod)
Secret: `openai-api-key` in GCP Secret Manager

---

### 2. `internal/planner/planner.go`
Takes a goal string, calls GPT-4o, returns a TaskGraph.

```go
type TaskGraph struct {
    Goal        string      `json:"goal"`
    Tasks       []PlannedTask `json:"tasks"`
    TotalSteps  int         `json:"total_steps"`
}

type PlannedTask struct {
    ID           string   `json:"id"`           // "task-1", "task-2"
    Title        string   `json:"title"`        // "Research competitor pricing"
    Instruction  string   `json:"instruction"`  // Full instruction for Claude
    DependsOn    []string `json:"depends_on"`   // task IDs that must complete first
    Parallel     bool     `json:"parallel"`     // can run concurrently with siblings
    ToolHints    []string `json:"tool_hints"`   // suggested tools: ["web_search", "write_file"]
    Priority     int      `json:"priority"`     // 1=high, 2=medium, 3=low
}

func (p *Planner) Plan(ctx context.Context, goal string) (*TaskGraph, error)
```

---

### 3. Planner System Prompt (GPT-4o)

```
You are a strategic planning agent. Your job is to decompose complex goals into 
concrete, executable tasks for an AI executor.

The executor has these tools available:
- web_search: search the internet
- fetch_url: read any webpage
- read_file / write_file: read and write files in the workspace
- run_code: execute Python, Go, JavaScript, or bash
- save_memory / search_memory: long-term memory
- github: read/write GitHub repos and issues
- sheets: read/write Google Sheets
- send_email: send emails

Rules:
1. Break the goal into 3-10 concrete tasks. No more.
2. Each task must have a clear, specific instruction the executor can act on immediately.
3. Mark tasks that can run in parallel (parallel: true) vs sequential (depends_on).
4. Suggest relevant tools for each task (tool_hints).
5. Return ONLY valid JSON matching the TaskGraph schema. No explanation, no markdown.

Schema:
{
  "goal": "original goal string",
  "total_steps": N,
  "tasks": [
    {
      "id": "task-1",
      "title": "short title",
      "instruction": "detailed instruction for the executor",
      "depends_on": [],
      "parallel": true,
      "tool_hints": ["web_search"],
      "priority": 1
    }
  ]
}
```

---

### 4. `internal/planner/submit.go`
Converts a TaskGraph into workflow DB tasks using the existing WorkflowStore.

```go
// Submit writes a TaskGraph to the workflow DB and returns the workflow ID.
// The existing WorkflowOrchestrator picks up the tasks automatically.
func Submit(ctx context.Context, store workflow.Store, graph *TaskGraph, sessionID string) (string, error)
```

---

### 5. Wire changes in `cmd/zbot/wire.go`

Add to secrets section:
```go
openaiKey, _ := sm.Get(ctx, "openai-api-key")
```

Add after LLM client init:
```go
var planner *planner.Planner
if openaiKey != "" {
    openaiClient := llm.NewOpenAIClient(openaiKey, "gpt-4o", logger)
    planner = planner.New(openaiClient, logger)
    logger.Info("planner ready", "model", "gpt-4o")
} else {
    logger.Warn("OpenAI key not set — /plan command disabled")
}
```

Add to handler slash commands:
```go
// /plan <goal> — GPT-4o decomposes goal, Claude executes tasks
if strings.HasPrefix(trimmed, "/plan ") && planner != nil && orch != nil {
    goal := strings.TrimSpace(strings.TrimPrefix(trimmed, "/plan "))
    
    // Show user we're planning
    // (send interim message to Slack)
    
    graph, err := planner.Plan(ctx, goal)
    if err != nil {
        return fmt.Sprintf("❌ Planning failed: %v", err), nil
    }
    
    wfID, err := planner.Submit(ctx, orch.Store(), graph, sessionID)
    if err != nil {
        return fmt.Sprintf("❌ Failed to submit plan: %v", err), nil
    }
    
    // Format task list for user
    var sb strings.Builder
    sb.WriteString(fmt.Sprintf("🧠 *GPT-4o planned %d tasks:*\n\n", len(graph.Tasks)))
    for _, t := range graph.Tasks {
        parallel := ""
        if t.Parallel {
            parallel = " _(parallel)_"
        }
        sb.WriteString(fmt.Sprintf("• *%s*%s\n  %s\n", t.Title, parallel, truncateStr(t.Instruction, 100)))
    }
    sb.WriteString(fmt.Sprintf("\n🚀 Workflow `%s` started — Claude is executing.\nUse `/status %s` to track.", wfID, wfID))
    
    return sb.String(), nil
}
```

---

### 6. Interim Slack messaging
The handler needs to send a "thinking..." message to Slack while GPT-4o plans.
This requires adding a `SendMessage(channelID, text string)` method to SlackGateway
and passing the gateway reference into the handler closure.

Currently handler only returns a string. Need to either:
- Option A: Pass slack client into handler closure (simpler)
- Option B: Return a channel of messages (cleaner, more work)

**Recommendation: Option A for now.**

---

## Secrets Needed

| Secret Name | Value | Status |
|-------------|-------|--------|
| `openai-api-key` | Your OpenAI API key | ❌ Need to add |
| `zbot-allowed-user-id` | U09EQGVUVNE | ✅ Done |
| `zbot-db-password` | ZilossMemory2024! | ✅ Done |

---

## New Dependency

```bash
go get github.com/sashabaranov/go-openai
```

---

## Slack UX Flow

```
Jeremy: /plan research the top 5 GoHighLevel competitors, 
        summarize pricing and key features, 
        save report to workspace

ZBOT:   🧠 GPT-4o planned 5 tasks:

        • Research competitor list (parallel)
          Search for top GoHighLevel alternatives...
        • Fetch HubSpot pricing page (parallel)
          Fetch https://hubspot.com/pricing and extract...
        • Fetch ActiveCampaign pricing page (parallel)
          Fetch https://activecampaign.com/pricing...
        • Fetch Keap/Infusionsoft pricing (parallel)
          Fetch https://keap.com/pricing...
        • Write comparison report
          Synthesize all research into a markdown report...

        🚀 Workflow abc123 started — Claude is executing.
        Use /status abc123 to track.

[5 minutes later]

Jeremy: /status abc123

ZBOT:   📊 Workflow abc123 — 4/5 tasks complete
        ✅ Research competitor list
        ✅ Fetch HubSpot pricing
        ✅ Fetch ActiveCampaign pricing  
        ⏳ Fetch Keap pricing (running)
        ⏸ Write comparison report (waiting)
```

---

## Definition of Done

1. `/plan <goal>` in Slack triggers GPT-4o planning
2. GPT-4o returns a valid TaskGraph in under 30 seconds
3. Tasks appear in workflow DB and Claude starts executing
4. User sees the plan breakdown in Slack before execution starts
5. `/status <id>` shows real-time progress
6. Final outputs written to workspace and summarized in Slack
7. `go build ./... ` passes clean

---

## Estimated Work

| Task | Time |
|------|------|
| `internal/llm/openai.go` | 30 min |
| `internal/planner/planner.go` | 45 min |
| Wire into `wire.go` | 30 min |
| Interim Slack messaging | 30 min |
| Testing end to end | 45 min |
| **Total** | **~3.5 hours** |

---

## Notes

- GPT-4o chosen over o3 for speed. Can swap to o3 for deeper planning on complex tasks later.
- The planner prompt is the most important piece — iterate on it aggressively.
- If GPT-4o returns malformed JSON, retry once with a stricter prompt before failing.
- Future: let Claude report back to GPT-4o mid-execution ("task 3 failed, replanning needed")
- Future: GPT-4o can see Claude's outputs and adjust remaining tasks dynamically
- That second future item is where it gets genuinely interesting — adaptive dual-brain execution
