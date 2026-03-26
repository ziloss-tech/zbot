// Package llm implements the Anthropic Claude LLM client.
// This is the primary AI model adapter for ZBOT.
package llm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/ziloss-tech/zbot/internal/agent"
)

// ─── MODEL CONSTANTS ────────────────────────────────────────────────────────

const (
	ModelSonnet = "claude-sonnet-4-6"
	ModelOpus   = "claude-opus-4-6"
	ModelHaiku  = "claude-haiku-4-5-20251001"

	DefaultMaxTokens = 8192

	// ThinkingMaxTokens is the total max_tokens when thinking is enabled.
	// Must be large enough for both thinking budget + response.
	ThinkingMaxTokens = 16000

	// ThinkingBudgetTokens is the token budget for extended thinking.
	// The model will use up to this many tokens for internal reasoning.
	ThinkingBudgetTokens = 10000

	// MaxImagesPerRequest is Claude's limit for image content blocks.
	MaxImagesPerRequest = 20
)

// ─── CLIENT ─────────────────────────────────────────────────────────────────

// Client implements agent.LLMClient using the Anthropic Claude API.
type Client struct {
	sdk    anthropic.Client
	model  string
	logger *slog.Logger
}

// New creates an Anthropic LLM client with the given API key.
func New(apiKey string, logger *slog.Logger) *Client {
	client := anthropic.NewClient(
		option.WithAPIKey(apiKey),
	)
	return &Client{
		sdk:    client,
		model:  ModelSonnet,
		logger: logger,
	}
}

// NewHaikuClient creates a cheap Anthropic client locked to Haiku.
// Used for lightweight tasks like insight extraction and fact classification.
func NewHaikuClient(apiKey string, logger *slog.Logger) *Client {
	client := anthropic.NewClient(
		option.WithAPIKey(apiKey),
	)
	return &Client{
		sdk:    client,
		model:  ModelHaiku,
		logger: logger,
	}
}

func (c *Client) ModelName() string { return c.model }

// Complete sends messages to Claude and returns the response.
// Handles system prompt extraction, tool definitions, multimodal images, and tool_use parsing.
func (c *Client) Complete(ctx context.Context, messages []agent.Message, tools []agent.ToolDefinition) (*agent.CompletionResult, error) {

	// 1. Separate system prompt from conversation messages.
	var systemBlocks []anthropic.TextBlockParam
	var sdkMessages []anthropic.MessageParam

	for _, msg := range messages {
		switch msg.Role {
		case agent.RoleSystem:
			systemBlocks = append(systemBlocks, anthropic.TextBlockParam{
				Text: msg.Content,
			})

		case agent.RoleUser:
			sdkMessages = append(sdkMessages, buildUserParam(msg))

		case agent.RoleAssistant:
			sdkMessages = append(sdkMessages, buildAssistantParam(msg))

		case agent.RoleTool:
			// Tool results must be grouped into user messages.
			sdkMessages = append(sdkMessages, anthropic.NewUserMessage(
				anthropic.NewToolResultBlock(msg.ToolCallID, msg.Content, msg.IsError),
			))
		}
	}

	// Merge consecutive tool-result user messages into single user messages.
	sdkMessages = mergeConsecutiveToolResults(sdkMessages)

	// 2. Convert tool definitions.
	sdkTools := make([]anthropic.ToolUnionParam, 0, len(tools))
	for _, t := range tools {
		sdkTools = append(sdkTools, anthropic.ToolUnionParam{
			OfTool: &anthropic.ToolParam{
				Name:        t.Name,
				Description: anthropic.String(t.Description),
				InputSchema: convertInputSchema(t.InputSchema),
			},
		})
	}

	// 3. Pick model based on context hint, message content, or default.
	model := c.pickModel(ctx, messages)

	// 4. Build request params.
	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(model),
		MaxTokens: int64(DefaultMaxTokens),
		Messages:  sdkMessages,
	}
	if len(systemBlocks) > 0 {
		params.System = systemBlocks
	}
	if len(sdkTools) > 0 {
		params.Tools = sdkTools
	}

	// 4b. Enable extended thinking for Sonnet/Opus (not Haiku).
	// Extended thinking lets the model reason internally before responding,
	// improving accuracy on complex tasks. Context hint "no_thinking" disables it.
	thinkingHint := agent.ThinkingHintFromCtx(ctx)
	if thinkingHint != "disabled" && model != ModelHaiku && !strings.Contains(model, "haiku") {
		params.Thinking = anthropic.ThinkingConfigParamOfEnabled(ThinkingBudgetTokens)
		params.MaxTokens = int64(ThinkingMaxTokens)
		c.logger.Debug("extended thinking enabled", "model", model, "budget", ThinkingBudgetTokens)
	}

	// 5. Call the API.
	c.logger.Debug("anthropic.Complete calling API",
		"model", model,
		"messages", len(sdkMessages),
		"tools", len(sdkTools),
	)

	resp, err := c.sdk.Messages.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("anthropic.Complete: %w", err)
	}

	// 6. Parse response into domain types.
	result := &agent.CompletionResult{
		StopReason:   string(resp.StopReason),
		Model:        model,
		InputTokens:  int(resp.Usage.InputTokens),
		OutputTokens: int(resp.Usage.OutputTokens),
	}

	for _, block := range resp.Content {
		switch b := block.AsAny().(type) {
		case anthropic.TextBlock:
			result.Content += b.Text

		case anthropic.ThinkingBlock:
			result.Thinking = b.Thinking
			result.ThinkingSignature = b.Signature

		case anthropic.ToolUseBlock:
			// Parse the input JSON into map[string]any.
			inputMap := make(map[string]any)
			if raw := b.JSON.Input.Raw(); raw != "" {
				if err := json.Unmarshal([]byte(raw), &inputMap); err != nil {
					c.logger.Warn("failed to parse tool input", "tool", b.Name, "err", err)
				}
			}
			result.ToolCalls = append(result.ToolCalls, agent.ToolCall{
				ID:    b.ID,
				Name:  b.Name,
				Input: inputMap,
			})
		}
	}

	c.logger.Debug("anthropic.Complete response",
		"stop_reason", result.StopReason,
		"tool_calls", len(result.ToolCalls),
		"input_tokens", result.InputTokens,
		"output_tokens", result.OutputTokens,
	)

	return result, nil
}

// CompleteStream streams the response token by token.
// For Sprint 1, Slack doesn't need streaming — this is a stub.
func (c *Client) CompleteStream(ctx context.Context, messages []agent.Message, tools []agent.ToolDefinition, out chan<- string) error {
	defer close(out)

	// Fall back to non-streaming Complete.
	result, err := c.Complete(ctx, messages, tools)
	if err != nil {
		return err
	}
	out <- result.Content
	return nil
}

// ─── MODEL ROUTER ───────────────────────────────────────────────────────────

// pickModel selects the appropriate model based on:
//  1. Context hint (set by orchestrator for workflow routing)
//  2. Message content (/think flag, simple messages)
//  3. Default model
//
// Hint values: "cheap" → Haiku, "smart" → Sonnet, "opus" → Opus,
// or a specific model name like "claude-haiku-4-5-20251001".
func (c *Client) pickModel(ctx context.Context, messages []agent.Message) string {
	// Priority 1: Context hint from orchestrator/caller.
	hint := agent.ModelHintFromCtx(ctx)
	c.logger.Debug("pickModel", "hint", hint, "default", c.model)
	switch hint {
	case "cheap":
		return ModelHaiku
	case "smart":
		return ModelSonnet
	case "opus":
		return ModelOpus
	case "":
		// no hint — fall through to content-based routing
	default:
		// specific model name passed directly
		return hint
	}

	if len(messages) == 0 {
		return c.model
	}

	// Check the last user message for routing hints.
	var lastUserMsg string
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == agent.RoleUser {
			lastUserMsg = messages[i].Content
			break
		}
	}

	// /think flag → use Opus for deep reasoning.
	if strings.Contains(lastUserMsg, "/think") {
		return ModelOpus
	}

	// Simple messages (short, no question marks, no complex requests) → Haiku.
	if isSimpleMessage(lastUserMsg) {
		return ModelHaiku
	}

	return c.model
}

// isSimpleMessage returns true for short, simple messages that don't need
// the full Sonnet model (greetings, acknowledgments, etc.).
func isSimpleMessage(text string) bool {
	if len(text) > 100 {
		return false
	}
	lower := strings.ToLower(text)
	simplePatterns := []string{
		"hello", "hi", "hey", "thanks", "thank you",
		"ok", "okay", "got it", "cool", "nice",
		"yes", "no", "yep", "nope",
	}
	for _, p := range simplePatterns {
		if lower == p || lower == p+"!" || lower == p+"." {
			return true
		}
	}
	return false
}

// ─── HELPERS ────────────────────────────────────────────────────────────────

// mediaTypeToSDK maps MIME types to Anthropic SDK media type constants.
func mediaTypeToSDK(mediaType string) anthropic.Base64ImageSourceMediaType {
	switch mediaType {
	case "image/jpeg":
		return anthropic.Base64ImageSourceMediaTypeImageJPEG
	case "image/png":
		return anthropic.Base64ImageSourceMediaTypeImagePNG
	case "image/gif":
		return anthropic.Base64ImageSourceMediaTypeImageGIF
	case "image/webp":
		return anthropic.Base64ImageSourceMediaTypeImageWebP
	default:
		return anthropic.Base64ImageSourceMediaTypeImageJPEG
	}
}

// buildUserParam converts an agent.Message (role=user) into an Anthropic SDK
// MessageParam, including image attachments as content blocks.
func buildUserParam(msg agent.Message) anthropic.MessageParam {
	var blocks []anthropic.ContentBlockParamUnion

	// Add image content blocks BEFORE text (Claude processes them in order).
	// Enforce MaxImagesPerRequest limit.
	imageCount := len(msg.Images)
	if imageCount > MaxImagesPerRequest {
		imageCount = MaxImagesPerRequest
	}
	for i := 0; i < imageCount; i++ {
		img := msg.Images[i]
		blocks = append(blocks, anthropic.NewImageBlockBase64(
			string(mediaTypeToSDK(img.MediaType)),
			base64.StdEncoding.EncodeToString(img.Data),
		))
	}

	// Add text content block.
	if msg.Content != "" {
		blocks = append(blocks, anthropic.NewTextBlock(msg.Content))
	}

	// Fallback: if no blocks at all, add empty text.
	if len(blocks) == 0 {
		blocks = append(blocks, anthropic.NewTextBlock(""))
	}

	return anthropic.MessageParam{
		Role:    anthropic.MessageParamRoleUser,
		Content: blocks,
	}
}

// buildAssistantParam converts an agent.Message (role=assistant) into an
// Anthropic SDK MessageParam, preserving thinking, text, and tool_use blocks.
func buildAssistantParam(msg agent.Message) anthropic.MessageParam {
	var blocks []anthropic.ContentBlockParamUnion

	// Thinking blocks MUST come first (required by API for multi-turn with thinking).
	if msg.Thinking != "" && msg.ThinkingSignature != "" {
		blocks = append(blocks, anthropic.NewThinkingBlock(msg.ThinkingSignature, msg.Thinking))
	}

	// Add text content if present.
	if msg.Content != "" {
		blocks = append(blocks, anthropic.NewTextBlock(msg.Content))
	}

	// Add tool_use blocks if present.
	for _, tc := range msg.ToolCalls {
		blocks = append(blocks, anthropic.NewToolUseBlock(tc.ID, tc.Input, tc.Name))
	}

	// Fallback: if no blocks at all, add empty text to avoid API error.
	if len(blocks) == 0 {
		blocks = append(blocks, anthropic.NewTextBlock(""))
	}

	return anthropic.MessageParam{
		Role:    anthropic.MessageParamRoleAssistant,
		Content: blocks,
	}
}

// mergeConsecutiveToolResults merges consecutive user messages that contain
// only tool results into single user messages. The Anthropic API requires
// that all tool results for a single assistant turn be in one user message.
func mergeConsecutiveToolResults(messages []anthropic.MessageParam) []anthropic.MessageParam {
	if len(messages) == 0 {
		return messages
	}

	merged := make([]anthropic.MessageParam, 0, len(messages))
	for i := 0; i < len(messages); i++ {
		msg := messages[i]

		// Check if this is a user message with tool results.
		if msg.Role == anthropic.MessageParamRoleUser && isToolResultMessage(msg) {
			// Collect all consecutive tool-result user messages.
			var allBlocks []anthropic.ContentBlockParamUnion
			allBlocks = append(allBlocks, msg.Content...)
			for i+1 < len(messages) &&
				messages[i+1].Role == anthropic.MessageParamRoleUser &&
				isToolResultMessage(messages[i+1]) {
				i++
				allBlocks = append(allBlocks, messages[i].Content...)
			}
			merged = append(merged, anthropic.MessageParam{
				Role:    anthropic.MessageParamRoleUser,
				Content: allBlocks,
			})
		} else {
			merged = append(merged, msg)
		}
	}
	return merged
}

// isToolResultMessage checks if a MessageParam contains only tool result blocks.
func isToolResultMessage(msg anthropic.MessageParam) bool {
	if len(msg.Content) == 0 {
		return false
	}
	for _, block := range msg.Content {
		if block.OfToolResult == nil {
			return false
		}
	}
	return true
}

// convertInputSchema converts our map[string]any JSON schema into the SDK's ToolInputSchemaParam.
func convertInputSchema(schema map[string]any) anthropic.ToolInputSchemaParam {
	result := anthropic.ToolInputSchemaParam{}
	if props, ok := schema["properties"].(map[string]any); ok {
		result.Properties = props
	}
	return result
}

// Ensure Client implements the port.
var _ agent.LLMClient = (*Client)(nil)
