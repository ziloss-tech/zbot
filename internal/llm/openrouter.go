package llm

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	openai "github.com/sashabaranov/go-openai"

	"github.com/jeremylerwick-max/zbot/internal/agent"
)

// OpenRouter base URL — uses OpenAI-compatible API format.
const openRouterBaseURL = "https://openrouter.ai/api/v1"

// OpenRouterClient wraps the OpenAI SDK pointed at OpenRouter.
// Provides access to cheap, high-quality models (Mistral, Llama, etc.).
type OpenRouterClient struct {
	client      *openai.Client
	model       string // OpenRouter model ID, e.g. "mistralai/mistral-large"
	displayName string // e.g. "Mistral Large 2 · Mistral AI"
	logger      *slog.Logger
}

// NewOpenRouterClient creates an OpenRouter client for a specific model.
// apiKey: OpenRouter API key from Secret Manager ("openrouter-api-key").
// model: OpenRouter model ID (e.g. "mistralai/mistral-large").
func NewOpenRouterClient(apiKey, model string, logger *slog.Logger) *OpenRouterClient {
	cfg := openai.DefaultConfig(apiKey)
	cfg.BaseURL = openRouterBaseURL

	display := model
	if name, ok := ModelDisplayNames[model]; ok {
		display = name
	}

	return &OpenRouterClient{
		client:      openai.NewClientWithConfig(cfg),
		model:       model,
		displayName: display,
		logger:      logger,
	}
}

// ModelName returns the OpenRouter model ID.
func (c *OpenRouterClient) ModelName() string { return c.model }

// DisplayName returns the human-readable model name for the UI.
func (c *OpenRouterClient) DisplayName() string { return c.displayName }

// Chat sends a system prompt + user message and returns the response text.
// Uses JSON response format for structured output.
func (c *OpenRouterClient) Chat(ctx context.Context, systemPrompt, userMsg string) (string, error) {
	if IsModelBlocked(c.model) {
		return "", fmt.Errorf("model %q is blocked by policy", c.model)
	}

	c.logger.Debug("openrouter chat", "model", c.model, "user_len", len(userMsg))

	resp, err := c.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: c.model,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: systemPrompt},
			{Role: openai.ChatMessageRoleUser, Content: userMsg},
		},
		ResponseFormat: &openai.ChatCompletionResponseFormat{
			Type: openai.ChatCompletionResponseFormatTypeJSONObject,
		},
		Temperature: 0.2,
		MaxTokens:   8192,
	})
	if err != nil {
		return "", fmt.Errorf("openrouter chat (%s): %w", c.displayName, err)
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("openrouter (%s) returned no choices", c.displayName)
	}

	content := resp.Choices[0].Message.Content
	c.logger.Debug("openrouter response",
		"model", c.model,
		"display", c.displayName,
		"len", len(content),
		"prompt_tokens", resp.Usage.PromptTokens,
		"completion_tokens", resp.Usage.CompletionTokens,
	)

	return content, nil
}

// ChatFreeform sends a system prompt + user message WITHOUT JSON response format.
// Use for the Searcher which may need tool calling or freeform output.
func (c *OpenRouterClient) ChatFreeform(ctx context.Context, systemPrompt, userMsg string) (string, error) {
	if IsModelBlocked(c.model) {
		return "", fmt.Errorf("model %q is blocked by policy", c.model)
	}

	c.logger.Debug("openrouter chat freeform", "model", c.model, "user_len", len(userMsg))

	resp, err := c.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: c.model,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: systemPrompt},
			{Role: openai.ChatMessageRoleUser, Content: userMsg},
		},
		Temperature: 0.3,
	})
	if err != nil {
		return "", fmt.Errorf("openrouter chat freeform (%s): %w", c.displayName, err)
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("openrouter (%s) returned no choices", c.displayName)
	}

	return resp.Choices[0].Message.Content, nil
}

// Complete implements agent.LLMClient for OpenRouter models.
func (c *OpenRouterClient) Complete(ctx context.Context, messages []agent.Message, tools []agent.ToolDefinition) (*agent.CompletionResult, error) {
	if IsModelBlocked(c.model) {
		return nil, fmt.Errorf("model %q is blocked by policy", c.model)
	}

	var sdkMessages []openai.ChatCompletionMessage
	for _, msg := range messages {
		role := openai.ChatMessageRoleUser
		switch msg.Role {
		case agent.RoleSystem:
			role = openai.ChatMessageRoleSystem
		case agent.RoleAssistant:
			role = openai.ChatMessageRoleAssistant
		case agent.RoleUser:
			role = openai.ChatMessageRoleUser
		}
		sdkMessages = append(sdkMessages, openai.ChatCompletionMessage{
			Role:    role,
			Content: msg.Content,
		})
	}

	resp, err := c.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model:    c.model,
		Messages: sdkMessages,
		ResponseFormat: &openai.ChatCompletionResponseFormat{
			Type: openai.ChatCompletionResponseFormatTypeJSONObject,
		},
		Temperature: 0.2,
		MaxTokens:   8192,
	})
	if err != nil {
		return nil, fmt.Errorf("openrouter complete (%s): %w", c.displayName, err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("openrouter (%s) returned no choices", c.displayName)
	}

	return &agent.CompletionResult{
		Content:      resp.Choices[0].Message.Content,
		InputTokens:  resp.Usage.PromptTokens,
		OutputTokens: resp.Usage.CompletionTokens,
	}, nil
}

// CompleteStream streams tokens — falls back to Complete for OpenRouter.
func (c *OpenRouterClient) CompleteStream(ctx context.Context, messages []agent.Message, tools []agent.ToolDefinition, out chan<- string) error {
	defer close(out)
	result, err := c.Complete(ctx, messages, tools)
	if err != nil {
		return err
	}
	out <- result.Content
	return nil
}

// TokenCost returns the estimated cost in USD for the given token counts.
func (c *OpenRouterClient) TokenCost(inputTokens, outputTokens int) float64 {
	rates, ok := modelCostPer1M[c.model]
	if !ok {
		return 0
	}
	return (float64(inputTokens) * rates.input / 1_000_000) +
		(float64(outputTokens) * rates.output / 1_000_000)
}

// ─── MODEL DISPLAY NAMES ───────────────────────────────────────────────────

// ModelDisplayNames maps model IDs to human-readable names for the UI.
var ModelDisplayNames = map[string]string{
	"mistralai/mistral-large":              "Mistral Large 2 · Mistral AI",
	"meta-llama/llama-4-scout":             "Llama 4 Scout · Meta",
	"meta-llama/llama-3.1-405b-instruct":   "Llama 3.1 405B · Meta",
	"meta-llama/llama-4-maverick":           "Llama 4 Maverick · Meta",
	"meta-llama/llama-3.3-70b-instruct":    "Llama 3.3 70B · Meta",
	"mistralai/mixtral-8x22b-instruct":     "Mixtral 8x22B · Mistral AI",
	"mistralai/mistral-small-3.1":          "Mistral Small 3.1 · Mistral AI",
	"gpt-4o":                               "GPT-4o · OpenAI",
	"claude-sonnet-4-6":                    "Claude Sonnet 4.6 · Anthropic",
}

// ─── BLOCKED MODELS ────────────────────────────────────────────────────────

// BlockedModelPrefixes — hard-coded exclusion list. No Chinese models.
var BlockedModelPrefixes = []string{
	"deepseek", "qwen", "yi-", "ernie", "baidu",
	"minimax", "moonshot", "zhipu", "internlm", "01-ai",
}

// IsModelBlocked returns true if the model ID matches any blocked prefix.
func IsModelBlocked(modelID string) bool {
	lower := strings.ToLower(modelID)
	for _, prefix := range BlockedModelPrefixes {
		if strings.Contains(lower, prefix) {
			return true
		}
	}
	return false
}

// ─── COST RATES ────────────────────────────────────────────────────────────

type costRate struct {
	input  float64 // cost per 1M input tokens
	output float64 // cost per 1M output tokens
}

var modelCostPer1M = map[string]costRate{
	"mistralai/mistral-large":            {input: 2.0, output: 6.0},
	"meta-llama/llama-4-scout":           {input: 0.11, output: 0.34},
	"meta-llama/llama-3.1-405b-instruct": {input: 1.79, output: 1.79},
	"meta-llama/llama-4-maverick":        {input: 0.40, output: 0.40},
	"meta-llama/llama-3.3-70b-instruct":  {input: 0.07, output: 0.07},
	"mistralai/mixtral-8x22b-instruct":   {input: 0.65, output: 0.65},
	"mistralai/mistral-small-3.1":        {input: 0.10, output: 0.30},
	// Direct API models (not via OpenRouter, but track costs).
	"gpt-4o":           {input: 5.0, output: 15.0},
	"claude-sonnet-4-6": {input: 3.0, output: 15.0},
}

// EstimateCost returns cost in USD for the given model and token counts.
func EstimateCost(modelID string, inputTokens, outputTokens int) float64 {
	rates, ok := modelCostPer1M[modelID]
	if !ok {
		return 0
	}
	return (float64(inputTokens) * rates.input / 1_000_000) +
		(float64(outputTokens) * rates.output / 1_000_000)
}

// Ensure OpenRouterClient implements agent.LLMClient.
var _ agent.LLMClient = (*OpenRouterClient)(nil)
