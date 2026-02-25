// Package tools — SearchMemoryTool lets the agent explicitly query its own long-term memory.
// Complementary to save_memory — this is the "recall" side.
package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/jeremylerwick-max/zbot/internal/agent"
)

// SearchMemoryTool implements agent.Tool for explicit memory search.
type SearchMemoryTool struct{ store agent.MemoryStore }

func NewSearchMemoryTool(store agent.MemoryStore) *SearchMemoryTool {
	return &SearchMemoryTool{store}
}

func (t *SearchMemoryTool) Name() string { return "search_memory" }

func (t *SearchMemoryTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "search_memory",
		Description: "Search your long-term memory for facts you've previously saved. Use when the user asks 'do you remember...' or 'what do you know about...'",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"query"},
			"properties": map[string]any{
				"query": map[string]any{"type": "string", "description": "What to search for"},
				"limit": map[string]any{"type": "integer", "description": "Max results (default 5)"},
			},
		},
	}
}

func (t *SearchMemoryTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	query, _ := input["query"].(string)
	if query == "" {
		return &agent.ToolResult{Content: "error: query required", IsError: true}, nil
	}
	limit := 5
	if l, ok := input["limit"].(float64); ok && l > 0 {
		limit = int(l)
	}
	facts, err := t.store.Search(ctx, query, limit)
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("search error: %v", err), IsError: true}, nil
	}
	if len(facts) == 0 {
		return &agent.ToolResult{Content: "No memories found for that query."}, nil
	}
	out := fmt.Sprintf("## Memory Search: %q\n\n", query)
	for i, f := range facts {
		age := time.Since(f.CreatedAt)
		ageStr := fmt.Sprintf("%.0f days ago", age.Hours()/24)
		if age.Hours() < 24 {
			ageStr = fmt.Sprintf("%.0f hours ago", age.Hours())
		}
		out += fmt.Sprintf("%d. [%s | %s] %s\n", i+1, f.Source, ageStr, f.Content)
	}
	return &agent.ToolResult{Content: out}, nil
}

var _ agent.Tool = (*SearchMemoryTool)(nil)
