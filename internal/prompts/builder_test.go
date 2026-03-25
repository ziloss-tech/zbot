package prompts

import (
	"strings"
	"testing"
)

func TestDefaultProfile(t *testing.T) {
	p := DefaultProfile()

	if p.IncludeReasoning {
		t.Error("DefaultProfile: expected IncludeReasoning=false")
	}
	if !p.IncludeMemoryPolicy {
		t.Error("DefaultProfile: expected IncludeMemoryPolicy=true")
	}
	if !p.IncludeToolControl {
		t.Error("DefaultProfile: expected IncludeToolControl=true")
	}
	if p.IncludeVerification {
		t.Error("DefaultProfile: expected IncludeVerification=false")
	}
	if p.ExecutionMode != "chat" {
		t.Errorf("DefaultProfile: expected ExecutionMode=chat, got %s", p.ExecutionMode)
	}
	if p.SocraticDepth != "skip" {
		t.Errorf("DefaultProfile: expected SocraticDepth=skip, got %s", p.SocraticDepth)
	}
}

func TestFullProfile(t *testing.T) {
	p := FullProfile()

	if !p.IncludeReasoning {
		t.Error("FullProfile: expected IncludeReasoning=true")
	}
	if !p.IncludeMemoryPolicy {
		t.Error("FullProfile: expected IncludeMemoryPolicy=true")
	}
	if !p.IncludeToolControl {
		t.Error("FullProfile: expected IncludeToolControl=true")
	}
	if !p.IncludeVerification {
		t.Error("FullProfile: expected IncludeVerification=true")
	}
	if p.ExecutionMode != "safe_autopilot" {
		t.Errorf("FullProfile: expected ExecutionMode=safe_autopilot, got %s", p.ExecutionMode)
	}
	if p.SocraticDepth != "minimal" {
		t.Errorf("FullProfile: expected SocraticDepth=minimal, got %s", p.SocraticDepth)
	}
}

func TestProfileFromRouter(t *testing.T) {
	tests := []struct {
		name              string
		classification    string
		socraticMode      string
		executionMode     string
		confidence        float64
		expectedReasoning bool
		expectedMemory    bool
		expectedTool      bool
		expectedVerify    bool
	}{
		{
			name:              "needs_memory",
			classification:    "needs_memory",
			socraticMode:      "skip",
			executionMode:     "chat",
			confidence:        0.9,
			expectedReasoning: false,
			expectedMemory:    true,
			expectedTool:      false,
			expectedVerify:    false,
		},
		{
			name:              "needs_tools",
			classification:    "needs_tools",
			socraticMode:      "skip",
			executionMode:     "chat",
			confidence:        0.9,
			expectedReasoning: false,
			expectedMemory:    true,
			expectedTool:      true,
			expectedVerify:    false,
		},
		{
			name:              "needs_plan",
			classification:    "needs_plan",
			socraticMode:      "minimal",
			executionMode:     "autopilot",
			confidence:        0.9,
			expectedReasoning: true,
			expectedMemory:    true,
			expectedTool:      true,
			expectedVerify:    false,
		},
		{
			name:              "low_confidence",
			classification:    "chat",
			socraticMode:      "skip",
			executionMode:     "chat",
			confidence:        0.6,
			expectedReasoning: false,
			expectedMemory:    false,
			expectedTool:      false,
			expectedVerify:    true,
		},
		{
			name:              "needs_verification",
			classification:    "needs_verification",
			socraticMode:      "deep",
			executionMode:     "safe_autopilot",
			confidence:        0.95,
			expectedReasoning: true,
			expectedMemory:    false,
			expectedTool:      false,
			expectedVerify:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := ProfileFromRouter(tt.classification, tt.socraticMode, tt.executionMode, tt.confidence)

			if p.IncludeReasoning != tt.expectedReasoning {
				t.Errorf("IncludeReasoning: expected %v, got %v", tt.expectedReasoning, p.IncludeReasoning)
			}
			if p.IncludeMemoryPolicy != tt.expectedMemory {
				t.Errorf("IncludeMemoryPolicy: expected %v, got %v", tt.expectedMemory, p.IncludeMemoryPolicy)
			}
			if p.IncludeToolControl != tt.expectedTool {
				t.Errorf("IncludeToolControl: expected %v, got %v", tt.expectedTool, p.IncludeToolControl)
			}
			if p.IncludeVerification != tt.expectedVerify {
				t.Errorf("IncludeVerification: expected %v, got %v", tt.expectedVerify, p.IncludeVerification)
			}
			if p.ExecutionMode != tt.executionMode {
				t.Errorf("ExecutionMode: expected %s, got %s", tt.executionMode, p.ExecutionMode)
			}
			if p.SocraticDepth != tt.socraticMode {
				t.Errorf("SocraticDepth: expected %s, got %s", tt.socraticMode, p.SocraticDepth)
			}
		})
	}
}

func TestBuildExecutorPromptBase(t *testing.T) {
	profile := PromptProfile{
		IncludeReasoning:    false,
		IncludeMemoryPolicy: false,
		IncludeToolControl:  false,
		IncludeVerification: false,
		ExecutionMode:       "chat",
		SocraticDepth:       "skip",
	}

	prompt := BuildExecutorPrompt(profile, "")

	if prompt == "" {
		t.Error("expected non-empty prompt")
	}
	if !strings.Contains(prompt, ClaudeExecutorSystem) {
		t.Error("expected base executor system prompt to be included")
	}
	if strings.Contains(prompt, ReasoningModule) {
		t.Error("did not expect reasoning module when IncludeReasoning=false")
	}
}

func TestBuildExecutorPromptWithReasoning(t *testing.T) {
	profile := PromptProfile{
		IncludeReasoning:    true,
		IncludeMemoryPolicy: false,
		IncludeToolControl:  false,
		IncludeVerification: false,
		ExecutionMode:       "chat",
		SocraticDepth:       "minimal",
	}

	prompt := BuildExecutorPrompt(profile, "")

	if !strings.Contains(prompt, ClaudeExecutorSystem) {
		t.Error("expected base executor system prompt")
	}
	if !strings.Contains(prompt, ReasoningModule) {
		t.Error("expected reasoning module")
	}
}

func TestBuildExecutorPromptWithMemory(t *testing.T) {
	profile := PromptProfile{
		IncludeReasoning:    false,
		IncludeMemoryPolicy: true,
		IncludeToolControl:  false,
		IncludeVerification: false,
		ExecutionMode:       "chat",
		SocraticDepth:       "skip",
	}

	prompt := BuildExecutorPrompt(profile, "")

	if !strings.Contains(prompt, ClaudeExecutorSystem) {
		t.Error("expected base executor system prompt")
	}
	if !strings.Contains(prompt, MemoryPolicyModule) {
		t.Error("expected memory policy module")
	}
}

func TestBuildExecutorPromptWithToolControl(t *testing.T) {
	profile := PromptProfile{
		IncludeReasoning:    false,
		IncludeMemoryPolicy: false,
		IncludeToolControl:  true,
		IncludeVerification: false,
		ExecutionMode:       "chat",
		SocraticDepth:       "skip",
	}

	prompt := BuildExecutorPrompt(profile, "")

	if !strings.Contains(prompt, ToolControlModule) {
		t.Error("expected tool control module")
	}
}

func TestBuildExecutorPromptWithExecutionModeOverride(t *testing.T) {
	profile := PromptProfile{
		IncludeReasoning:    false,
		IncludeMemoryPolicy: false,
		IncludeToolControl:  true,
		IncludeVerification: false,
		ExecutionMode:       "safe_autopilot",
		SocraticDepth:       "skip",
	}

	prompt := BuildExecutorPrompt(profile, "")

	if !strings.Contains(prompt, "execution_mode_override") {
		t.Error("expected execution_mode_override tag")
	}
	if !strings.Contains(prompt, "SAFE_AUTOPILOT") {
		t.Error("expected uppercased execution mode")
	}
}

func TestBuildExecutorPromptWithVerification(t *testing.T) {
	profile := PromptProfile{
		IncludeReasoning:    false,
		IncludeMemoryPolicy: false,
		IncludeToolControl:  false,
		IncludeVerification: true,
		ExecutionMode:       "chat",
		SocraticDepth:       "skip",
	}

	prompt := BuildExecutorPrompt(profile, "")

	if !strings.Contains(prompt, VerificationModule) {
		t.Error("expected verification module")
	}
}

func TestBuildExecutorPromptWithSkillAddendum(t *testing.T) {
	profile := PromptProfile{
		IncludeReasoning:    false,
		IncludeMemoryPolicy: false,
		IncludeToolControl:  false,
		IncludeVerification: false,
		ExecutionMode:       "chat",
		SocraticDepth:       "skip",
	}

	skillAddendum := "## Available Tools\n- Tool 1\n- Tool 2"
	prompt := BuildExecutorPrompt(profile, skillAddendum)

	if !strings.Contains(prompt, skillAddendum) {
		t.Error("expected skill addendum in prompt")
	}
}

func TestBuildExecutorPromptFullProfile(t *testing.T) {
	profile := FullProfile()
	skillAddendum := "## Tools"

	prompt := BuildExecutorPrompt(profile, skillAddendum)

	// Verify all modules are included
	if !strings.Contains(prompt, ClaudeExecutorSystem) {
		t.Error("expected executor system")
	}
	if !strings.Contains(prompt, ReasoningModule) {
		t.Error("expected reasoning module")
	}
	if !strings.Contains(prompt, MemoryPolicyModule) {
		t.Error("expected memory policy module")
	}
	if !strings.Contains(prompt, ToolControlModule) {
		t.Error("expected tool control module")
	}
	if !strings.Contains(prompt, VerificationModule) {
		t.Error("expected verification module")
	}
	if !strings.Contains(prompt, skillAddendum) {
		t.Error("expected skill addendum")
	}
}

func TestBuildChatPromptBasic(t *testing.T) {
	baseChatPrompt := "You are a helpful assistant"
	skillAddendum := "## Tools"
	memoryContext := "Remember: Important facts"

	prompt := BuildChatPrompt(baseChatPrompt, skillAddendum, memoryContext, false)

	if !strings.Contains(prompt, baseChatPrompt) {
		t.Error("expected base chat prompt")
	}
	if !strings.Contains(prompt, MemoryPolicyModule) {
		t.Error("expected memory policy module (always included in chat)")
	}
	if !strings.Contains(prompt, skillAddendum) {
		t.Error("expected skill addendum")
	}
	if !strings.Contains(prompt, memoryContext) {
		t.Error("expected memory context")
	}
	if strings.Contains(prompt, ReasoningModule) {
		t.Error("did not expect reasoning module when includeReasoning=false")
	}
}

func TestBuildChatPromptWithReasoning(t *testing.T) {
	baseChatPrompt := "You are helpful"
	skillAddendum := ""
	memoryContext := ""

	prompt := BuildChatPrompt(baseChatPrompt, skillAddendum, memoryContext, true)

	if !strings.Contains(prompt, baseChatPrompt) {
		t.Error("expected base chat prompt")
	}
	if !strings.Contains(prompt, ReasoningModule) {
		t.Error("expected reasoning module when includeReasoning=true")
	}
}

func TestBuildChatPromptEmptyMemoryContext(t *testing.T) {
	baseChatPrompt := "Assistant"
	skillAddendum := "Tools"
	memoryContext := ""

	prompt := BuildChatPrompt(baseChatPrompt, skillAddendum, memoryContext, false)

	if !strings.Contains(prompt, baseChatPrompt) {
		t.Error("expected base chat prompt")
	}
	if !strings.Contains(prompt, MemoryPolicyModule) {
		t.Error("expected memory policy module")
	}
	if !strings.Contains(prompt, skillAddendum) {
		t.Error("expected skill addendum")
	}
}

func TestPromptAssemblyNoDoubleNewlines(t *testing.T) {
	profile := PromptProfile{
		IncludeReasoning:    true,
		IncludeMemoryPolicy: true,
		IncludeToolControl:  true,
		IncludeVerification: true,
		ExecutionMode:       "chat",
		SocraticDepth:       "skip",
	}

	prompt := BuildExecutorPrompt(profile, "")

	// Check no consecutive newlines (should have \n\n only for section breaks)
	if strings.Contains(prompt, "\n\n\n") {
		t.Error("found excessive newlines in prompt")
	}
}

func TestExecutionModeChat(t *testing.T) {
	profile := PromptProfile{
		IncludeToolControl: true,
		ExecutionMode:      "chat",
	}

	prompt := BuildExecutorPrompt(profile, "")

	// "chat" mode should not have execution_mode_override
	if strings.Contains(prompt, "execution_mode_override") {
		t.Error("chat mode should not have execution_mode_override")
	}
}
