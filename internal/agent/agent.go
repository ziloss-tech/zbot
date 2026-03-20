// Package agent implements the core ZBOT agent loop.
// It orchestrates: receive message → load context → call LLM → execute tools → respond.
// This package depends ONLY on the interfaces defined in ports.go — never on adapters.
package agent

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ziloss-tech/zbot/internal/prompts"
	"github.com/ziloss-tech/zbot/internal/security"
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
	cheapLLM     LLMClient // Frontal Lobe + Thalamus use this (Haiku/Grok Fast). Nil = skip cognitive stages.
	memory       MemoryStore
	tools        map[string]Tool
	audit        AuditLogger
	events       EventBus
	logger       *slog.Logger
	confirmStore *security.ConfirmationStore // Sprint 9: destructive op confirmation
}

// SetConfirmationStore wires the confirmation gate for destructive tool calls.
func (a *Agent) SetConfirmationStore(cs *security.ConfirmationStore) {
	a.confirmStore = cs
}

// SetCheapLLM wires the cheap model used by Frontal Lobe (planning) and Thalamus (verification).
// If nil, cognitive stages are skipped and the agent falls back to single-call mode.
func (a *Agent) SetCheapLLM(llm LLMClient) {
	a.cheapLLM = llm
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
	events EventBus,
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
		events: events,
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

	// Emit turn_start event for Thalamus / UI.
	a.emit(ctx, input.SessionID, EventTurnStart, "Turn started", map[string]any{
		"goal": input.UserMsg.Content,
	})

	// 0. Inject model hint into context if set.
	if input.ModelHint != "" {
		ctx = WithModelHint(ctx, input.ModelHint)
	}

	// ── STAGE 0: ROUTER — Classify the message ─────────────────────────────
	route := a.routeMessage(ctx, input.SessionID, input.UserMsg.Content, a.cheapLLM)

	// Apply Router's model tier hint if no explicit hint was given.
	// Only escalate UP (to Opus) — never downgrade Cortex to cheap.
	// The cheap cognitive stages (Router, Frontal Lobe, Thalamus) already use cheapLLM directly.
	if route != nil && input.ModelHint == "" && route.ModelTier == "strong" {
		ctx = WithModelHint(ctx, "opus")
	}

	// ── STAGE 1: FRONTAL LOBE — Plan the approach ──────────────────────────
	// Skip Frontal Lobe if Router says it's a direct answer (saves one cheapLLM call).
	var plan *TaskPlan
	if route != nil && route.Classification == "direct_answer" {
		// Synthesize a minimal plan from the Router decision.
		plan = &TaskPlan{
			Type:         "chat",
			Complexity:   "simple",
			Steps:        []string{"respond directly"},
			NeedsMemory:  false,
			Verification: "none",
			ModelTier:    route.ModelTier,
			Reasoning:    "Router: " + route.Reasoning,
		}
	} else {
		plan = a.planTask(ctx, input.SessionID, input.UserMsg.Content, a.cheapLLM)
	}

	// ── STAGE 2: HIPPOCAMPUS — Load relevant memories ──────────────────────
	// Skip memory search if Router says no memory needed and confidence is high.
	skipMemory := route != nil && route.Classification == "direct_answer" && route.Confidence > 0.9
	var facts []Fact
	if !skipMemory {
		var memErr error
		facts, memErr = a.memory.Search(ctx, input.UserMsg.Content, a.cfg.MemorySearchLimit)
		if memErr != nil {
			a.logger.Warn("memory search failed", "err", memErr)
			facts = nil
		}
	}
	if len(facts) > 0 {
		a.emit(ctx, input.SessionID, EventMemoryLoaded,
			fmt.Sprintf("Loaded %d memories", len(facts)),
			map[string]any{"count": len(facts)})
	}

	// ── STAGE 3: CORTEX — Build context with plan injection ────────────────
	messages := a.buildContextWithPlan(input, facts, plan)

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

	// ── STAGE 4: HIPPOCAMPUS — Mid-task memory enrichment ──────────────────
	// If the plan says we need memory and tools were invoked, check memory again
	// based on what Cortex discovered. "Wait, we already knew part of this."
	if plan != nil && plan.NeedsMemory && len(invokedTools) > 0 {
		// Collect tool results from the messages (they're the tool-role messages).
		var toolResultsForMemory []ToolResult
		for _, msg := range messages {
			if msg.Role == RoleTool && !msg.IsError {
				toolResultsForMemory = append(toolResultsForMemory, ToolResult{Content: msg.Content})
			}
		}
		enrichedFacts := a.enrichMemory(ctx, input.SessionID, toolResultsForMemory)
		if len(enrichedFacts) > 0 {
			// Inject enriched memory as a system note before the final response.
			memNote := "\n\nIMPORTANT - Additional context from memory (discovered mid-task):\n"
			for _, f := range enrichedFacts {
				memNote += fmt.Sprintf("- %s\n", f.Content)
			}
			memNote += "\nConsider this information in your final response."
			messages = append(messages, Message{Role: RoleSystem, Content: memNote})
			// Run one more Cortex call to incorporate the new memory.
			revisedResult, revErr := a.llm.Complete(ctx, messages, toolDefs)
			if revErr == nil && revisedResult.Content != "" {
				output.Reply = revisedResult.Content
				output.InputTokens += revisedResult.InputTokens
				output.OutputTokens += revisedResult.OutputTokens
				output.TokensUsed += revisedResult.InputTokens + revisedResult.OutputTokens
			}
		}
	}

	// ── STALL RECOVERY — Frontal Lobe override ──────────────────────────────
	// If Cortex asked for permission instead of executing, attempt recovery.
	if plan != nil && len(invokedTools) == 0 && output.Reply != "" {
		if isStalled(output.Reply, plan) {
			recovered := a.recoverFromStall(ctx, input, plan, output.Reply, toolDefs)
			if recovered != nil {
				output = recovered
			}
		}
	}

	// ── STAGE 5: THALAMUS — Verify the reply before sending ────────────────
	if output.Reply != "" && plan != nil && plan.Verification != "none" {
		a.logger.Info("thalamus verification starting",
			"reply_len", len(output.Reply),
			"verification", plan.Verification)
		// Collect evidence from tool results for verification context.
		var evidence string
		for _, msg := range messages {
			if msg.Role == RoleTool && !msg.IsError {
				if len(msg.Content) > 500 {
					evidence += msg.Content[:500] + "\n[truncated]\n---\n"
				} else {
					evidence += msg.Content + "\n---\n"
				}
			}
		}

		approved, suggestion := a.verifyReply(ctx, input.SessionID, plan, input.UserMsg.Content, output.Reply, evidence, a.cheapLLM)
		if !approved && suggestion != "" {
			// One revision attempt: send the suggestion back to Cortex.
			revisionMsg := fmt.Sprintf("THALAMUS REVIEW - Your draft reply needs revision.\nIssue: %s\nPlease revise your response addressing this feedback.", suggestion)
			messages = append(messages, Message{Role: RoleSystem, Content: revisionMsg})
			
			revisedResult, revErr := a.llm.Complete(ctx, messages, nil) // no tools for revision
			if revErr == nil && revisedResult.Content != "" {
				output.Reply = revisedResult.Content
				output.InputTokens += revisedResult.InputTokens
				output.OutputTokens += revisedResult.OutputTokens
				output.TokensUsed += revisedResult.InputTokens + revisedResult.OutputTokens
			}
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

	// Emit turn_complete event for Thalamus / UI.
	a.emit(ctx, input.SessionID, EventTurnComplete, "Turn complete", map[string]any{
		"input_tokens":  output.InputTokens,
		"output_tokens": output.OutputTokens,
		"cost_usd":      costUSD,
		"tools_used":    output.ToolsInvoked,
	})

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

// buildContextWithPlan assembles the context with the Frontal Lobe's plan injected.
// buildContextWithPlan assembles the context with the Frontal Lobe's plan injected.
func (a *Agent) buildContextWithPlan(input TurnInput, facts []Fact, plan *TaskPlan) []Message {
	messages := make([]Message, 0, len(input.History)+4)

	// System prompt with memory — now uses Pantheon's modular builder.
	sysPrompt := a.buildSystemPromptWithPlan(facts, plan)

	// Inject plan if available.
	if plan != nil {
		sysPrompt += "\n\n## Execution Plan (from Frontal Lobe)\n"
		sysPrompt += fmt.Sprintf("Task type: %s | Complexity: %s | Verification: %s\n", plan.Type, plan.Complexity, plan.Verification)
		if len(plan.Steps) > 0 {
			sysPrompt += "Steps:\n"
			for i, step := range plan.Steps {
				sysPrompt += fmt.Sprintf("  %d. %s\n", i+1, step)
			}
		}
		sysPrompt += "\nFollow this plan. Execute each step in order. If the plan is wrong, adjust — but explain why."
	}

	messages = append(messages, Message{
		Role:    RoleSystem,
		Content: sysPrompt,
	})

	messages = append(messages, input.History...)
	messages = append(messages, input.UserMsg)

	return messages
}

// buildSystemPrompt constructs the system prompt using the modular builder.
// Pantheon architecture: modules are conditionally loaded based on the Frontal Lobe's plan.
func (a *Agent) buildSystemPrompt(facts []Fact) string {
	return a.buildSystemPromptWithPlan(facts, nil)
}

// buildSystemPromptWithPlan constructs the system prompt with plan-aware module selection.
// This is the Pantheon's prompt assembly — only load what this turn needs.
func (a *Agent) buildSystemPromptWithPlan(facts []Fact, plan *TaskPlan) string {
	// Build profile from plan (or use defaults if no plan).
	profile := prompts.DefaultProfile()
	if plan != nil {
		// Map Frontal Lobe plan to prompt profile.
		profile.IncludeReasoning = plan.Complexity != "simple"
		profile.IncludeMemoryPolicy = plan.NeedsMemory || plan.Type == "research"
		profile.IncludeToolControl = plan.Type == "code" || plan.Type == "analysis" || plan.Type == "research"
		profile.IncludeVerification = plan.Verification == "basic" || plan.Verification == "thorough"

		// Map plan type to execution mode.
		switch plan.Type {
		case "chat":
			profile.ExecutionMode = "chat"
			profile.SocraticDepth = "skip"
		case "code", "analysis":
			profile.ExecutionMode = "safe_autopilot"
			profile.SocraticDepth = "minimal"
		case "research":
			profile.ExecutionMode = "safe_autopilot"
			profile.SocraticDepth = "deep"
		default:
			profile.ExecutionMode = "chat"
			profile.SocraticDepth = "skip"
		}
	}

	// Assemble the prompt using the modular builder.
	base := prompts.BuildExecutorPrompt(profile, "")

	// Inject temporal awareness.
	now := time.Now()
	temporal := fmt.Sprintf("\n\n## Current Time\nCurrent date and time: %s\nTimezone: %s\nDay of week: %s",
		now.Format("2006-01-02 15:04:05"),
		now.Format("MST"),
		now.Format("Monday"))
	base += temporal

	// Inject relevant memories.
	if len(facts) > 0 {
		memSection := "\n\n## Relevant Memory\n"
		for _, f := range facts {
			memSection += fmt.Sprintf("- %s (saved: %s)\n", f.Content, f.CreatedAt.Format("2006-01-02"))
		}
		base += memSection
	}

	return base
}
// emit is a helper that publishes an event to the bus (nil-safe).
func (a *Agent) emit(ctx context.Context, sessionID string, evtType EventType, summary string, detail map[string]any) {
	if a.events == nil {
		return
	}
	a.events.Emit(ctx, AgentEvent{
		SessionID: sessionID,
		Type:      evtType,
		Summary:   summary,
		Detail:    detail,
		Timestamp: time.Now(),
	})
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

		// Emit tool_called event.
		a.emit(ctx, sessionID, EventToolCalled, call.Name, map[string]any{
			"tool": call.Name,
		})

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

		// Emit tool result/error event.
		if result.IsError {
			a.emit(ctx, sessionID, EventToolError, fmt.Sprintf("%s failed", call.Name), map[string]any{
				"tool": call.Name, "duration_ms": durationMs,
			})
		} else {
			a.emit(ctx, sessionID, EventToolResult, fmt.Sprintf("%s done", call.Name), map[string]any{
				"tool": call.Name, "duration_ms": durationMs,
			})
		}

		// Sprint 3: Emit file_read/file_write events for code mode UI.
		if !result.IsError {
			if call.Name == "read_file" {
				if path, ok := call.Input["path"].(string); ok {
					a.emit(ctx, sessionID, EventFileRead, path, map[string]any{"path": path})
				}
			} else if call.Name == "write_file" {
				if path, ok := call.Input["path"].(string); ok {
					a.emit(ctx, sessionID, EventFileWrite, path, map[string]any{
						"path": path,
						"size": len(result.Content),
					})
				}
			}
		}

		results = append(results, *result)
	}

	return results, files, nil
}
