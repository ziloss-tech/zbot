package llm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	openai "github.com/sashabaranov/go-openai"
)

// OpenAIClient wraps the OpenAI API for use as the planner LLM.
// Used exclusively for structured planning — returns JSON task graphs.
type OpenAIClient struct {
	client *openai.Client
	model  string
	logger *slog.Logger
}

// NewOpenAIClient creates a new OpenAI client.
// model: "gpt-4o" or "o3" etc.
func NewOpenAIClient(apiKey, model string, logger *slog.Logger) *OpenAIClient {
	return &OpenAIClient{
		client: openai.NewClient(apiKey),
		model:  model,
		logger: logger,
	}
}

// ModelName returns the model identifier.
func (c *OpenAIClient) ModelName() string {
	return c.model
}

// Chat sends a system prompt + user message and returns the response text.
// No tool calling — planner uses pure JSON output mode.
func (c *OpenAIClient) Chat(ctx context.Context, systemPrompt, userMsg string) (string, error) {
	c.logger.Debug("openai chat", "model", c.model, "user_len", len(userMsg))

	resp, err := c.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: c.model,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: systemPrompt},
			{Role: openai.ChatMessageRoleUser, Content: userMsg},
		},
		ResponseFormat: &openai.ChatCompletionResponseFormat{
			Type: openai.ChatCompletionResponseFormatTypeJSONObject,
		},
		Temperature: 0.2, // low temp for consistent structured output
	})
	if err != nil {
		return "", fmt.Errorf("openai chat: %w", err)
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("openai returned no choices")
	}

	content := resp.Choices[0].Message.Content
	c.logger.Debug("openai response", "model", c.model, "len", len(content),
		"prompt_tokens", resp.Usage.PromptTokens,
		"completion_tokens", resp.Usage.CompletionTokens)

	return content, nil
}

// ChatStream sends a system prompt + user message and streams response tokens
// to the provided channel. Returns the full accumulated response text.
// The channel is NOT closed by this method — caller owns it.
func (c *OpenAIClient) ChatStream(ctx context.Context, systemPrompt, userMsg string, tokens chan<- string) (string, error) {
	c.logger.Debug("openai chat stream", "model", c.model, "user_len", len(userMsg))

	stream, err := c.client.CreateChatCompletionStream(ctx, openai.ChatCompletionRequest{
		Model: c.model,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: systemPrompt},
			{Role: openai.ChatMessageRoleUser, Content: userMsg},
		},
		ResponseFormat: &openai.ChatCompletionResponseFormat{
			Type: openai.ChatCompletionResponseFormatTypeJSONObject,
		},
		Temperature: 0.2,
		Stream:      true,
	})
	if err != nil {
		return "", fmt.Errorf("openai chat stream: %w", err)
	}
	defer stream.Close()

	var accumulated string
	for {
		resp, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return accumulated, fmt.Errorf("openai stream recv: %w", err)
		}
		if len(resp.Choices) > 0 {
			delta := resp.Choices[0].Delta.Content
			if delta != "" {
				accumulated += delta
				select {
				case tokens <- delta:
				case <-ctx.Done():
					return accumulated, ctx.Err()
				}
			}
		}
	}

	c.logger.Debug("openai stream complete", "model", c.model, "len", len(accumulated))
	return accumulated, nil
}
