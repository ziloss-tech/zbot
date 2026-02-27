package memory

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/jeremylerwick-max/zbot/internal/agent"
)

// ─── SAVE MEMORY TOOL ────────────────────────────────────────────────────────

// SaveMemoryTool persists a fact to long-term memory.
type SaveMemoryTool struct{ store agent.MemoryStore }

func NewSaveMemoryTool(store agent.MemoryStore) *SaveMemoryTool {
	return &SaveMemoryTool{store}
}

func (t *SaveMemoryTool) Name() string { return "save_memory" }

func (t *SaveMemoryTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "save_memory",
		Description: "Save an important fact to long-term memory. Use this when you learn something important about Jeremy, his business, preferences, or discover a useful insight during a task.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"fact"},
			"properties": map[string]any{
				"fact": map[string]any{
					"type":        "string",
					"description": "The fact to remember",
				},
				"category": map[string]any{
					"type":        "string",
					"description": "Category: preference, business, technical, personal, workflow_insight",
					"enum":        []string{"preference", "business", "technical", "personal", "workflow_insight"},
				},
			},
		},
	}
}

func (t *SaveMemoryTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	content, _ := input["fact"].(string)
	if content == "" {
		return &agent.ToolResult{Content: "error: fact is required", IsError: true}, nil
	}

	category, _ := input["category"].(string)
	if category == "" {
		category = "business"
	}

	fact := agent.Fact{
		ID:        randomID(),
		Content:   content,
		Source:    "agent",
		Tags:      []string{category},
		CreatedAt: time.Now(),
	}

	if err := t.store.Save(ctx, fact); err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("error saving: %v", err), IsError: true}, nil
	}

	return &agent.ToolResult{Content: fmt.Sprintf("Memory saved: %s", content)}, nil
}

var _ agent.Tool = (*SaveMemoryTool)(nil)

// ─── SEARCH MEMORY TOOL ─────────────────────────────────────────────────────

// SearchMemoryTool queries long-term memory for relevant facts.
type SearchMemoryTool struct{ store agent.MemoryStore }

func NewSearchMemoryTool(store agent.MemoryStore) *SearchMemoryTool {
	return &SearchMemoryTool{store}
}

func (t *SearchMemoryTool) Name() string { return "search_memory" }

func (t *SearchMemoryTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "search_memory",
		Description: "Search long-term memory for facts relevant to a topic. Use this when Jeremy references something from the past or when context from previous sessions would help.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"query"},
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "What to search for in memory",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Max results to return (default 5)",
				},
			},
		},
	}
}

func (t *SearchMemoryTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	query, _ := input["query"].(string)
	if query == "" {
		return &agent.ToolResult{Content: "error: query is required", IsError: true}, nil
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
		tags := ""
		if len(f.Tags) > 0 {
			tags = fmt.Sprintf(" [%s]", f.Tags[0])
		}
		out += fmt.Sprintf("%d.%s [%s] %s\n", i+1, tags, ageStr, f.Content)
	}

	return &agent.ToolResult{Content: out}, nil
}

var _ agent.Tool = (*SearchMemoryTool)(nil)

// ─── HELPERS ─────────────────────────────────────────────────────────────────

func randomID() string {
	b := make([]byte, 12)
	rand.Read(b)
	return hex.EncodeToString(b)
}
