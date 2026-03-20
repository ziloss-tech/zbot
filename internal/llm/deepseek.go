// Package llm — DeepSeek V3.2 client via DeepInfra (OpenAI-compatible).
//
// DeepSeek V3.2 replaces Haiku as the "cheap smart" model for ZBOT's
// Pantheon cognitive stages (Frontal Lobe planning, Thalamus verification,
// Hypothalamus sentinel).
//
// Cost comparison:
//   - Haiku:         $0.25 / $1.25 per M (input/output)
//   - DeepSeek V3.2: $0.14 / $0.28 per M (input/output) via DeepInfra
//   - Savings: ~78% on output tokens, ~44% on input tokens
//
// DeepInfra hosts DeepSeek and speaks the OpenAI chat completions format,
// so this wraps OpenAICompatClient with the right defaults.
package llm

import (
	"log/slog"
)

// DeepSeek model constants.
const (
	// DeepSeekV3Model is the model ID on DeepInfra.
	DeepSeekV3Model = "deepseek-ai/DeepSeek-V3-0324"

	// DeepInfraBaseURL is the OpenAI-compatible endpoint for DeepInfra.
	DeepInfraBaseURL = "https://api.deepinfra.com/v1/openai"
)

// NewDeepSeekCheapClient creates a DeepSeek V3.2 client via DeepInfra.
// This is the cheapest smart model for the Pantheon's supporting brain regions:
//   - Frontal Lobe (planning): structured JSON plan output
//   - Thalamus (verification): Socratic self-check
//   - Hypothalamus (sentinel): background monitoring
//
// Uses OpenAI-compatible API format — no special SDK required.
// Falls back to nil if apiKey is empty (caller should handle gracefully).
func NewDeepSeekCheapClient(deepInfraAPIKey string, logger *slog.Logger) *OpenAICompatClient {
	return NewOpenAICompatClient(
		DeepInfraBaseURL,
		deepInfraAPIKey,
		DeepSeekV3Model,
		logger,
	)
}
