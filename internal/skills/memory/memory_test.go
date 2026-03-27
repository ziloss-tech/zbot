package memory

import (
	"context"
	"testing"

	"github.com/ziloss-tech/zbot/internal/agent"
)

type mockMemoryStore struct {
	saved    []agent.Fact
	results  []agent.Fact
}

func (m *mockMemoryStore) Save(_ context.Context, fact agent.Fact) error {
	m.saved = append(m.saved, fact)
	return nil
}

func (m *mockMemoryStore) Search(_ context.Context, _ string, limit int) ([]agent.Fact, error) {
	if limit > len(m.results) {
		return m.results, nil
	}
	return m.results[:limit], nil
}

func (m *mockMemoryStore) Delete(_ context.Context, _ string) error { return nil }

func TestSkillName(t *testing.T) {
	s := NewSkill(&mockMemoryStore{})
	if s.Name() != "memory" {
		t.Fatalf("expected 'memory', got %q", s.Name())
	}
}

func TestSkillHasTwoTools(t *testing.T) {
	s := NewSkill(&mockMemoryStore{})
	tools := s.Tools()
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}
	names := map[string]bool{}
	for _, tool := range tools {
		names[tool.Name()] = true
	}
	if !names["save_memory"] || !names["search_memory"] {
		t.Fatalf("expected save_memory and search_memory, got %v", names)
	}
}

func TestSaveMemoryTool(t *testing.T) {
	store := &mockMemoryStore{}
	tool := NewSaveMemoryTool(store)
	result, err := tool.Execute(context.Background(), map[string]any{
		"fact":   "Jeremy is the CEO of Ziloss",
		"source": "test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content)
	}
	if len(store.saved) != 1 {
		t.Fatalf("expected 1 saved fact, got %d", len(store.saved))
	}
}

func TestSearchMemoryTool(t *testing.T) {
	store := &mockMemoryStore{
		results: []agent.Fact{
			{ID: "f1", Content: "Jeremy runs Lead Certain", Source: "test"},
		},
	}
	tool := NewSearchMemoryTool(store)
	result, err := tool.Execute(context.Background(), map[string]any{
		"query": "who is Jeremy",
		"limit": float64(5),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content)
	}
}

func TestSaveMemoryMissingContent(t *testing.T) {
	store := &mockMemoryStore{}
	tool := NewSaveMemoryTool(store)
	result, err := tool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for missing content")
	}
}
