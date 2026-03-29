package memory

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ziloss-tech/zbot/internal/agent"
)

// batchMockLLM returns canned responses for batch builder tests.
type batchMockLLM struct {
	clusterResponse  string
	compressResponse string
	callCount        int
}

func (m *batchMockLLM) Complete(_ context.Context, messages []agent.Message, _ []agent.ToolDefinition) (*agent.CompletionResult, error) {
	m.callCount++
	prompt := messages[len(messages)-1].Content

	// Return cluster response for clustering prompts, compress for compression
	var content string
	if len(prompt) > 100 && (strings.Contains(prompt, "Group these") || strings.Contains(prompt, "topic clusters")) {
		content = m.clusterResponse
	} else {
		content = m.compressResponse
	}
	return &agent.CompletionResult{Content: content, Model: "mock-deepseek"}, nil
}

func (m *batchMockLLM) CompleteStream(_ context.Context, _ []agent.Message, _ []agent.ToolDefinition, out chan<- string) error {
	close(out)
	return nil
}

func (m *batchMockLLM) ModelName() string { return "mock-deepseek-v3.2" }


// mockMemStore wraps InMemoryStore to also implement List for the batch builder.
type mockMemStore struct {
	facts []agent.Fact
}

func (m *mockMemStore) Save(_ context.Context, f agent.Fact) error {
	m.facts = append(m.facts, f)
	return nil
}

func (m *mockMemStore) Search(_ context.Context, _ string, _ int) ([]agent.Fact, error) {
	return m.facts, nil
}

func (m *mockMemStore) Delete(_ context.Context, _ string) error { return nil }

func (m *mockMemStore) List(_ context.Context, limit int) ([]agent.Fact, error) {
	if limit > len(m.facts) {
		return m.facts, nil
	}
	return m.facts[:limit], nil
}

func (m *mockMemStore) Count(_ context.Context) (int64, error) {
	return int64(len(m.facts)), nil
}

func (m *mockMemStore) Stats(_ context.Context) (*MemoryStats, error) {
	return &MemoryStats{Total: int64(len(m.facts))}, nil
}

func (m *mockMemStore) AutoSave(_ context.Context, _, _ string) {}


func TestBatchBuilder_Run(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	// Seed mock facts
	facts := []agent.Fact{
		{ID: "f1", Content: "ZBOT is an AI agent written in Go with a Pantheon architecture", Source: "conversation", CreatedAt: time.Now()},
		{ID: "f2", Content: "ZBOT uses pgvector for memory and Vertex AI for embeddings", Source: "conversation", CreatedAt: time.Now()},
		{ID: "f3", Content: "GHL workflow migration tool clones workflows between locations automatically", Source: "conversation", CreatedAt: time.Now()},
		{ID: "f4", Content: "GHL uses Firebase JWT tokens with 60-minute TTL for API auth", Source: "conversation", CreatedAt: time.Now()},
		{ID: "f5", Content: "Jeremy is the CEO of Ziloss Technologies and Lead Certain", Source: "agent", CreatedAt: time.Now()},
	}
	memStore := &mockMemStore{facts: facts}
	pkgStore := NewInMemoryPackageStore()

	// Build mock LLM cluster response
	clusters := []Cluster{
		{Label: "projects/zbot", Keywords: []string{"zbot", "agent", "go", "pantheon", "memory"}, FactIDs: []string{"f1", "f2"}},
		{Label: "ghl/workflows", Keywords: []string{"ghl", "workflow", "migration", "firebase"}, FactIDs: []string{"f3", "f4"}},
		{Label: "identity/jeremy", Keywords: []string{"jeremy", "ziloss", "lead", "certain", "ceo"}, FactIDs: []string{"f5"}},
	}
	clusterJSON, _ := json.Marshal(clusters)

	llm := &batchMockLLM{
		clusterResponse:  string(clusterJSON),
		compressResponse: "Compressed memory block with all relevant facts preserved.",
	}

	builder := NewBatchBuilder(memStore, pkgStore, llm, logger)

	result, err := builder.Run(ctx)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Verify results
	if result.FactsRead != 5 {
		t.Errorf("FactsRead = %d, want 5", result.FactsRead)
	}
	if result.ClustersFound != 3 {
		t.Errorf("ClustersFound = %d, want 3", result.ClustersFound)
	}
	if result.PackagesCreated != 3 {
		t.Errorf("PackagesCreated = %d, want 3", result.PackagesCreated)
	}
	if result.LLMCalls < 4 {
		// 1 cluster call + 3 compress calls = 4 minimum
		t.Errorf("LLMCalls = %d, want >= 4", result.LLMCalls)
	}
	if len(result.Errors) > 0 {
		t.Errorf("unexpected errors: %v", result.Errors)
	}

	// Verify packages were saved
	pkgs, _ := pkgStore.ListPackages(ctx)
	if len(pkgs) != 3 {
		t.Fatalf("packages saved = %d, want 3", len(pkgs))
	}

	// Check package labels exist
	labels := make(map[string]bool)
	for _, p := range pkgs {
		labels[p.Label] = true
	}
	for _, want := range []string{"projects/zbot", "ghl/workflows", "identity/jeremy"} {
		if !labels[want] {
			t.Errorf("missing package label: %s", want)
		}
	}
}

func TestBatchBuilder_EmptyFacts(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	memStore := &mockMemStore{facts: nil}
	pkgStore := NewInMemoryPackageStore()
	llm := &batchMockLLM{}

	builder := NewBatchBuilder(memStore, pkgStore, llm, logger)
	result, err := builder.Run(ctx)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.FactsRead != 0 {
		t.Errorf("FactsRead = %d, want 0", result.FactsRead)
	}
	if llm.callCount != 0 {
		t.Errorf("LLM should not have been called for empty facts, got %d calls", llm.callCount)
	}
}

func TestBatchBuilder_VersionIncrement(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	facts := []agent.Fact{
		{ID: "f1", Content: "ZBOT fact one", Source: "conversation", CreatedAt: time.Now()},
	}
	memStore := &mockMemStore{facts: facts}
	pkgStore := NewInMemoryPackageStore()

	clusters := []Cluster{
		{Label: "projects/zbot", Keywords: []string{"zbot"}, FactIDs: []string{"f1"}},
	}
	clusterJSON, _ := json.Marshal(clusters)
	llm := &batchMockLLM{
		clusterResponse:  string(clusterJSON),
		compressResponse: "Compressed ZBOT fact.",
	}

	builder := NewBatchBuilder(memStore, pkgStore, llm, logger)

	// Run once — should create version 1
	_, err := builder.Run(ctx)
	if err != nil {
		t.Fatalf("Run 1: %v", err)
	}
	pkg1, _ := pkgStore.GetPackage(ctx, "pkg-projects-zbot")
	if pkg1 == nil {
		t.Fatal("package not found after first run")
	}
	if pkg1.Version != 1 {
		t.Errorf("first run version = %d, want 1", pkg1.Version)
	}

	// Run again — should increment to version 2
	_, err = builder.Run(ctx)
	if err != nil {
		t.Fatalf("Run 2: %v", err)
	}
	pkg2, _ := pkgStore.GetPackage(ctx, "pkg-projects-zbot")
	if pkg2.Version != 2 {
		t.Errorf("second run version = %d, want 2", pkg2.Version)
	}
}

func TestSanitizeID(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"ghl/workflows", "ghl-workflows"},
		{"projects/zbot", "projects-zbot"},
		{"identity/jeremy lerwick", "identity-jeremy-lerwick"},
		{"UPPER_Case", "upper-case"},
		{"no--double---dashes", "no-double-dashes"},
	}
	for _, tt := range tests {
		got := sanitizeID(tt.input)
		if got != tt.want {
			t.Errorf("sanitizeID(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestEstimateTokens(t *testing.T) {
	// ~4 chars per token
	if got := estimateTokens("hello world"); got < 2 || got > 4 {
		t.Errorf("estimateTokens('hello world') = %d, want 2-4", got)
	}
	if got := estimateTokens(""); got != 0 {
		t.Errorf("estimateTokens('') = %d, want 0", got)
	}
}
