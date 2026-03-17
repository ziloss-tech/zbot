package prompts

import "strings"

// PromptProfile represents a routing decision that determines which
// prompt modules to assemble for a given turn.
type PromptProfile struct {
	// IncludeReasoning injects the Socratic/Aristotelian reasoning protocol.
	// Set true when socratic_mode is "minimal" or "deep", or when the task
	// involves non-trivial conclusions.
	IncludeReasoning bool

	// IncludeMemoryPolicy injects the memory read/write rules.
	// Set true when classification is "needs_memory" or at session start.
	IncludeMemoryPolicy bool

	// IncludeToolControl injects execution modes + confirmation gates.
	// Set true when the task involves tool use (classification is
	// "needs_tools" or "needs_plan").
	IncludeToolControl bool

	// IncludeVerification injects the tiered self-check schedule.
	// Set true when classification is "needs_verification" or when
	// the router flags confidence < 0.7.
	IncludeVerification bool

	// ExecutionMode overrides the default mode in tool_control.
	// Values: "chat", "safe_autopilot", "autopilot"
	ExecutionMode string

	// SocraticDepth overrides the default socratic mode.
	// Values: "skip", "minimal", "deep"
	SocraticDepth string
}

// DefaultProfile returns a profile suitable for most turns:
// memory policy + tool control active, reasoning and verification on standby.
func DefaultProfile() PromptProfile {
	return PromptProfile{
		IncludeReasoning:    false,
		IncludeMemoryPolicy: true,
		IncludeToolControl:  true,
		IncludeVerification: false,
		ExecutionMode:       "chat",
		SocraticDepth:       "skip",
	}
}

// FullProfile returns a profile with all modules active.
// Used for complex, high-stakes tasks.
func FullProfile() PromptProfile {
	return PromptProfile{
		IncludeReasoning:    true,
		IncludeMemoryPolicy: true,
		IncludeToolControl:  true,
		IncludeVerification: true,
		ExecutionMode:       "safe_autopilot",
		SocraticDepth:       "minimal",
	}
}

// ProfileFromRouter builds a PromptProfile from a router classification.
// This bridges the RouterSystem output to the prompt assembly.
func ProfileFromRouter(classification, socraticMode, executionMode string, confidence float64) PromptProfile {
	p := PromptProfile{
		ExecutionMode: executionMode,
		SocraticDepth: socraticMode,
	}

	// Reasoning module: include when socratic mode is active or task is complex.
	p.IncludeReasoning = socraticMode != "skip"

	// Memory policy: include when classification involves memory or at session start.
	p.IncludeMemoryPolicy = classification == "needs_memory" ||
		classification == "needs_tools" ||
		classification == "needs_plan"

	// Tool control: include when tools are involved.
	p.IncludeToolControl = classification == "needs_tools" ||
		classification == "needs_plan"

	// Verification: include when explicitly flagged or confidence is low.
	p.IncludeVerification = classification == "needs_verification" || confidence < 0.7

	return p
}

// BuildExecutorPrompt assembles the full system prompt for the Claude executor
// by composing the base executor prompt with the relevant modules.
//
// This replaces the old pattern of just using ClaudeExecutorSystem directly.
// The key insight from Part 2 research: modules are injected conditionally
// based on routing, not dumped wholesale into every turn.
//
// Token budget awareness:
//   - ClaudeExecutorSystem base:  ~800 tokens
//   - ReasoningModule:            ~650 tokens
//   - MemoryPolicyModule:         ~700 tokens
//   - ToolControlModule:          ~750 tokens
//   - VerificationModule:         ~800 tokens
//   - Skill addendum:             varies (~100-300 tokens)
//   - Memory injection:           varies (~50-400 tokens)
//
// Full load (all modules): ~3,700 tokens — well within budget.
// Typical turn (2-3 modules): ~2,000-2,500 tokens.
// Minimal turn (base only): ~800 tokens.
func BuildExecutorPrompt(profile PromptProfile, skillAddendum string) string {
	var b strings.Builder
	b.Grow(4096)

	// Base executor prompt is always included.
	b.WriteString(ClaudeExecutorSystem)

	// Conditionally inject modules.
	if profile.IncludeReasoning {
		b.WriteString("\n")
		b.WriteString(ReasoningModule)
	}

	if profile.IncludeMemoryPolicy {
		b.WriteString("\n")
		b.WriteString(MemoryPolicyModule)
	}

	if profile.IncludeToolControl {
		b.WriteString("\n")
		b.WriteString(ToolControlModule)

		// Inject current execution mode as an override.
		if profile.ExecutionMode != "" && profile.ExecutionMode != "chat" {
			b.WriteString("\n\n<execution_mode_override>")
			b.WriteString("Current mode: ")
			b.WriteString(strings.ToUpper(profile.ExecutionMode))
			b.WriteString("</execution_mode_override>")
		}
	}

	if profile.IncludeVerification {
		b.WriteString("\n")
		b.WriteString(VerificationModule)
	}

	// Skill addendum (registered tool descriptions from the skills system).
	if skillAddendum != "" {
		b.WriteString("\n")
		b.WriteString(skillAddendum)
	}

	return b.String()
}

// BuildChatPrompt assembles the system prompt for the casual chat path
// (direct Slack DMs, web UI quick chat). This is lighter than the executor
// prompt — no tool control or verification, but includes memory policy
// and optionally reasoning.
func BuildChatPrompt(baseChatPrompt, skillAddendum, memoryContext string, includeReasoning bool) string {
	var b strings.Builder
	b.Grow(4096)

	b.WriteString(baseChatPrompt)

	if includeReasoning {
		b.WriteString("\n")
		b.WriteString(ReasoningModule)
	}

	// Chat always gets memory policy (lighter touch — just the retrieval parts).
	b.WriteString("\n")
	b.WriteString(MemoryPolicyModule)

	if skillAddendum != "" {
		b.WriteString("\n")
		b.WriteString(skillAddendum)
	}

	if memoryContext != "" {
		b.WriteString("\n")
		b.WriteString(memoryContext)
	}

	return b.String()
}
