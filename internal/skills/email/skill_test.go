package email

import (
	"testing"
)

func TestSkillName(t *testing.T) {
	s := NewSkill("smtp.test.com", 587, "user@test.com", "pass", "from@test.com")
	if s.Name() != "email" {
		t.Fatalf("expected 'email', got %q", s.Name())
	}
}

func TestSkillToolCount(t *testing.T) {
	s := NewSkill("smtp.test.com", 587, "user@test.com", "pass", "from@test.com")
	tools := s.Tools()
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Name() != "send_email" {
		t.Fatalf("expected 'send_email', got %q", tools[0].Name())
	}
}

func TestToolDefinition(t *testing.T) {
	s := NewSkill("smtp.test.com", 587, "user@test.com", "pass", "from@test.com")
	def := s.Tools()[0].Definition()
	if def.Name != "send_email" {
		t.Fatalf("expected definition name 'send_email', got %q", def.Name)
	}
	if def.InputSchema == nil {
		t.Fatal("expected non-nil input schema")
	}
	props, ok := def.InputSchema["properties"]
	if !ok || props == nil {
		t.Fatal("expected properties in input schema")
	}
}
