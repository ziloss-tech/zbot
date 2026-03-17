// Package agent implements the core ZBOT agent loop.
// It orchestrates: receive message → load context → call LLM → execute tools → respond.
// This package depends ONLY on the interfaces defined in ports.go — never on adapters.
package agent

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/zbot-ai/zbot/internal/security"
)

// ─── Model Hint Context ─────────────────────────────────────────────────────

type ctxKeyModelHint struct{}

// WithModelHint attaches a model routing hint to the context.
// Used by the LLM client's pickModel to override default selection.
func WithModelHint(ctx context.Context, hint string) context.Context {
	return context.WithValue(ctx, ctxKeyModelHint{}, hint)
}

// ModelHintFromCtx reads the model hint from context (empty = use default).
func ModelHintFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyModelHint{}).(string)
	return v
}

// Config holds the agent's runtime configuration.
// All sensitive values (API keys) come via SecretsManager — never here.
type Config struct {
	// SystemPrompt is the base instruction set for the agent.
	SystemPrompt string

	// MaxToolRounds is the maximum number of tool call / result cycles per turn.
	// Prevents infinite loops. Default: 10.
	MaxToolRounds int

	// MemorySearchLimit is how many facts to inject per turn.
	MemorySearchLimit int

	// TokenWarningThreshold triggers a memory flush at this fraction of context used.
	// Default: 0.80 (flush at 80% context usage). v2: raised from 0.75.
	TokenWarningThreshold float64

	// Router controls model tier escalation behavior.
	Router RouterConfig
}

// DefaultConfig returns safe production defaults.
func DefaultConfig() Config {
	return Config{
		MaxToolRounds:         10,
		MemorySearchLimit:     8,
		TokenWarningThreshold: 0.80,
		Router:                DefaultRouterConfig(),
	}
}

// Agent is the core agent. It is stateless between turns —
// all state lives in the stores passed at construction.
type Agent struct {
	cfg          Config
	llm          LLMClient
	memory       MemoryStore
	tools        map[string]Tool
	audit        AuditLogger
	logger       *slog.Logger
	confirmStore *security.ConfirmationStore // Sprint 9: destructive op confirmation
}

// SetConfirmationStore wires the confirmation gate for destructive tool calls.
func (a *Agent) SetConfirmationStore(cs *security.ConfirmationStore) {
	a.confirmStore = cs
}

// GetTool returns a tool by name, for direct execution (e.g. after confirmation).
func (a *Agent) GetTool(name string) (Tool, bool) {
	t, ok := a.tools[name]
	return t, ok
}

// New constructs an Agent. All dependencies are injected as interfaces.
func New(
	cfg Config,
	llm LLMClient,
	memory MemoryStore,
	audit AuditLogger,
	logger *slog.Logger,
	tools ...Tool,
) *Agent {
	toolMap := make(map[string]Tool, len(tools))
	for _, t := range tools {
		toolMap[t.Name()] = t
	}
	return &Agent{
		cfg:    cfg,
		llm:    llm,
		memory: memory,
		tools:  toolMap,
		audit:  audit,
		logger: logger,
	}
}

// TurnInput is everything the agent needs to process one user turn.
type TurnInput struct {
	SessionID  string
	WorkflowID string // set when this turn is part of a workflow task
	TaskID     string // set when this turn is part of a workflow task
	ModelHint  string // optional model override: "cheap", "smart", "opus", or specific model name
	History    []Message // conversation history (caller manages this)
	UserMsg    Message   // the new incoming message
}

// TurnOutput is the agent's response for one turn.
type TurnOutput struct {
	Reply        string
	Files        []OutputFile // any files to send back
	InputTokens  int
	OutputTokens int
	TokensUsed   int          // InputTokens + OutputTokens (kept for compatibility)
	CostUSD      float64
	ToolsInvoked []string
}

// OutputFile is a file the agent wants to send to the user.
type OutputFile struct {
	Name string
	Data []byte
}

// Run executes one full agent turn: load memory → build context → LLM loop → respond.
// It is safe to call concurrently from multiple goroutines (e.g. workflow workers).
func (a *Agent) Run(ctx context.Context, input TurnInput) (*TurnOutput, error) {
	start := time.Now()

	logArgs := []any{
		"component", "executor",
		"session", input.SessionID,
		"model", a.llm.ModelName(),
	}
	if input.WorkflowID != "" {
		logArgs = append(logArgs, "workflow_id", input.WorkflowID, "task_id", input.TaskID)
	}
	a.logger.Info("agent turn start", logArgs...)

	// 0. Inject model hint into context if set.
	if input.ModelHint != "" {
		ctx = WithModelHint(ctx, input.ModelHint)
	}

	// 1. Load relevant memories for this message.
	facts, err := a.memory.Search(ctx, input.UserMsg.Content, a.cfg.MemorySearchLimit)
	if err != nil {
		// Memory failure is non-fatal — log and continue without it.
		a.logger.Warn("memory search failed", "err", err)
		facts = nil
	}

	// 2. Build the context window for this turn.
	messages := a.buildContext(input, facts)

	// 3. Collect tool definitions to expose to the model.
	toolDefs := make([]ToolDefinition, 0, len(a.tools))
	for _, t := range a.tools {
		toolDefs = append(toolDefs, t.Definition())
	}

	// 4. LLM agentic loop — runs until the model stops requesting tools
	//    or MaxToolRounds is hit.
	output := &TurnOutput{}
	invokedTools := map[string]struct{}{}

	for round := 0; round < a.cfg.MaxToolRounds; round++ {
		callStart := time.Now()
		result, err := a.llm.Complete(ctx, messages, toolDefs)
		if err != nil {
			return nil, fmt.Errorf("agent.Run llm.Complete round=%d: %w", round, err)
		}

		modelUsed := result.Model
		if modelUsed == "" {
			modelUsed = a.llm.ModelName()
		}
		a.audit.LogModelCall(ctx, input.SessionID, modelUsed,
			result.InputTokens, result.OutputTokens,
			time.Since(callStart).Milliseconds())

		output.InputTokens += result.InputTokens
		output.OutputTokens += result.OutputTokens
		output.TokensUsed += result.InputTokens + result.OutputTokens

		// Append assistant message to running context (including any tool calls).
		assistantMsg := Message{
			Role:      RoleAssistant,
			Content:   result.Content,
			ToolCalls: result.ToolCalls,
		}
		messages = append(messages, assistantMsg)

		// If no tool calls, the model is done.
		if len(result.ToolCalls) == 0 {
			output.Reply = result.Content
			break
		}

		// 5. Execute each requested tool and append results.
		toolResults, files, err := a.executeTools(ctx, input.SessionID, result.ToolCalls, invokedTools)
		if err != nil {
			return nil, fmt.Errorf("agent.Run executeTools round=%d: %w", round, err)
		}
		output.Files = append(output.Files, files...)

		// Append tool results as tool-role messages with their tool call IDs.
		for _, tr := range toolResults {
			messages = append(messages, Message{
				Role:       RoleTool,
				Content:    tr.Content,
				ToolCallID: tr.ToolCallID,
				IsError:    tr.IsError,
			})
		}
	}

	// 6. Track which tools were used.
	for name := range invokedTools {
		output.ToolsInvoked = append(output.ToolsInvoked, name)
	}

	// v2: Calculate cost based on actual model tier used (not always Sonnet).
	modelTier := ResolveModelTier(input.ModelHint, input.UserMsg.Content)
	inputCost, outputCost := ModelTierCost(modelTier)
	costUSD := float64(output.InputTokens)*inputCost + float64(output.OutputTokens)*outputCost
	output.CostUSD = costUSD

	completeArgs := []any{
		"component", "executor",
		"session", input.SessionID,
		"model", a.llm.ModelName(),
		"input_tokens", output.InputTokens,
		"output_tokens", output.OutputTokens,
		"cost_usd", fmt.Sprintf("%.4f", costUSD),
		"tools", output.ToolsInvoked,
		"duration_ms", time.Since(start).Milliseconds(),
	}
	if input.WorkflowID != "" {
		completeArgs = append(completeArgs, "workflow_id", input.WorkflowID, "task_id", input.TaskID)
	}
	a.logger.Info("agent turn complete", completeArgs...)

	return output, nil
}

// buildContext assembles the message slice for the LLM call.
// System prompt + memory injection + conversation history + new user message.
func (a *Agent) buildContext(input TurnInput, facts []Fact) []Message {
	messages := make([]Message, 0, len(input.History)+3)

	// System prompt.
	messages = append(messages, Message{
		Role:    RoleSystem,
		Content: a.buildSystemPrompt(facts),
	})

	// Conversation history (caller is responsible for trimming if needed).
	messages = append(messages, input.History...)

	// New user message.
	messages = append(messages, input.UserMsg)

	return messages
}

// buildSystemPrompt constructs the system prompt, injecting relevant memories.
func (a *Agent) buildSystemPrompt(facts []Fact) string {
	base := a.cfg.SystemPrompt
	if len(facts) == 0 {
		return base
	}

	memSection := "\n\n## Relevant Memory\n"
	for _, f := range facts {
		memSection += fmt.Sprintf("- %s\n", f.Content)
	}
	return base + memSection
}

// executeTools runs each tool call, returning results and any output files.
func (a *Agent) executeTools(
	ctx context.Context,
	sessionID string,
	calls []ToolCall,
	invoked map[string]struct{},
) ([]ToolResult, []OutputFile, error) {
	results := make([]ToolResult, 0, len(calls))
	var files []OutputFile

	for _, call := range calls {
		tool, ok := a.tools[call.Name]
		if !ok {
			results = append(results, ToolResult{
				ToolCallID: call.ID,
				Content:    fmt.Sprintf("error: unknown tool %q", call.Name),
				IsError:    true,
			})
			continue
		}

		invoked[call.Name] = struct{}{}

		// Sprint 9: Validate tool inputs before execution.
		if valErr := security.ValidateToolInput(call.Name, call.Input); valErr != nil {
			a.logger.Warn("tool input validation failed",
				"tool", call.Name,
				"session", sessionID,
				"err", valErr,
			)
			results = append(results, ToolResult{
				ToolCallID: call.ID,
				Content:    fmt.Sprintf("validation error: %v", valErr),
				IsError:    true,
			})
			continue
		}

		// Sprint 9: Destructive operation confirmation gate.
		if a.confirmStore != nil && security.IsDestructive(call.Name) {
			preview := security.FormatPreview(call.Name, call.Input)
			a.confirmStore.SetPending(sessionID, &security.PendingAction{
				ToolCallID: call.ID,
				ToolName:   call.Name,
				Input:      call.Input,
				Preview:    preview,
			})
			a.logger.Info("destructive tool requires confirmation",
				"tool", call.Name,
				"session", sessionID,
			)
			results = append(results, ToolResult{
				ToolCallID: call.ID,
				Content:    fmt.Sprintf("⚠️ This action requires your confirmation before I execute it:\n\n%s\n\nReply *yes* to confirm or *no* to cancel.", preview),
				IsError:    false,
			})
			continue
		}

		execStart := time.Now()

		result, err := tool.Execute(ctx, call.Input)
		durationMs := time.Since(execStart).Milliseconds()

		if err != nil {
			result = &ToolResult{
				ToolCallID: call.ID,
				Content:    fmt.Sprintf("error executing %s: %v", call.Name, err),
				IsError:    true,
			}
		}

		// Ensure ToolCallID is always set (tools don't know their call ID).
		result.ToolCallID = call.ID

		a.audit.LogToolCall(ctx, sessionID, call.Name, call.Input, result, durationMs)
		results = append(results, *result)
	}

	return results, files, nil
}
