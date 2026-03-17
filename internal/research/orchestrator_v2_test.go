package research

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/jeremylerwick-max/zbot/internal/agent"
)

// mockLLMClient implements agent.LLMClient for testing.
type mockLLMClient struct {
	name      string
	responses []string // queued responses, popped in order
	callIdx   int
}

func (m *mockLLMClient) Complete(_ context.Context, _ []agent.Message, _ []agent.ToolDefinition) (*agent.CompletionResult, error) {
	resp := "{}"
	if m.callIdx < len(m.responses) {
		resp = m.responses[m.callIdx]
		m.callIdx++
	}
	return &agent.CompletionResult{
		Content:      resp,
		InputTokens:  100,
		OutputTokens: 50,
	}, nil
}

func (m *mockLLMClient) CompleteStream(_ context.Context, _ []agent.Message, _ []agent.ToolDefinition, out chan<- string) error {
	close(out)
	return nil
}

func (m *mockLLMClient) ModelName() string { return m.name }

// mockSearchTool returns fake search results.
type mockSearchTool struct{}

func (t *mockSearchTool) Name() string                    { return "web_search" }
func (t *mockSearchTool) Definition() agent.ToolDefinition { return agent.ToolDefinition{Name: "web_search"} }
func (t *mockSearchTool) Execute(_ context.Context, _ map[string]any) (*agent.ToolResult, error) {
	return &agent.ToolResult{
		Content: "**URL:** https://example.com/article\nExample article about AI\n",
	}, nil
}

func TestNewV2ResearchOrchestrator(t *testing.T) {
	haiku := &mockLLMClient{name: "claude-haiku-4-5"}
	sonnet := &mockLLMClient{name: "claude-sonnet-4-6"}

	orch := NewV2ResearchOrchestrator(
		haiku, sonnet,
		&mockSearchTool{},
		nil, nil, nil, nil, slog.Default(),
	)

	if orch == nil {
		t.Fatal("expected non-nil orchestrator")
	}
	if orch.haiku.ModelName() != "claude-haiku-4-5" {
		t.Errorf("haiku model = %q, want claude-haiku-4-5", orch.haiku.ModelName())
	}
	if orch.sonnet.ModelName() != "claude-sonnet-4-6" {
		t.Errorf("sonnet model = %q, want claude-sonnet-4-6", orch.sonnet.ModelName())
	}
}

func TestEstimateHaikuCost(t *testing.T) {
	// 1000 input + 500 output tokens
	cost := estimateHaikuCost(1000, 500)
	// Expected: 1000*0.25/1e6 + 500*1.25/1e6 = 0.00025 + 0.000625 = 0.000875
	if cost < 0.0008 || cost > 0.0009 {
		t.Errorf("haiku cost = %f, want ~0.000875", cost)
	}
}

func TestEstimateSonnetCost(t *testing.T) {
	cost := estimateSonnetCost(1000, 500)
	// Expected: 1000*3/1e6 + 500*15/1e6 = 0.003 + 0.0075 = 0.0105
	if cost < 0.010 || cost > 0.011 {
		t.Errorf("sonnet cost = %f, want ~0.0105", cost)
	}
}

func TestV2RunDeepResearch_HappyPath(t *testing.T) {
	planJSON, _ := json.Marshal(ResearchPlan{
		Goal:         "test research",
		SubQuestions: []string{"What is AI?"},
		SearchTerms:  []string{"artificial intelligence"},
		Depth:        "shallow",
	})
	claimSetJSON, _ := json.Marshal(ClaimSet{
		Claims: []Claim{
			{ID: "CLM_001", Statement: "AI is growing fast", EvidenceIDs: []string{"SRC_001"}, Confidence: 0.9},
		},
		Gaps:      []string{},
		SourceIDs: []string{"SRC_001"},
	})
	critiqueJSON, _ := json.Marshal(CritiqueReport{
		Passed:          true,
		ConfidenceScore: 0.85,
	})

	haiku := &mockLLMClient{
		name: "claude-haiku-4-5",
		responses: []string{
			string(planJSON),
			string(claimSetJSON),
		},
	}
	sonnet := &mockLLMClient{
		name: "claude-sonnet-4-6",
		responses: []string{
			string(critiqueJSON),
			"# Research Report\n\nAI is growing fast [1].\n\n## Sources\n[1] Example — https://example.com",
		},
	}

	orch := NewV2ResearchOrchestrator(
		haiku, sonnet,
		&mockSearchTool{},
		nil, nil, nil, nil, slog.Default(),
	)

	state, err := orch.RunDeepResearch(context.Background(), "test research", "test-session-1")
	if err != nil {
		t.Fatalf("RunDeepResearch failed: %v", err)
	}

	if !state.Complete {
		t.Error("expected state.Complete = true")
	}
	if state.FinalReport == "" {
		t.Error("expected non-empty FinalReport")
	}
	if len(state.Claims) == 0 {
		t.Error("expected at least one claim")
	}
	if state.Iteration != 1 {
		t.Errorf("expected 1 iteration (passed on first try), got %d", state.Iteration)
	}
	if state.CostUSD <= 0 {
		t.Error("expected positive cost")
	}
}

func TestV2SessionManagement(t *testing.T) {
	haiku := &mockLLMClient{name: "haiku"}
	sonnet := &mockLLMClient{name: "sonnet"}

	orch := NewV2ResearchOrchestrator(
		haiku, sonnet, &mockSearchTool{},
		nil, nil, nil, nil, slog.Default(),
	)

	// No sessions should be running.
	if orch.IsRunning("nonexistent") {
		t.Error("expected IsRunning=false for nonexistent session")
	}

	// GetEmitter should return nil for nonexistent sessions.
	if em := orch.GetEmitter("nonexistent"); em != nil {
		t.Error("expected nil emitter for nonexistent session")
	}
}
