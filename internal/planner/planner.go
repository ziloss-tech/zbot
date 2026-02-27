package planner

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jeremylerwick-max/zbot/internal/agent"
	"github.com/jeremylerwick-max/zbot/internal/prompts"
)

// TaskGraph is the structured plan GPT-4o returns.
type TaskGraph struct {
	Goal       string        `json:"goal"`
	TotalSteps int           `json:"total_steps"`
	Tasks      []PlannedTask `json:"tasks"`
}

// PlannedTask is one unit of work in the plan.
type PlannedTask struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Instruction string   `json:"instruction"`
	DependsOn   []string `json:"depends_on"`
	Parallel    bool     `json:"parallel"`
	ToolHints   []string `json:"tool_hints"`
	Priority    int      `json:"priority"`
}

// ChatClient is the interface the planner uses — satisfied by llm.OpenAIClient.
type ChatClient interface {
	Chat(ctx context.Context, systemPrompt, userMsg string) (string, error)
	ChatStream(ctx context.Context, systemPrompt, userMsg string, tokens chan<- string) (string, error)
	ModelName() string
}

// Planner uses GPT-4o to decompose goals into structured task graphs.
type Planner struct {
	client ChatClient
	logger *slog.Logger
	memory agent.MemoryStore // optional: inject relevant memories into planning context
}

// New creates a new Planner.
func New(client ChatClient, logger *slog.Logger) *Planner {
	return &Planner{client: client, logger: logger}
}

// SetMemoryStore wires the memory store into the planner for context injection.
func (p *Planner) SetMemoryStore(mem agent.MemoryStore) {
	p.memory = mem
}

// fetchMemoryContext searches memory for facts relevant to the goal and formats them.
func (p *Planner) fetchMemoryContext(ctx context.Context, goal string) string {
	if p.memory == nil {
		return ""
	}

	facts, err := p.memory.Search(ctx, goal, 5)
	if err != nil {
		p.logger.Warn("planner memory search failed, continuing without", "err", err)
		return ""
	}
	if len(facts) == 0 {
		p.logger.Debug("planner memory: no relevant facts found", "goal", goal)
		return ""
	}

	p.logger.Info("memory: injected facts into planner",
		"component", "planner",
		"count", len(facts),
	)

	var sb strings.Builder
	sb.WriteString("## Relevant Context From Memory\n")
	for _, f := range facts {
		sb.WriteString(fmt.Sprintf("- %s\n", f.Content))
	}
	sb.WriteString("\n")
	return sb.String()
}

// Plan calls GPT-4o and returns a TaskGraph for the given goal.
func (p *Planner) Plan(ctx context.Context, goal string) (*TaskGraph, error) {
	start := time.Now()
	p.logger.Info("planning goal",
		"component", "planner",
		"model", p.client.ModelName(),
		"goal", goal,
	)

	// Inject relevant memories into the system prompt.
	systemPrompt := prompts.GPTPlannerSystem
	if memCtx := p.fetchMemoryContext(ctx, goal); memCtx != "" {
		systemPrompt = memCtx + systemPrompt
	}

	raw, err := p.client.Chat(ctx, systemPrompt, "Goal: "+goal)
	if err != nil {
		return nil, fmt.Errorf("planner LLM call: %w", err)
	}

	// Strip markdown fences if model ignored instructions
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	var graph TaskGraph
	if err := json.Unmarshal([]byte(raw), &graph); err != nil {
		// Retry once with stricter prompt
		p.logger.Warn("planner returned invalid JSON, retrying", "raw", raw[:min(200, len(raw))])
		raw2, err2 := p.client.Chat(ctx, systemPrompt,
			"Goal: "+goal+"\n\nCRITICAL: Return ONLY raw JSON. No markdown. No explanation. Start with { and end with }")
		if err2 != nil {
			return nil, fmt.Errorf("planner retry failed: %w", err2)
		}
		raw2 = strings.TrimSpace(raw2)
		if err3 := json.Unmarshal([]byte(raw2), &graph); err3 != nil {
			return nil, fmt.Errorf("planner returned invalid JSON after retry: %w", err3)
		}
	}

	if len(graph.Tasks) == 0 {
		return nil, fmt.Errorf("planner returned empty task list")
	}

	// Normalize: ensure total_steps matches actual task count
	graph.TotalSteps = len(graph.Tasks)

	p.logger.Info("plan ready",
		"component", "planner",
		"goal", goal,
		"tasks", len(graph.Tasks),
		"duration_ms", time.Since(start).Milliseconds(),
	)
	return &graph, nil
}

// PlanStream calls GPT-4o with streaming and sends tokens to the provided channel.
// When planning is complete, it parses the full JSON and returns the TaskGraph.
// The tokens channel is NOT closed by this method — caller owns it.
func (p *Planner) PlanStream(ctx context.Context, goal string, tokens chan<- string) (*TaskGraph, error) {
	start := time.Now()
	p.logger.Info("plan stream starting",
		"component", "planner",
		"model", p.client.ModelName(),
		"goal", goal,
	)

	// Inject relevant memories into the system prompt.
	systemPrompt := prompts.GPTPlannerSystem
	if memCtx := p.fetchMemoryContext(ctx, goal); memCtx != "" {
		systemPrompt = memCtx + systemPrompt
	}

	raw, err := p.client.ChatStream(ctx, systemPrompt, "Goal: "+goal, tokens)
	if err != nil {
		return nil, fmt.Errorf("planner stream LLM call: %w", err)
	}

	// Strip markdown fences if model ignored instructions
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	var graph TaskGraph
	if err := json.Unmarshal([]byte(raw), &graph); err != nil {
		// Retry once with stricter prompt (non-streaming fallback)
		p.logger.Warn("planner stream returned invalid JSON, retrying non-stream",
			"raw", raw[:min(200, len(raw))])
		raw2, err2 := p.client.Chat(ctx, systemPrompt,
			"Goal: "+goal+"\n\nCRITICAL: Return ONLY raw JSON. No markdown. No explanation. Start with { and end with }")
		if err2 != nil {
			return nil, fmt.Errorf("planner retry failed: %w", err2)
		}
		raw2 = strings.TrimSpace(raw2)
		if err3 := json.Unmarshal([]byte(raw2), &graph); err3 != nil {
			return nil, fmt.Errorf("planner returned invalid JSON after retry: %w", err3)
		}
	}

	if len(graph.Tasks) == 0 {
		return nil, fmt.Errorf("planner returned empty task list")
	}

	graph.TotalSteps = len(graph.Tasks)
	graph.Goal = goal

	p.logger.Info("plan stream complete",
		"component", "planner",
		"goal", goal,
		"tasks", len(graph.Tasks),
		"duration_ms", time.Since(start).Milliseconds(),
	)
	return &graph, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
