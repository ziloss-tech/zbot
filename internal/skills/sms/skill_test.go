package sms

import (
	"context"
	"testing"
)

func TestSendSMSTool_Definition(t *testing.T) {
	skill := NewSkill("AC_test", "token_test", "+15555555555")
	tools := skill.Tools()
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	def := tools[0].Definition()
	if def.Name != "send_sms" {
		t.Errorf("tool name = %q, want send_sms", def.Name)
	}
	props, ok := def.InputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatal("InputSchema missing properties")
	}
	if _, ok := props["message"]; !ok {
		t.Error("missing 'message' property")
	}
	if _, ok := props["to"]; !ok {
		t.Error("missing 'to' property")
	}
}

func TestSendSMSTool_EmptyMessage(t *testing.T) {
	skill := NewSkill("AC_test", "token_test", "+15555555555")
	tool := skill.Tools()[0]
	result, err := tool.Execute(context.Background(), map[string]any{"message": ""})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for empty message")
	}
}

func TestSkillMetadata(t *testing.T) {
	skill := NewSkill("AC_test", "token_test", "+15555555555")
	if skill.Name() != "sms" {
		t.Errorf("Name() = %q, want sms", skill.Name())
	}
	if skill.SystemPromptAddendum() == "" {
		t.Error("SystemPromptAddendum should not be empty")
	}
}
