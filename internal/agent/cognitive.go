// Package agent — cognitive.go implements the brain-region cognitive stages.
// Each function represents a distinct cognitive phase that runs independently
// with its own LLM call (cheap model) or database query (zero LLM cost).
//
// The key insight: these stages use SEPARATE LLM calls from Cortex.
// Frontal Lobe and Thalamus use cheapLLM (Haiku/Grok Fast at ~$0.001/call).
// Hippocampus enrichment is pure database (zero LLM cost).
// Cortex (the main reasoning) is the only expensive call.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// ─── ROUTER DECISION (Stage 0 output) ───────────────────────────────────────

// RouterDecision is the output of the Router classification stage.
// It runs BEFORE Frontal Lobe and sets the execution contract for the turn.
type RouterDecision struct {
	Classification string   `json:"classification"`  // "direct_answer", "needs_memory", "needs_tools", "needs_plan", "needs_verification"
	SocraticMode   string   `json:"socratic_mode"`   // "skip", "minimal", "deep"
	ModelTier      string   `json:"model_tier"`      // "cheap", "standard", "strong"
	ToolSubset     []string `json:"tool_subset"`     // which tools to expose to Cortex
	ExecutionMode  string   `json:"execution_mode"`  // "chat", "safe_autopilot", "autopilot"
	Confidence     float64  `json:"confidence"`      // 0.0-1.0
	Reasoning      string   `json:"reasoning"`       // one sentence explaining the classification
}

// ─── TASK PLAN (Frontal Lobe output) ────────────────────────────────────────

// TaskPlan is the output of the Frontal Lobe planning stage.
// It tells Cortex HOW to approach the task before it starts.
type TaskPlan struct {
	Type         string   `json:"type"`         // "chat", "research", "code", "analysis", "clarify"
	Complexity   string   `json:"complexity"`   // "simple", "moderate", "complex"
	Steps        []string `json:"steps"`        // ordered execution steps
	NeedsMemory  bool     `json:"needs_memory"` // should we check memory mid-task?
	Verification string   `json:"verification"` // "none", "basic", "thorough"
	ModelTier    string   `json:"model_tier"`   // "fast", "default", "advanced"
	Reasoning    string   `json:"reasoning"`    // why this plan was chosen
}

// ─── STAGE 1: FRONTAL LOBE — Planning ───────────────────────────────────────

const frontalLobePrompt = `You are the Frontal Lobe — the executive planner in a multi-stage AI cognitive system.

Your job: analyze the user's message and produce a structured execution plan BEFORE the main reasoning engine (Cortex) sees it.

You do NOT answer the user's question. You plan HOW it should be answered.

Respond with ONLY a JSON object, no other text:
{
  "type": "chat|research|code|analysis|clarify",
  "complexity": "simple|moderate|complex",
  "steps": ["step 1 description", "step 2 description", ...],
  "needs_memory": true/false,
  "verification": "none|basic|thorough",
  "model_tier": "fast|default|advanced",
  "reasoning": "one sentence explaining why you chose this plan"
}

Guidelines:
- "chat" = simple conversational reply, no tools needed. verification=none.
- "research" = needs web search, multiple sources. verification=basic or thorough.
- "code" = write or modify code. verification=basic (check if code makes sense).
- "analysis" = interpret data, compare options, make recommendations. verification=thorough.
- "clarify" = the request is ambiguous, ask for clarification. verification=none.
- model_tier "fast" for simple chat, "default" for most tasks, "advanced" for complex reasoning.
- needs_memory=true when the task might benefit from recalling past interactions or saved facts.
- For complex tasks, list 3-7 concrete steps. For simple chat, steps can be ["respond directly"].`

// planTask runs the Frontal Lobe planning stage.
// Uses cheapLLM (~$0.001) to classify the task and write a plan.
// Returns nil if cheapLLM is not configured (falls back to single-call mode).
func (a *Agent) planTask(ctx context.Context, sessionID string, userMessage string, cheap LLMClient) *TaskPlan {
	if cheap == nil {
		return nil // No cheap model → skip planning, Cortex handles everything
	}

	a.emit(ctx, sessionID, EventPlanStart, "Frontal Lobe planning", nil)

	messages := []Message{
		{Role: RoleSystem, Content: frontalLobePrompt},
		{Role: RoleUser, Content: userMessage},
	}

	result, err := cheap.Complete(ctx, messages, nil) // no tools for planning
	if err != nil {
		a.logger.Warn("frontal lobe planning failed, falling back to single-call", "err", err)
		a.emit(ctx, sessionID, EventPlanComplete, "Planning skipped (error)", nil)
		return nil
	}

	// Parse the JSON plan from the response.
	plan := &TaskPlan{}
	content := strings.TrimSpace(result.Content)
	
	// Find JSON in the response (model might wrap it in markdown)
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start >= 0 && end > start {
		content = content[start : end+1]
	}

	if err := json.Unmarshal([]byte(content), plan); err != nil {
		a.logger.Warn("frontal lobe plan parse failed", "err", err, "raw", result.Content[:200])
		a.emit(ctx, sessionID, EventPlanComplete, "Planning skipped (parse error)", nil)
		return nil
	}

	a.emit(ctx, sessionID, EventPlanComplete, fmt.Sprintf("Plan: %s (%s, %d steps)", plan.Type, plan.Complexity, len(plan.Steps)), map[string]any{
		"type":         plan.Type,
		"complexity":   plan.Complexity,
		"steps":        len(plan.Steps),
		"verification": plan.Verification,
		"model_tier":   plan.ModelTier,
		"reasoning":    plan.Reasoning,
	})

	a.logger.Info("frontal lobe plan",
		"type", plan.Type,
		"complexity", plan.Complexity,
		"steps", len(plan.Steps),
		"verification", plan.Verification,
		"reasoning", plan.Reasoning,
	)

	return plan
}

// ─── STAGE 0: ROUTER — Classify the message ─────────────────────────────────

// routeMessage runs the Router classification stage.
// Uses cheapLLM (~$0.0001) to classify the message before Frontal Lobe sees it.
// Returns nil if cheapLLM is not configured (falls back to Frontal-Lobe-only mode).
func (a *Agent) routeMessage(ctx context.Context, sessionID string, userMessage string, cheap LLMClient) *RouterDecision {
	if cheap == nil {
		return nil
	}

	// Short-circuit: if the message is obviously simple, skip the LLM call entirely.
	if IsSimpleMessage(userMessage) {
		return &RouterDecision{
			Classification: "direct_answer",
			SocraticMode:   "skip",
			ModelTier:      "cheap",
			ToolSubset:     nil,
			ExecutionMode:  "chat",
			Confidence:     0.99,
			Reasoning:      "simple greeting/acknowledgment — no LLM call needed",
		}
	}

	messages := []Message{
		{Role: RoleSystem, Content: routerSystemPrompt},
		{Role: RoleUser, Content: userMessage},
	}

	result, err := cheap.Complete(ctx, messages, nil)
	if err != nil {
		a.logger.Warn("router classification failed, skipping", "err", err)
		return nil
	}

	decision := &RouterDecision{}
	content := strings.TrimSpace(result.Content)

	// Find JSON in the response (model might wrap it in markdown).
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start >= 0 && end > start {
		content = content[start : end+1]
	}

	if err := json.Unmarshal([]byte(content), decision); err != nil {
		a.logger.Warn("router classification parse failed", "err", err, "raw", content[:min(200, len(content))])
		return nil
	}

	// Validate and apply defaults.
	if decision.Classification == "" {
		decision.Classification = "needs_tools"
	}
	if decision.ExecutionMode == "" {
		decision.ExecutionMode = "chat"
	}
	if decision.SocraticMode == "" {
		decision.SocraticMode = "skip"
	}
	if decision.ModelTier == "" {
		decision.ModelTier = "standard"
	}

	a.logger.Info("router decision",
		"classification", decision.Classification,
		"model_tier", decision.ModelTier,
		"execution_mode", decision.ExecutionMode,
		"confidence", decision.Confidence,
		"tool_subset", decision.ToolSubset,
		"reasoning", decision.Reasoning,
	)

	return decision
}

// routerSystemPrompt is set from the prompts package at init time.
// Kept as a package-level var to avoid import cycle with prompts package.
var routerSystemPrompt string

// SetRouterPrompt sets the router system prompt. Called from wire.go.
func SetRouterPrompt(prompt string) {
	routerSystemPrompt = prompt
}

// ─── STALL RECOVERY (Frontal Lobe override) ─────────────────────────────────

// stallPatterns are phrases that indicate Cortex hesitated instead of executing.
var stallPatterns = []string{
	"shall i proceed",
	"would you like me to",
	"i can write",
	"let me know if",
	"should i go ahead",
	"do you want me to",
	"i'll need your confirmation",
	"shall i create",
	"want me to proceed",
	"i'd be happy to",
	"shall i execute",
	"let me know and i",
	"if you'd like",
	"should i create",
}

// isStalled checks if Cortex asked for permission instead of executing.
// Conditions: plan says code/task + no tools invoked + reply contains permission patterns.
func isStalled(reply string, plan *TaskPlan) bool {
	if plan == nil {
		return false
	}
	// Only recover for task types that clearly need tool execution.
	if plan.Type != "code" && plan.Type != "analysis" {
		return false
	}

	lower := strings.ToLower(reply)
	for _, pattern := range stallPatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}
	return false
}

const stallRecoveryPrompt = `The user asked for a specific action and the primary AI hesitated instead of executing.

User's original request: %s
Frontal Lobe plan type: %s (steps: %s)
Primary AI's response (stalled): %s

The user clearly wants this executed. Generate the appropriate tool call(s) to fulfill the request.
Do NOT ask for permission. Do NOT describe what you would do. Execute the tool call directly.`

// recoverFromStall attempts to execute the stalled task using cheapLLM with tools.
// Returns a revised TurnOutput if recovery succeeds, nil if it doesn't.
func (a *Agent) recoverFromStall(ctx context.Context, input TurnInput, plan *TaskPlan, stalledReply string, toolDefs []ToolDefinition) *TurnOutput {
	if a.cheapLLM == nil {
		return nil
	}

	a.emit(ctx, input.SessionID, EventStallDetected, "Cortex stalled — attempting recovery", map[string]any{
		"plan_type": plan.Type,
		"stalled":   stalledReply[:min(200, len(stalledReply))],
	})
	a.logger.Info("stall detected — attempting recovery",
		"plan_type", plan.Type,
		"stalled_preview", stalledReply[:min(100, len(stalledReply))],
	)

	stepsStr := strings.Join(plan.Steps, ", ")
	prompt := fmt.Sprintf(stallRecoveryPrompt, input.UserMsg.Content, plan.Type, stepsStr, stalledReply)

	messages := []Message{
		{Role: RoleSystem, Content: prompt},
		{Role: RoleUser, Content: input.UserMsg.Content},
	}

	// Use the MAIN LLM (not cheap) with tools enabled — we need tool execution capability.
	result, err := a.llm.Complete(ctx, messages, toolDefs)
	if err != nil {
		a.logger.Warn("stall recovery LLM call failed", "err", err)
		return nil
	}

	// If the recovery also didn't produce tool calls, give up.
	if len(result.ToolCalls) == 0 {
		a.logger.Info("stall recovery also produced no tool calls — giving up")
		return nil
	}

	// Execute the tool calls from recovery.
	recoveredOutput := &TurnOutput{
		InputTokens:  result.InputTokens,
		OutputTokens: result.OutputTokens,
		TokensUsed:   result.InputTokens + result.OutputTokens,
	}
	invokedTools := make(map[string]struct{})

	// Agentic loop for recovery (up to 5 rounds).
	for round := 0; round < 5; round++ {
		if len(result.ToolCalls) == 0 {
			recoveredOutput.Reply = result.Content
			break
		}

		// Append assistant message with tool calls first.
		messages = append(messages, Message{Role: RoleAssistant, Content: result.Content, ToolCalls: result.ToolCalls})

		toolResults, files, toolErr := a.executeTools(ctx, input.SessionID, result.ToolCalls, invokedTools)
		if toolErr != nil {
			a.logger.Warn("stall recovery tool execution failed", "err", toolErr)
			return nil
		}
		recoveredOutput.Files = append(recoveredOutput.Files, files...)

		// Append tool results.
		for _, tr := range toolResults {
			messages = append(messages, Message{
				Role:       RoleTool,
				Content:    tr.Content,
				ToolCallID: tr.ToolCallID,
				IsError:    tr.IsError,
			})
		}

		// Continue the conversation with tool results.
		result, err = a.llm.Complete(ctx, messages, toolDefs)
		if err != nil {
			a.logger.Warn("stall recovery continuation failed", "err", err)
			return nil
		}
		recoveredOutput.InputTokens += result.InputTokens
		recoveredOutput.OutputTokens += result.OutputTokens
		recoveredOutput.TokensUsed += result.InputTokens + result.OutputTokens
	}

	if recoveredOutput.Reply == "" {
		recoveredOutput.Reply = result.Content
	}

	for name := range invokedTools {
		recoveredOutput.ToolsInvoked = append(recoveredOutput.ToolsInvoked, name)
	}

	a.emit(ctx, input.SessionID, EventStallRecovered, fmt.Sprintf("Recovery complete — %d tools used", len(invokedTools)), map[string]any{
		"tools_used": len(invokedTools),
	})
	a.logger.Info("stall recovery succeeded", "tools_used", len(invokedTools))

	return recoveredOutput
}

// ─── STAGE 4: HIPPOCAMPUS — Mid-task memory enrichment ──────────────────────

// enrichMemory searches memory based on topics discovered during tool execution.
// This catches "wait, we already knew part of this" moments.
// Zero LLM cost — pure database queries.
func (a *Agent) enrichMemory(ctx context.Context, sessionID string, toolResults []ToolResult) []Fact {
	if a.memory == nil || len(toolResults) == 0 {
		return nil
	}

	// Extract key topics from tool results (first 200 chars of each, concatenated).
	var topicBuilder strings.Builder
	for _, tr := range toolResults {
		if tr.IsError {
			continue
		}
		content := tr.Content
		if len(content) > 200 {
			content = content[:200]
		}
		topicBuilder.WriteString(content)
		topicBuilder.WriteString(" ")
	}
	topics := topicBuilder.String()
	if len(topics) < 20 {
		return nil // Not enough content to search on
	}

	facts, err := a.memory.Search(ctx, topics, 3)
	if err != nil {
		a.logger.Warn("mid-task memory enrichment failed", "err", err)
		return nil
	}

	if len(facts) > 0 {
		a.emit(ctx, sessionID, EventMemoryLoaded,
			fmt.Sprintf("Mid-task: found %d related memories", len(facts)),
			map[string]any{"count": len(facts), "stage": "enrichment"})
		a.logger.Info("hippocampus mid-task enrichment", "facts_found", len(facts))
	}

	return facts
}

// ─── STAGE 5: THALAMUS — Socratic verification ─────────────────────────────

const thalamusVerifyPrompt = `You are Thalamus, the verification engine in a multi-stage AI cognitive system.

Your job: review Cortex's draft reply BEFORE it reaches the user. Apply Socratic method and Aristotelian logic.

You receive:
1. The original user question
2. The execution plan that was followed
3. The evidence gathered (tool results)
4. Cortex's draft reply

Your verification checklist:
- Does the conclusion follow from the evidence? (Aristotelian deduction)
- Are there contradictions between sources?
- Did Cortex miss relevant evidence it gathered?
- Is the reasoning logically valid?
- Are there unsupported claims?
- Is anything hallucinated (stated as fact without evidence)?

Respond with ONLY a JSON object:
{
  "approved": true/false,
  "confidence": 0.0-1.0,
  "issues": ["list of specific issues found"],
  "suggestion": "if not approved, specific guidance for revision"
}

If the reply is a simple factual answer or chat response, approve it quickly (confidence 0.9+).
Only reject if there are genuine logical or factual problems.`

// verifyReply runs the Thalamus verification stage.
// Uses cheapLLM to check if Cortex's reply is logically sound.
// Returns (approved, suggestion). If approved=false, suggestion explains what to fix.
func (a *Agent) verifyReply(ctx context.Context, sessionID string, plan *TaskPlan, userQuestion, draftReply, evidence string, cheap LLMClient) (bool, string) {
	if cheap == nil || plan == nil {
		return true, "" // No cheap model or no plan → skip verification
	}

	// Skip verification for simple chat
	if plan.Verification == "none" || plan.Type == "chat" || plan.Type == "clarify" {
		return true, ""
	}

	a.emit(ctx, sessionID, EventVerifyStart, "Thalamus verifying", nil)

	// Build verification context
	verifyContent := fmt.Sprintf("## User Question\n%s\n\n## Execution Plan\nType: %s | Steps: %d\n\n",
		userQuestion, plan.Type, len(plan.Steps))

	if evidence != "" {
		// Truncate evidence to keep costs low
		if len(evidence) > 2000 {
			evidence = evidence[:2000] + "\n[truncated]"
		}
		verifyContent += fmt.Sprintf("## Evidence Gathered\n%s\n\n", evidence)
	}

	verifyContent += fmt.Sprintf("## Cortex Draft Reply\n%s", draftReply)

	messages := []Message{
		{Role: RoleSystem, Content: thalamusVerifyPrompt},
		{Role: RoleUser, Content: verifyContent},
	}

	result, err := cheap.Complete(ctx, messages, nil)
	if err != nil {
		a.logger.Warn("thalamus verification failed, approving by default", "err", err)
		a.emit(ctx, sessionID, EventVerifyComplete, "Verification skipped (error)", nil)
		return true, ""
	}

	// Parse verification result
	var verification struct {
		Approved   bool     `json:"approved"`
		Confidence float64  `json:"confidence"`
		Issues     []string `json:"issues"`
		Suggestion string   `json:"suggestion"`
	}

	content := strings.TrimSpace(result.Content)
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start >= 0 && end > start {
		content = content[start : end+1]
	}

	if err := json.Unmarshal([]byte(content), &verification); err != nil {
		a.logger.Warn("thalamus verification parse failed, approving", "err", err)
		a.emit(ctx, sessionID, EventVerifyComplete, "Verification skipped (parse error)", nil)
		return true, ""
	}

	if verification.Approved {
		a.emit(ctx, sessionID, EventVerifyComplete,
			fmt.Sprintf("Approved (confidence: %.0f%%)", verification.Confidence*100),
			map[string]any{"confidence": verification.Confidence})
		a.logger.Info("thalamus approved", "confidence", verification.Confidence)
	} else {
		a.emit(ctx, sessionID, EventVerifyComplete,
			fmt.Sprintf("Revision needed: %s", verification.Suggestion),
			map[string]any{
				"confidence": verification.Confidence,
				"issues":     verification.Issues,
				"suggestion": verification.Suggestion,
			})
		a.logger.Info("thalamus rejected",
			"confidence", verification.Confidence,
			"issues", verification.Issues,
			"suggestion", verification.Suggestion,
		)
	}

	return verification.Approved, verification.Suggestion
}
