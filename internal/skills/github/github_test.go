package github

import (
	"testing"

	"github.com/ziloss-tech/zbot/internal/skills"
)

// Compile-time interface check.
var _ skills.Skill = (*Skill)(nil)

func TestGitHubSkillName(t *testing.T) {
	s := NewSkill("fake-token")
	if s.Name() != "github" {
		t.Errorf("expected 'github', got %q", s.Name())
	}
}

func TestGitHubToolCount(t *testing.T) {
	s := NewSkill("fake-token")
	tools := s.Tools()
	if len(tools) != 13 {
		t.Errorf("expected 13 GitHub tools, got %d", len(tools))
	}
}

func TestGitHubToolDefinitionsValid(t *testing.T) {
	s := NewSkill("fake-token")
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

func TestGitHubToolNamesUnique(t *testing.T) {
	s := NewSkill("fake-token")
	seen := make(map[string]bool)
	for _, tool := range s.Tools() {
		name := tool.Name()
		if seen[name] {
			t.Errorf("duplicate tool name: %q", name)
		}
		seen[name] = true
	}
}
