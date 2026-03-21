package ghl

import (
	"testing"

	"github.com/ziloss-tech/zbot/internal/skills"
)

// Compile-time interface check.
var _ skills.Skill = (*Skill)(nil)

func TestGHLSkillName(t *testing.T) {
	s := NewSkill("fake-key", "fake-location")
	if s.Name() != "ghl" {
		t.Errorf("expected 'ghl', got %q", s.Name())
	}
}

func TestGHLToolCount(t *testing.T) {
	s := NewSkill("fake-key", "fake-location")
	tools := s.Tools()
	if len(tools) != 20 {
		t.Errorf("expected 20 GHL tools, got %d", len(tools))
	}
}

func TestGHLToolDefinitionsValid(t *testing.T) {
	s := NewSkill("fake-key", "fake-location")
	for _, tool := range s.Tools() {
		def := tool.Definition()
		if def.Name == "" {
			t.Error("tool has empty name")
		}
		if def.Description == "" {
			t.Errorf("tool %q has empty description", def.Name)
		}
		if def.InputSchema == nil {
			t.Errorf("tool %q has nil InputSchema", def.Name)
		}
	}
}

func TestGHLToolNamesUnique(t *testing.T) {
	s := NewSkill("fake-key", "fake-location")
	seen := make(map[string]bool)
	for _, tool := range s.Tools() {
		name := tool.Name()
		if seen[name] {
			t.Errorf("duplicate tool name: %q", name)
		}
		seen[name] = true
	}
}

func TestGHLSystemPromptAddendum(t *testing.T) {
	s := NewSkill("fake-key", "fake-location")
	addendum := s.SystemPromptAddendum()
	if addendum == "" {
		t.Error("system prompt addendum should not be empty")
	}
}
