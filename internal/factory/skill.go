package factory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ziloss-tech/zbot/internal/agent"
)

// ─── FACTORY SKILL ──────────────────────────────────────────────────────────

// Skill wraps the Software Factory pipeline as a ZBOT skill.
type Skill struct {
	pipeline *PipelineV2
	store    SessionStore // optional — nil means in-memory only

	// Active planning sessions (in-memory, keyed by plan ID).
	mu       sync.Mutex
	sessions map[string]*PlanStateV2
}

// SessionStore is the interface for persisting factory sessions.
type SessionStore interface {
	Save(ctx context.Context, state *PlanStateV2) error
	Load(ctx context.Context, id string) (*PlanStateV2, error)
	LoadIncomplete(ctx context.Context) ([]*PlanStateV2, error)
	Delete(ctx context.Context, id string) error
}

// NewSkill creates a factory skill that orchestrates the planning pipeline.
func NewSkill(pipeline *PipelineV2) *Skill {
	return &Skill{
		pipeline: pipeline,
		sessions: make(map[string]*PlanStateV2),
	}
}

// SetStore sets a persistence store for factory sessions.
// If set, sessions survive ZBOT restarts.
func (s *Skill) SetStore(store SessionStore) {
	s.store = store
}

// RestoreSessions loads incomplete sessions from the store into memory.
// Call this at startup after SetStore.
func (s *Skill) RestoreSessions(ctx context.Context) error {
	if s.store == nil {
		return nil
	}
	sessions, err := s.store.LoadIncomplete(ctx)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, state := range sessions {
		s.sessions[state.ID] = state
	}
	return nil
}

func (s *Skill) Name() string        { return "factory" }
func (s *Skill) Description() string { return "Software Factory — autonomous planning pipeline" }

func (s *Skill) Tools() []agent.Tool {
	return []agent.Tool{
		&FactoryPlanTool{skill: s},
		&FactoryContinueTool{skill: s},
	}
}

func (s *Skill) SystemPromptAddendum() string {
	return `### Software Factory (2 tools)
You can create complete software project plans through an autonomous specialist pipeline.
- factory_plan: Start a new software project from an idea. Returns interview questions for the user.
- factory_continue: Answer questions or advance the planning pipeline. Feed user answers back.

The pipeline runs: Interviewer → PRD Writer → Architect → Security Reviewer → Critic → Manifest Generator.
Each phase uses specialist AI agents. The critic watches every phase. Security is continuous.
Every decision is logged. Every requirement is traced. Tests are written by separate agents from coders.

Workflow:
1. User describes an idea → call factory_plan → returns interview questions
2. User answers questions → call factory_continue with answers → pipeline advances
3. Repeat until pipeline produces a complete task manifest for parallel dispatch`
}

func (s *Skill) getSession(id string) *PlanStateV2 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if state, ok := s.sessions[id]; ok {
		return state
	}
	// Try loading from persistent store.
	if s.store != nil {
		state, err := s.store.Load(context.Background(), id)
		if err == nil && state != nil {
			s.sessions[id] = state
			return state
		}
	}
	return nil
}

func (s *Skill) setSession(id string, state *PlanStateV2) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[id] = state
	// Persist to store if available. Best-effort — don't fail the tool call.
	if s.store != nil {
		_ = s.store.Save(context.Background(), state)
	}
}

// ─── FACTORY_PLAN TOOL ──────────────────────────────────────────────────────

// FactoryPlanTool starts a new software factory planning session.
type FactoryPlanTool struct {
	skill *Skill
}

func (t *FactoryPlanTool) Name() string { return "factory_plan" }

func (t *FactoryPlanTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "factory_plan",
		Description: "Start a new Software Factory planning session. Takes a vague idea and begins the autonomous interview → PRD → architecture → security → manifest pipeline. Returns interview questions for the user.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"idea"},
			"properties": map[string]any{
				"idea": map[string]any{
					"type":        "string",
					"description": "The user's project idea in natural language",
				},
				"plan_id": map[string]any{
					"type":        "string",
					"description": "Optional custom plan ID. Auto-generated if omitted.",
				},
			},
		},
	}
}

func (t *FactoryPlanTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	idea, _ := input["idea"].(string)
	if idea == "" {
		return &agent.ToolResult{Content: "error: idea is required", IsError: true}, nil
	}

	planID, _ := input["plan_id"].(string)
	if planID == "" {
		planID = fmt.Sprintf("plan_%d", randomPlanID())
	}

	// Check for existing session with this ID.
	if existing := t.skill.getSession(planID); existing != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("error: plan '%s' already exists (phase: %s). Use factory_continue to advance it.", planID, existing.Phase),
			IsError: true,
		}, nil
	}

	state, err := t.skill.pipeline.StartPlan(ctx, planID, idea)
	if err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("error starting plan: %v", err),
			IsError: true,
		}, nil
	}

	// Store the session.
	t.skill.setSession(planID, state)

	// Format the response.
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## 🏭 Software Factory — Plan `%s`\n\n", planID))
	sb.WriteString(fmt.Sprintf("**Idea:** %s\n", idea))
	sb.WriteString(fmt.Sprintf("**Phase:** %s\n\n", state.Phase))

	if len(state.PendingQuestions) > 0 {
		sb.WriteString("### Interview Questions\n\n")
		sb.WriteString("The interviewer needs answers to these before proceeding:\n\n")
		for _, q := range state.PendingQuestions {
			sb.WriteString(fmt.Sprintf("%s\n", q))
		}
		sb.WriteString(fmt.Sprintf("\n---\n*Use `factory_continue` with plan_id `%s` to provide answers.*\n", planID))
	}

	return &agent.ToolResult{Content: sb.String()}, nil
}

var _ agent.Tool = (*FactoryPlanTool)(nil)

// ─── FACTORY_CONTINUE TOOL ─────────────────────────────────────────────────

// FactoryContinueTool advances an existing planning session.
type FactoryContinueTool struct {
	skill *Skill
}

func (t *FactoryContinueTool) Name() string { return "factory_continue" }

func (t *FactoryContinueTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "factory_continue",
		Description: "Continue an existing Software Factory planning session. Provide user answers to advance the pipeline, or approve the current phase output to move to the next phase.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"plan_id"},
			"properties": map[string]any{
				"plan_id": map[string]any{
					"type":        "string",
					"description": "The plan ID returned by factory_plan",
				},
				"answers": map[string]any{
					"type":        "string",
					"description": "User's answers to interview questions, or 'approve' to advance to next phase",
				},
			},
		},
	}
}

func (t *FactoryContinueTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	planID, _ := input["plan_id"].(string)
	if planID == "" {
		return &agent.ToolResult{Content: "error: plan_id is required", IsError: true}, nil
	}

	answers, _ := input["answers"].(string)

	state := t.skill.getSession(planID)
	if state == nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("error: no active plan with ID '%s'. Use factory_plan to start one.", planID),
			IsError: true,
		}, nil
	}

	if state.Phase == PhaseComplete {
		return t.formatCompleteState(state)
	}

	// Advance the pipeline.
	var err error
	if answers != "" {
		state, err = t.skill.pipeline.AnswerQuestions(ctx, state, answers)
	} else {
		state, err = t.skill.pipeline.advancePhase(ctx, state)
	}
	if err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("error advancing plan: %v", err),
			IsError: true,
		}, nil
	}

	// Update stored session.
	t.skill.setSession(planID, state)

	return t.formatState(state)
}

func (t *FactoryContinueTool) formatState(state *PlanStateV2) (*agent.ToolResult, error) {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## 🏭 Software Factory — Plan `%s`\n\n", state.ID))
	sb.WriteString(fmt.Sprintf("**Phase:** %s\n", state.Phase))

	// Show critic feedback if any.
	if len(state.CriticFeedback) > 0 {
		latest := state.CriticFeedback[len(state.CriticFeedback)-1]
		icon := "ℹ️"
		switch latest.Severity {
		case "warning":
			icon = "⚠️"
		case "concern":
			icon = "🔶"
		case "blocker":
			icon = "🛑"
		}
		sb.WriteString(fmt.Sprintf("\n**Critic [%s %s]:** %s\n", icon, latest.Severity, truncateOutput(latest.Message, 300)))
	}

	// Show focus score if available.
	if state.Focus != nil {
		sb.WriteString(fmt.Sprintf("**Focus Score:** %.0f%% (%d/%d decisions traced to PRD)\n",
			state.Focus.DriftScore*100, state.Focus.TracedModules, state.Focus.TotalModules))
	}

	// Show pending questions.
	if len(state.PendingQuestions) > 0 {
		sb.WriteString("\n### Questions\n\n")
		for _, q := range state.PendingQuestions {
			sb.WriteString(fmt.Sprintf("%s\n", q))
		}
		sb.WriteString(fmt.Sprintf("\n*Provide answers via `factory_continue` with plan_id `%s`.*\n", state.ID))
		return &agent.ToolResult{Content: sb.String()}, nil
	}

	// Show phase output.
	switch state.Phase {
	case PhasePRD:
		if state.PRD != "" {
			sb.WriteString("\n### PRD Generated\n\n")
			sb.WriteString(truncateOutput(state.PRD, 2000))
			sb.WriteString(fmt.Sprintf("\n\n*Call `factory_continue` with plan_id `%s` to advance to architecture phase.*\n", state.ID))
		}
	case PhaseArchitect:
		if state.Architecture != "" {
			sb.WriteString("\n### Architecture Generated\n\n")
			sb.WriteString(truncateOutput(state.Architecture, 2000))
			sb.WriteString(fmt.Sprintf("\n\n*Call `factory_continue` with plan_id `%s` to advance to security review.*\n", state.ID))
		}
	case PhaseSecurity:
		if len(state.SecurityNotes) > 0 {
			sb.WriteString(fmt.Sprintf("\n### Security Review (%d findings)\n\n", len(state.SecurityNotes)))
			for _, note := range state.SecurityNotes {
				sb.WriteString(fmt.Sprintf("- %s\n", note))
			}
			sb.WriteString(fmt.Sprintf("\n*Call `factory_continue` with plan_id `%s` to generate task manifest.*\n", state.ID))
		}
	case PhaseComplete:
		return t.formatCompleteState(state)
	}

	// Show errors.
	if len(state.Errors) > 0 {
		sb.WriteString("\n### Errors\n")
		for _, e := range state.Errors {
			sb.WriteString(fmt.Sprintf("- ❌ %s\n", e))
		}
	}

	return &agent.ToolResult{Content: sb.String()}, nil
}

func (t *FactoryContinueTool) formatCompleteState(state *PlanStateV2) (*agent.ToolResult, error) {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## 🏭 Software Factory — Plan `%s` ✅ COMPLETE\n\n", state.ID))

	// Summary stats.
	decisions := state.Decisions.All()
	sb.WriteString(fmt.Sprintf("**Total decisions logged:** %d\n", len(decisions)))
	sb.WriteString(fmt.Sprintf("**Critic notes:** %d\n", len(state.CriticFeedback)))
	if state.Focus != nil {
		sb.WriteString(fmt.Sprintf("**Focus score:** %.0f%%\n", state.Focus.DriftScore*100))
	}
	sb.WriteString(fmt.Sprintf("**Security findings:** %d\n", len(state.SecurityNotes)))
	sb.WriteString(fmt.Sprintf("**Data sensitivity:** %s\n", state.DataSensitivity))
	sb.WriteString(fmt.Sprintf("**Security level:** %s\n\n", state.SecurityLevel))

	// Artifacts produced.
	sb.WriteString("### Artifacts\n")
	if state.PRD != "" {
		sb.WriteString("- ✅ PRD with traceable requirement IDs\n")
	}
	if state.Architecture != "" {
		sb.WriteString("- ✅ Architecture with layered build plan\n")
	}
	if len(state.SecurityNotes) > 0 {
		sb.WriteString("- ✅ Security review\n")
	}
	sb.WriteString("- ✅ Decision log\n")
	sb.WriteString("- ✅ Task manifest (ready for parallel dispatch)\n")

	sb.WriteString("\n### Next Step\n")
	sb.WriteString("Use `parallel_dispatch` with the generated manifest to farm coding tasks to Qwen.\n")

	// Include the decision log summary.
	if len(decisions) > 0 {
		sb.WriteString("\n### Decision Log (last 10)\n\n")
		start := 0
		if len(decisions) > 10 {
			start = len(decisions) - 10
		}
		for _, d := range decisions[start:] {
			sb.WriteString(fmt.Sprintf("- **[%s]** %s — %s\n", d.Agent, d.Choice, truncateOutput(d.Rationale, 100)))
		}
	}

	return &agent.ToolResult{Content: sb.String()}, nil
}

var _ agent.Tool = (*FactoryContinueTool)(nil)

// ─── HELPERS ────────────────────────────────────────────────────────────────

func truncateOutput(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "\n[...truncated...]"
}

func randomPlanID() int64 {
	// Simple timestamp-based ID — good enough for in-memory sessions.
	return time.Now().UnixNano() / 1e6
}

// MarshalSessionJSON serializes a plan state for external storage.
// The DecisionLog is included as a flat array.
func MarshalSessionJSON(state *PlanStateV2) ([]byte, error) {
	// Create a serializable wrapper.
	type exportState struct {
		PlanStateV2
		DecisionList []Decision `json:"decisions"`
	}
	export := exportState{
		PlanStateV2:  *state,
		DecisionList: state.Decisions.All(),
	}
	return json.Marshal(export)
}
