package factory

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/ziloss-tech/zbot/internal/agent"
)

// ─── MOCK LLM CLIENT ────────────────────────────────────────────────────────

// mockLLM returns canned responses for testing the factory pipeline.
type mockLLM struct {
	responses []string
	callIndex int
	calls     [][]agent.Message // recorded calls for assertion
}

func newMockLLM(responses ...string) *mockLLM {
	return &mockLLM{responses: responses}
}

func (m *mockLLM) Complete(ctx context.Context, messages []agent.Message, tools []agent.ToolDefinition) (*agent.CompletionResult, error) {
	m.calls = append(m.calls, messages)
	resp := "mock response"
	if m.callIndex < len(m.responses) {
		resp = m.responses[m.callIndex]
	}
	m.callIndex++
	return &agent.CompletionResult{
		Content:      resp,
		InputTokens:  100,
		OutputTokens: 50,
	}, nil
}

func (m *mockLLM) CompleteStream(ctx context.Context, messages []agent.Message, tools []agent.ToolDefinition, out chan<- string) error {
	close(out)
	return nil
}

func (m *mockLLM) ModelName() string { return "mock-model" }

// ─── SKILL INTERFACE TESTS ──────────────────────────────────────────────────

func TestSkillImplementsInterface(t *testing.T) {
	cheapModel := newMockLLM()
	smartModel := newMockLLM()
	pipeline := NewPipelineV2(cheapModel, smartModel, testLogger())
	skill := NewSkill(pipeline)

	if skill.Name() != "factory" {
		t.Errorf("expected skill name 'factory', got '%s'", skill.Name())
	}

	if skill.Description() == "" {
		t.Error("skill description should not be empty")
	}

	tools := skill.Tools()
	if len(tools) != 2 {
		t.Errorf("expected 2 tools, got %d", len(tools))
	}

	toolNames := make(map[string]bool)
	for _, tool := range tools {
		toolNames[tool.Name()] = true
	}
	if !toolNames["factory_plan"] {
		t.Error("missing factory_plan tool")
	}
	if !toolNames["factory_continue"] {
		t.Error("missing factory_continue tool")
	}

	if skill.SystemPromptAddendum() == "" {
		t.Error("system prompt addendum should not be empty")
	}
}

// ─── FACTORY_PLAN TOOL TESTS ────────────────────────────────────────────────

func TestFactoryPlanRequiresIdea(t *testing.T) {
	skill := newTestSkill()
	tool := skill.Tools()[0] // factory_plan

	result, err := tool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error result when idea is missing")
	}
	if result.Content != "error: idea is required" {
		t.Errorf("unexpected error message: %s", result.Content)
	}
}

func TestFactoryPlanStartsSession(t *testing.T) {
	cheapModel := newMockLLM("1. Who are the target users?\n2. What data does it handle?\n3. How sensitive is that data?\n4. What platform?\n5. Authentication needed?")
	smartModel := newMockLLM()
	pipeline := NewPipelineV2(cheapModel, smartModel, testLogger())
	skill := NewSkill(pipeline)
	tool := skill.Tools()[0] // factory_plan

	result, err := tool.Execute(context.Background(), map[string]any{
		"idea":    "a supplement tracker app",
		"plan_id": "test-plan-001",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", result.Content)
	}

	if !strings.Contains(result.Content, "test-plan-001") {
		t.Error("result should contain the plan ID")
	}
	if !strings.Contains(result.Content, "Interview Questions") {
		t.Error("result should contain interview questions section")
	}

	session := skill.getSession("test-plan-001")
	if session == nil {
		t.Fatal("session should be stored after starting a plan")
	}
	if session.Phase != PhaseInterview {
		t.Errorf("expected phase 'interview', got '%s'", session.Phase)
	}
	if session.Idea != "a supplement tracker app" {
		t.Errorf("expected idea to be stored, got '%s'", session.Idea)
	}
}

func TestFactoryPlanRejectsDuplicateID(t *testing.T) {
	cheapModel := newMockLLM("1. Question?")
	smartModel := newMockLLM()
	pipeline := NewPipelineV2(cheapModel, smartModel, testLogger())
	skill := NewSkill(pipeline)
	tool := skill.Tools()[0]

	_, err := tool.Execute(context.Background(), map[string]any{
		"idea":    "first idea",
		"plan_id": "dup-id",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, err := tool.Execute(context.Background(), map[string]any{
		"idea":    "second idea",
		"plan_id": "dup-id",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error when using duplicate plan ID")
	}
	if !strings.Contains(result.Content, "already exists") {
		t.Errorf("error should mention plan already exists: %s", result.Content)
	}
}

func TestFactoryPlanAutoGeneratesID(t *testing.T) {
	cheapModel := newMockLLM("1. Question?")
	smartModel := newMockLLM()
	pipeline := NewPipelineV2(cheapModel, smartModel, testLogger())
	skill := NewSkill(pipeline)
	tool := skill.Tools()[0]

	result, err := tool.Execute(context.Background(), map[string]any{
		"idea": "an app with no explicit plan_id",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "plan_") {
		t.Error("result should contain an auto-generated plan ID")
	}
}

// ─── FACTORY_CONTINUE TOOL TESTS ────────────────────────────────────────────

func TestFactoryContinueRequiresPlanID(t *testing.T) {
	skill := newTestSkill()
	tool := skill.Tools()[1] // factory_continue

	result, err := tool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error result when plan_id is missing")
	}
}

func TestFactoryContinueRejectsUnknownPlan(t *testing.T) {
	skill := newTestSkill()
	tool := skill.Tools()[1]

	result, err := tool.Execute(context.Background(), map[string]any{
		"plan_id": "nonexistent",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for unknown plan ID")
	}
	if !strings.Contains(result.Content, "no active plan") {
		t.Errorf("error should mention no active plan: %s", result.Content)
	}
}

func TestFactoryContinueAdvancesToPRD(t *testing.T) {
	// Mock responses:
	// 1. Interviewer initial questions
	// 2. Interviewer assesses answers → SPEC_READY + spec
	// 3. PRD writer generates PRD
	// cheapModel calls: [1] interviewer questions, [2] answers assessment, [3] PRD generation
	// smartModel calls: [1] critic on spec, [2] critic on PRD
	cheapModel := newMockLLM(
		"1. Who uses this?\n2. What data?",
		"SPEC_READY\n\n1. Users can track supplements\n2. Daily reminders",
		"# PRD\n\n## Requirements\nREQ-001: Track supplements\nREQ-002: Daily reminders",
	)
	smartModel := newMockLLM(
		"Good functional spec. No major concerns. Severity: info",
		"Solid PRD with clear requirement IDs. Severity: info",
	)

	pipeline := NewPipelineV2(cheapModel, smartModel, testLogger())
	skill := NewSkill(pipeline)
	planTool := skill.Tools()[0]
	continueTool := skill.Tools()[1]

	// Step 1: Start plan.
	_, err := planTool.Execute(context.Background(), map[string]any{
		"idea":    "supplement tracker",
		"plan_id": "test-advance",
	})
	if err != nil {
		t.Fatalf("start plan error: %v", err)
	}

	session := skill.getSession("test-advance")
	if session.Phase != PhaseInterview {
		t.Fatalf("expected interview phase, got %s", session.Phase)
	}

	// Step 2: Answer questions → should advance to PRD.
	result, err := continueTool.Execute(context.Background(), map[string]any{
		"plan_id": "test-advance",
		"answers": "1. Individual users tracking daily supplements\n2. Supplement names, doses, timestamps — personal health data, confidential",
	})
	if err != nil {
		t.Fatalf("continue error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", result.Content)
	}

	// Session should have advanced past interview.
	session = skill.getSession("test-advance")
	if session.Phase == PhaseInterview {
		t.Error("expected to advance past interview phase")
	}

	// Should have a functional spec.
	if session.FunctionalSpec == "" {
		t.Error("functional spec should be populated after interview")
	}

	// Should have critic feedback.
	if len(session.CriticFeedback) == 0 {
		t.Error("critic feedback should be present after spec review")
	}
}

func TestFactoryContinueCompletePlanReportsStatus(t *testing.T) {
	skill := newTestSkill()
	continueTool := skill.Tools()[1]

	// Manually create a completed session.
	state := &PlanStateV2{
		ID:              "completed-plan",
		Idea:            "test app",
		Phase:           PhaseComplete,
		PRD:             "Some PRD content",
		Architecture:    "Some architecture",
		SecurityNotes:   []string{"SECURITY: LOW | No issues found"},
		DataSensitivity: "internal",
		SecurityLevel:   "standard",
		Decisions:       NewDecisionLog(),
		CriticFeedback: []CriticNote{
			{Severity: "info", Message: "Looks good"},
		},
		Focus: &FocusScore{DriftScore: 0.95, TotalModules: 10, TracedModules: 9},
	}
	state.Decisions.Record(Decision{Agent: "architect", Choice: "Go", Rationale: "Performance"})
	skill.setSession("completed-plan", state)

	result, err := continueTool.Execute(context.Background(), map[string]any{
		"plan_id": "completed-plan",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", result.Content)
	}

	if !strings.Contains(result.Content, "COMPLETE") {
		t.Error("result should indicate plan is complete")
	}
	if !strings.Contains(result.Content, "Focus score") {
		t.Error("result should show focus score")
	}
	if !strings.Contains(result.Content, "Decision Log") {
		t.Error("result should include decision log")
	}
}

// ─── TOOL DEFINITION TESTS ─────────────────────────────────────────────────

func TestToolDefinitionsAreValid(t *testing.T) {
	skill := newTestSkill()

	for _, tool := range skill.Tools() {
		def := tool.Definition()

		if def.Name == "" {
			t.Error("tool definition name should not be empty")
		}
		if def.Name != tool.Name() {
			t.Errorf("tool Name() '%s' doesn't match Definition().Name '%s'", tool.Name(), def.Name)
		}
		if def.Description == "" {
			t.Errorf("tool '%s' should have a description", def.Name)
		}
		if def.InputSchema == nil {
			t.Errorf("tool '%s' should have an input schema", def.Name)
		}

		props, ok := def.InputSchema["properties"]
		if !ok {
			t.Errorf("tool '%s' schema should have properties", def.Name)
		}
		if props == nil {
			t.Errorf("tool '%s' properties should not be nil", def.Name)
		}
	}
}

// ─── HELPERS ────────────────────────────────────────────────────────────────

func newTestSkill() *Skill {
	cheapModel := newMockLLM()
	smartModel := newMockLLM()
	pipeline := NewPipelineV2(cheapModel, smartModel, testLogger())
	return NewSkill(pipeline)
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
