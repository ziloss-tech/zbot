package agent

import (
	"testing"
)

func TestResolveModelTier_ExplicitHints(t *testing.T) {
	tests := []struct {
		hint    string
		content string
		want    ModelTier
	}{
		{"cheap", "anything", ModelTierHaiku},
		{"haiku", "anything", ModelTierHaiku},
		{"smart", "anything", ModelTierSonnet},
		{"sonnet", "anything", ModelTierSonnet},
		{"opus", "anything", ModelTierOpus},
		{"think", "anything", ModelTierOpus},
		{"CHEAP", "anything", ModelTierHaiku},  // case insensitive
		{"OPUS", "anything", ModelTierOpus},     // case insensitive
	}

	for _, tt := range tests {
		got := ResolveModelTier(tt.hint, tt.content)
		if got != tt.want {
			t.Errorf("ResolveModelTier(%q, %q) = %q, want %q", tt.hint, tt.content, got, tt.want)
		}
	}
}

func TestResolveModelTier_UserCommands(t *testing.T) {
	tests := []struct {
		content string
		want    ModelTier
	}{
		{"/think what is the meaning of life?", ModelTierOpus},
		{"please /deep research AR displays", ModelTierOpus},
		{"normal question about golang", ModelTierSonnet},
	}

	for _, tt := range tests {
		got := ResolveModelTier("", tt.content) // no hint
		if got != tt.want {
			t.Errorf("ResolveModelTier('', %q) = %q, want %q", tt.content, got, tt.want)
		}
	}
}

func TestResolveModelTier_SimpleMessages(t *testing.T) {
	simpleMessages := []string{
		"hello", "hi", "hey", "thanks", "ok", "yes", "no",
		"Hello!", "Thanks.", "Yep!", "Nope.",
		"good morning", "gm", "gn",
	}

	for _, msg := range simpleMessages {
		got := ResolveModelTier("", msg)
		if got != ModelTierHaiku {
			t.Errorf("ResolveModelTier('', %q) = %q, want haiku (simple message)", msg, got)
		}
	}
}

func TestResolveModelTier_DefaultSonnet(t *testing.T) {
	normalMessages := []string{
		"What are the best practices for Go error handling?",
		"Research the AR display market for Z-Glass",
		"Write me a Python script that calculates compound interest",
	}

	for _, msg := range normalMessages {
		got := ResolveModelTier("", msg)
		if got != ModelTierSonnet {
			t.Errorf("ResolveModelTier('', %q) = %q, want sonnet (default)", msg, got)
		}
	}
}

func TestShouldEscalate(t *testing.T) {
	cfg := DefaultRouterConfig()

	tests := []struct {
		name       string
		tier       ModelTier
		toolRound  int
		tokensPct  float64
		want       bool
	}{
		{"already opus", ModelTierOpus, 10, 95, false},
		{"low complexity", ModelTierSonnet, 2, 30, false},
		{"tool depth exceeded", ModelTierSonnet, 6, 30, true},
		{"token budget exceeded", ModelTierSonnet, 2, 91, true},
		{"both thresholds", ModelTierSonnet, 6, 95, true},
		{"haiku can escalate", ModelTierHaiku, 6, 30, true},
		{"at exact threshold", ModelTierSonnet, 5, 90, true},
		{"just below threshold", ModelTierSonnet, 4, 89, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShouldEscalate(cfg, tt.tier, tt.toolRound, tt.tokensPct)
			if got != tt.want {
				t.Errorf("ShouldEscalate(tier=%q, round=%d, pct=%.0f) = %v, want %v",
					tt.tier, tt.toolRound, tt.tokensPct, got, tt.want)
			}
		})
	}
}

func TestShouldEscalate_Disabled(t *testing.T) {
	cfg := DefaultRouterConfig()
	cfg.AutoEscalate = false

	// Even with exceeded thresholds, escalation should be disabled.
	got := ShouldEscalate(cfg, ModelTierSonnet, 10, 99)
	if got {
		t.Error("ShouldEscalate with AutoEscalate=false should always return false")
	}
}

func TestIsSimpleMessage(t *testing.T) {
	simple := []string{"hi", "hello!", "thanks.", "ok", "yes", "no", "gm"}
	complex := []string{
		"What is the capital of France?",
		"Research the top 5 competitors",
		"This is a long message that definitely requires more than haiku to process properly",
	}

	for _, s := range simple {
		if !IsSimpleMessage(s) {
			t.Errorf("IsSimpleMessage(%q) = false, want true", s)
		}
	}
	for _, c := range complex {
		if IsSimpleMessage(c) {
			t.Errorf("IsSimpleMessage(%q) = true, want false", c)
		}
	}
}

func TestModelTierCost(t *testing.T) {
	tests := []struct {
		tier       ModelTier
		wantInput  float64
		wantOutput float64
	}{
		{ModelTierHaiku, 0.00000025, 0.00000125},
		{ModelTierSonnet, 0.000003, 0.000015},
		{ModelTierOpus, 0.000015, 0.000075},
		{ModelTierAuto, 0.000003, 0.000015}, // defaults to Sonnet
	}

	for _, tt := range tests {
		inCost, outCost := ModelTierCost(tt.tier)
		if inCost != tt.wantInput {
			t.Errorf("ModelTierCost(%q) input = %v, want %v", tt.tier, inCost, tt.wantInput)
		}
		if outCost != tt.wantOutput {
			t.Errorf("ModelTierCost(%q) output = %v, want %v", tt.tier, outCost, tt.wantOutput)
		}
	}
}
