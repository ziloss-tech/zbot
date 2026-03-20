// Package llm — OpenAI-compatible LLM client.
// Works with any API that speaks the OpenAI chat completions format:
// Ollama, Together, Groq, vLLM, LM Studio, OpenRouter, OpenAI, etc.
package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"

	openai "github.com/sashabaranov/go-openai"

	"github.com/ziloss-tech/zbot/internal/agent"
)

// OpenAICompatClient implements agent.LLMClient for any OpenAI-compatible API.
// Set BaseURL to point at Ollama (http://localhost:11434/v1), Together,
// Groq, vLLM, LM Studio, OpenRouter, or plain OpenAI.
type OpenAICompatClient struct {
	client *openai.Client
	model  string
	logger *slog.Logger
}

// NewOpenAICompatClient creates a client for any OpenAI-compatible endpoint.
//
//	baseURL: API base URL (e.g. "http://localhost:11434/v1" for Ollama,
//	         "https://api.together.xyz/v1" for Together, "" for OpenAI default)
//	apiKey:  API key (use "ollama" or any string for local models)
//	model:   model name (e.g. "llama3.1:8b", "meta-llama/Llama-3-70b-chat-hf")
func NewOpenAICompatClient(baseURL, apiKey, model string, logger *slog.Logger) *OpenAICompatClient {
	cfg := openai.DefaultConfig(apiKey)
	if baseURL != "" {
		cfg.BaseURL = baseURL
	}
	return &OpenAICompatClient{
		client: openai.NewClientWithConfig(cfg),
		model:  model,
		logger: logger,
	}
}

func (c *OpenAICompatClient) ModelName() string { return c.model }

// Complete sends messages + optional tools to the model and returns the response.
// Supports function/tool calling for models that support it (GPT-4o, Llama 3.1+,
// Mistral, Qwen, etc.). Falls back gracefully if the model doesn't support tools.
func (c *OpenAICompatClient) Complete(ctx context.Context, messages []agent.Message, tools []agent.ToolDefinition) (*agent.CompletionResult, error) {

	// 1. Convert domain messages → OpenAI format.
	oaiMessages := make([]openai.ChatCompletionMessage, 0, len(messages))
	for _, msg := range messages {
		switch msg.Role {
		case agent.RoleSystem:
			oaiMessages = append(oaiMessages, openai.ChatCompletionMessage{
				Role:    openai.ChatMessageRoleSystem,
				Content: msg.Content,
			})
		case agent.RoleUser:
			oaiMessages = append(oaiMessages, openai.ChatCompletionMessage{
				Role:    openai.ChatMessageRoleUser,
				Content: msg.Content,
			})
		case agent.RoleAssistant:
			m := openai.ChatCompletionMessage{
				Role:    openai.ChatMessageRoleAssistant,
				Content: msg.Content,
			}
			// Re-attach tool calls if present (for multi-turn tool use).
			for _, tc := range msg.ToolCalls {
				raw, _ := json.Marshal(tc.Input)
				m.ToolCalls = append(m.ToolCalls, openai.ToolCall{
					ID:   tc.ID,
					Type: openai.ToolTypeFunction,
					Function: openai.FunctionCall{
						Name:      tc.Name,
						Arguments: string(raw),
					},
				})
			}
			oaiMessages = append(oaiMessages, m)
		case agent.RoleTool:
			oaiMessages = append(oaiMessages, openai.ChatCompletionMessage{
				Role:       openai.ChatMessageRoleTool,
				Content:    msg.Content,
				ToolCallID: msg.ToolCallID,
			})
		}
	}

	// 2. Convert tool definitions → OpenAI format.
	var oaiTools []openai.Tool
	for _, t := range tools {
		paramBytes, _ := json.Marshal(t.InputSchema)
		oaiTools = append(oaiTools, openai.Tool{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  json.RawMessage(paramBytes),
			},
		})
	}

	// 3. Build request.
	req := openai.ChatCompletionRequest{
		Model:    c.model,
		Messages: oaiMessages,
	}
	if len(oaiTools) > 0 {
		req.Tools = oaiTools
	}

	c.logger.Debug("openaicompat.Complete",
		"model", c.model,
		"messages", len(oaiMessages),
		"tools", len(oaiTools),
	)

	// 4. Call API.
	resp, err := c.client.CreateChatCompletion(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("openaicompat.Complete: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("openaicompat: model returned no choices")
	}

	choice := resp.Choices[0]

	// 5. Parse response.
	result := &agent.CompletionResult{
		Content:      choice.Message.Content,
		StopReason:   string(choice.FinishReason),
		Model:        c.model,
		InputTokens:  resp.Usage.PromptTokens,
		OutputTokens: resp.Usage.CompletionTokens,
	}

	// Parse tool calls.
	for _, tc := range choice.Message.ToolCalls {
		inputMap := make(map[string]any)
		if tc.Function.Arguments != "" {
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &inputMap); err != nil {
				c.logger.Warn("failed to parse tool call args", "tool", tc.Function.Name, "err", err)
			}
		}
		result.ToolCalls = append(result.ToolCalls, agent.ToolCall{
			ID:    tc.ID,
			Name:  tc.Function.Name,
			Input: inputMap,
		})
	}

	c.logger.Debug("openaicompat.Complete response",
		"stop_reason", result.StopReason,
		"tool_calls", len(result.ToolCalls),
		"input_tokens", result.InputTokens,
		"output_tokens", result.OutputTokens,
	)

	return result, nil
}

// CompleteStream streams the response token by token.
func (c *OpenAICompatClient) CompleteStream(ctx context.Context, messages []agent.Message, tools []agent.ToolDefinition, out chan<- string) error {
	defer close(out)

	// Convert messages (same as Complete).
	oaiMessages := make([]openai.ChatCompletionMessage, 0, len(messages))
	for _, msg := range messages {
		switch msg.Role {
		case agent.RoleSystem:
			oaiMessages = append(oaiMessages, openai.ChatCompletionMessage{
				Role: openai.ChatMessageRoleSystem, Content: msg.Content,
			})
		case agent.RoleUser:
			oaiMessages = append(oaiMessages, openai.ChatCompletionMessage{
				Role: openai.ChatMessageRoleUser, Content: msg.Content,
			})
		case agent.RoleAssistant:
			oaiMessages = append(oaiMessages, openai.ChatCompletionMessage{
				Role: openai.ChatMessageRoleAssistant, Content: msg.Content,
			})
		case agent.RoleTool:
			oaiMessages = append(oaiMessages, openai.ChatCompletionMessage{
				Role: openai.ChatMessageRoleTool, Content: msg.Content, ToolCallID: msg.ToolCallID,
			})
		}
	}

	req := openai.ChatCompletionRequest{
		Model:    c.model,
		Messages: oaiMessages,
		Stream:   true,
	}

	stream, err := c.client.CreateChatCompletionStream(ctx, req)
	if err != nil {
		return fmt.Errorf("openaicompat.CompleteStream: %w", err)
	}
	defer stream.Close()

	for {
		resp, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("openaicompat stream: %w", err)
		}
		if len(resp.Choices) > 0 {
			delta := resp.Choices[0].Delta.Content
			if delta != "" {
				select {
				case out <- delta:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}
	}
}

// Verify interface compliance at compile time.
var _ agent.LLMClient = (*OpenAICompatClient)(nil)
