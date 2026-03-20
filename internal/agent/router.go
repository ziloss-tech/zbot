// Package agent — router.go implements the v2 single-brain model tier selection.
//
// The model router replaces the old multi-model orchestration (planner/executor/critic).
// Instead of routing between different AI systems, the router selects the appropriate
// cost/capability tier within the same model family (Haiku → Sonnet → Opus).
//
// Escalation is one-shot: Opus handles the full task, it does not hand back to Sonnet.
package agent

import (
	"strings"
)

// RouterConfig controls model escalation behavior.
type RouterConfig struct {
	// AutoEscalate enables automatic escalation to Opus on complex tasks.
	AutoEscalate bool

	// ToolChainDepthThreshold escalates to Opus if a task requires more than
	// this many sequential tool calls. Default: 5.
	ToolChainDepthThreshold int

	// TokenBudgetPct escalates to Opus if Sonnet uses more than this percentage
	// of its max token budget. Default: 90.
	TokenBudgetPct int
}

// DefaultRouterConfig returns safe production defaults.
func DefaultRouterConfig() RouterConfig {
	return RouterConfig{
		AutoEscalate:            true,
		ToolChainDepthThreshold: 5,
		TokenBudgetPct:          90,
	}
}

// ResolveModelTier determines the effective model tier for a request.
//
// Priority order:
//  1. Explicit ModelHint in context (set by caller/orchestrator)
//  2. User command signals (/think → Opus)
//  3. Content-based heuristics (simple greeting → Haiku)
//  4. Default: Sonnet
func ResolveModelTier(hint string, userContent string) ModelTier {
	// Priority 1: Explicit hint from caller.
	switch strings.ToLower(hint) {
	case "cheap", "deepseek":
		return ModelTierCheap
	case "haiku":
		return ModelTierHaiku
	case "smart", "sonnet":
		return ModelTierSonnet
	case "opus", "think":
		return ModelTierOpus
	case "auto", "":
		// fall through to content-based routing
	default:
		// Assume it's a specific model name — treat as explicit Sonnet-tier
		// (the LLM adapter handles mapping to actual model strings).
		return ModelTierSonnet
	}

	// Priority 2: User command signals.
	if strings.Contains(userContent, "/think") || strings.Contains(userContent, "/deep") {
		return ModelTierOpus
	}

	// Priority 3: Simple messages route to cheap model (DeepSeek V3.2).
	if IsSimpleMessage(userContent) {
		return ModelTierCheap
	}

	// Priority 4: Default to Sonnet.
	return ModelTierSonnet
}

// ShouldEscalate checks whether an in-progress task should be escalated to Opus.
// Called after each tool round to detect complexity that wasn't apparent upfront.
func ShouldEscalate(cfg RouterConfig, currentTier ModelTier, toolRound int, tokensPct float64) bool {
	if !cfg.AutoEscalate {
		return false
	}

	// Already at Opus — can't escalate further.
	if currentTier == ModelTierOpus {
		return false
	}

	// Tool chain depth exceeded.
	if toolRound >= cfg.ToolChainDepthThreshold {
		return true
	}

	// Token budget nearly exhausted.
	if tokensPct >= float64(cfg.TokenBudgetPct) {
		return true
	}

	return false
}

// IsSimpleMessage returns true for short, simple messages that don't need
// the full Sonnet model (greetings, acknowledgments, etc.).
// Exported for use by both the router and LLM adapters.
func IsSimpleMessage(text string) bool {
	if len(text) > 100 {
		return false
	}
	lower := strings.ToLower(strings.TrimSpace(text))
	simplePatterns := []string{
		"hello", "hi", "hey", "thanks", "thank you",
		"ok", "okay", "got it", "cool", "nice",
		"yes", "no", "yep", "nope", "sup", "yo",
		"good morning", "good night", "gm", "gn",
	}
	for _, p := range simplePatterns {
		if lower == p || lower == p+"!" || lower == p+"." {
			return true
		}
	}
	return false
}

// ModelTierCost returns the per-token cost (input, output) in USD per token
// for each model tier. Used for accurate cost tracking.
func ModelTierCost(tier ModelTier) (inputCostPerToken, outputCostPerToken float64) {
	switch tier {
	case ModelTierCheap:
		return 0.00000014, 0.00000028 // $0.14 / $0.28 per million (DeepSeek V3.2 via DeepInfra)
	case ModelTierHaiku:
		return 0.00000025, 0.00000125 // $0.25 / $1.25 per million
	case ModelTierSonnet:
		return 0.000003, 0.000015 // $3 / $15 per million
	case ModelTierOpus:
		return 0.000015, 0.000075 // $15 / $75 per million
	default:
		return 0.000003, 0.000015 // default to Sonnet pricing
	}
}
