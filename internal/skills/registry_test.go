package skills

import (
	"testing"

	"github.com/ziloss-tech/zbot/internal/agent"
)

// mockSkill implements the Skill interface for testing.
type mockSkill struct {
	name  string
	tools []agent.Tool
}

func (m *mockSkill) Name() string                   { return m.name }
func (m *mockSkill) Description() string             { return "test skill" }
func (m *mockSkill) Tools() []agent.Tool             { return m.tools }
func (m *mockSkill) SystemPromptAddendum() string    { return "Use " + m.name }

func TestRegistryRegisterAndNames(t *testing.T) {
	r := NewRegistry()
	r.Register(&mockSkill{name: "alpha", tools: nil})
	r.Register(&mockSkill{name: "beta", tools: nil})

	names := r.Names()
	if len(names) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(names))
	}
}

func TestRegistryAllTools(t *testing.T) {
	r := NewRegistry()
	r.Register(&mockSkill{name: "s1", tools: nil})
	tools := r.AllTools()
	// With no tools registered, AllTools returns empty (nil or empty slice)
	if len(tools) != 0 {
		t.Fatalf("expected 0 tools from skill with nil tools, got %d", len(tools))
	}
}

func TestRegistrySystemPromptAddendum(t *testing.T) {
	r := NewRegistry()
	r.Register(&mockSkill{name: "test", tools: nil})
	addendum := r.SystemPromptAddendum()
	if addendum == "" {
		t.Fatal("expected non-empty addendum")
	}
}
