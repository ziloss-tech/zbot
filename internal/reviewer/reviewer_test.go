package reviewer

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ziloss-tech/zbot/internal/agent"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultReviewConfig()

	if cfg.Enabled {
		t.Errorf("expected Enabled=false, got %v", cfg.Enabled)
	}
	if cfg.ReviewInterval != 60*time.Second {
		t.Errorf("expected ReviewInterval=60s, got %v", cfg.ReviewInterval)
	}
	if cfg.ModelName != "gpt-4o-mini" {
		t.Errorf("expected ModelName=gpt-4o-mini, got %s", cfg.ModelName)
	}
	if cfg.MaxCostPerDay != 2.00 {
		t.Errorf("expected MaxCostPerDay=2.00, got %f", cfg.MaxCostPerDay)
	}
	if cfg.MaxChunksPerCycle != 10 {
		t.Errorf("expected MaxChunksPerCycle=10, got %d", cfg.MaxChunksPerCycle)
	}
	if cfg.MinSeverity != "warning" {
		t.Errorf("expected MinSeverity=warning, got %s", cfg.MinSeverity)
	}
}

func TestChunkRecentEvents(t *testing.T) {
	eventBus := agent.NewMemEventBus(200)
	sessionID := "test-session"

	// Emit some events
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		eventBus.Emit(ctx, agent.AgentEvent{
			SessionID: sessionID,
			Type:      agent.EventToolCalled,
			Summary:   fmt.Sprintf("Tool call %d", i),
			Timestamp: time.Now(),
		})
	}

	chunks := ChunkRecentEvents(eventBus, sessionID, 5)

	if len(chunks) == 0 {
		t.Fatal("expected at least one chunk")
	}

	for _, chunk := range chunks {
		if chunk.SessionID != sessionID {
			t.Errorf("expected SessionID=%s, got %s", sessionID, chunk.SessionID)
		}
		if len(chunk.Actions) == 0 {
			t.Error("expected non-empty Actions")
		}
		if chunk.TokenEst <= 0 {
			t.Error("expected TokenEst > 0")
		}
	}
}

func TestChunkRecentEventsNoEvents(t *testing.T) {
	eventBus := agent.NewMemEventBus(200)
	sessionID := "test-session"

	chunks := ChunkRecentEvents(eventBus, sessionID, 5)

	if len(chunks) != 0 {
		t.Errorf("expected no chunks for empty event bus, got %d", len(chunks))
	}
}

func TestReviewOnceNoChecker(t *testing.T) {
	cfg := DefaultReviewConfig()
	cfg.APIKey = "" // Disable checker
	eventBus := agent.NewMemEventBus(200)
	logger := slog.Default()

	engine := NewReviewEngine(cfg, eventBus, logger)

	ctx := context.Background()
	_, err := engine.ReviewOnce(ctx)

	if err == nil {
		t.Error("expected error when no checker is configured")
	}
	if err.Error() != "no checker configured (missing API key)" {
		t.Errorf("expected specific error message, got: %v", err)
	}
}

func TestReviewOnceWithMockedChecker(t *testing.T) {
	// Create a mock HTTP server that returns a review finding
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]string{
						"content": `[{"severity":"warning","category":"logic","description":"Potential issue detected","location":"chunk-0","suggestion":"Fix the logic","confidence":0.8}]`,
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	cfg := DefaultReviewConfig()
	cfg.ModelEndpoint = server.URL
	cfg.APIKey = "test-key"

	eventBus := agent.NewMemEventBus(200)
	logger := slog.Default()

	// Emit some events first
	ctx := context.Background()
	eventBus.Emit(ctx, agent.AgentEvent{
		SessionID: "web-chat",
		Type:      agent.EventToolCalled,
		Summary:   "Test tool call",
		Timestamp: time.Now(),
	})

	engine := NewReviewEngine(cfg, eventBus, logger)

	findings, err := engine.ReviewOnce(ctx)
	if err != nil {
		t.Fatalf("ReviewOnce failed: %v", err)
	}

	if len(findings) == 0 {
		t.Error("expected at least one finding")
	}

	for _, f := range findings {
		if f.Severity != "warning" {
			t.Errorf("expected Severity=warning, got %s", f.Severity)
		}
		if f.Category != "logic" {
			t.Errorf("expected Category=logic, got %s", f.Category)
		}
		if f.ReviewerModel != cfg.ModelName {
			t.Errorf("expected ReviewerModel=%s, got %s", cfg.ModelName, f.ReviewerModel)
		}
	}
}

func TestCostTracking(t *testing.T) {
	cfg := DefaultReviewConfig()
	cfg.MaxCostPerDay = 1.00
	cfg.APIKey = "test-key"

	eventBus := agent.NewMemEventBus(200)
	logger := slog.Default()

	engine := NewReviewEngine(cfg, eventBus, logger)

	// Manually set today's cost to near the cap
	engine.mu.Lock()
	engine.todayCost = 0.95
	engine.mu.Unlock()

	// Create a mock checker that would consume 0.10 cost
	checker := &MockChecker{
		findings: []ReviewFinding{
			{Severity: "warning", Category: "test"},
		},
	}
	engine.checker = checker

	ctx := context.Background()
	eventBus.Emit(ctx, agent.AgentEvent{
		SessionID: "web-chat",
		Type:      agent.EventToolCalled,
		Summary:   "Test",
		Timestamp: time.Now(),
	})

	// First call should succeed
	findings, err := engine.ReviewOnce(ctx)
	if err != nil {
		t.Fatalf("first ReviewOnce failed: %v", err)
	}
	if len(findings) == 0 {
		t.Error("expected findings from first review")
	}

	// Manually update cost to exceed cap
	engine.mu.Lock()
	engine.todayCost = 1.01
	engine.mu.Unlock()

	// Second call should skip due to cost cap
	findings, err = engine.ReviewOnce(ctx)
	if err != nil {
		t.Fatalf("second ReviewOnce failed: %v", err)
	}
	if len(findings) != 0 {
		t.Error("expected no findings when cost cap exceeded")
	}
}

func TestCostResetAtMidnight(t *testing.T) {
	cfg := DefaultReviewConfig()
	cfg.APIKey = "test-key"

	eventBus := agent.NewMemEventBus(200)
	logger := slog.Default()

	engine := NewReviewEngine(cfg, eventBus, logger)

	// Set lastReset to yesterday
	engine.mu.Lock()
	engine.lastReset = time.Now().Add(-24 * time.Hour)
	engine.todayCost = 1.99
	engine.mu.Unlock()

	// Mock checker
	checker := &MockChecker{
		findings: []ReviewFinding{},
	}
	engine.checker = checker

	ctx := context.Background()
	eventBus.Emit(ctx, agent.AgentEvent{
		SessionID: "web-chat",
		Type:      agent.EventToolCalled,
		Summary:   "Test",
		Timestamp: time.Now(),
	})

	engine.ReviewOnce(ctx)

	// Check that cost was reset
	engine.mu.Lock()
	if engine.todayCost >= 1.99 {
		t.Errorf("expected cost to reset at midnight, got %f", engine.todayCost)
	}
	engine.mu.Unlock()
}

func TestMeetsSeverity(t *testing.T) {
	tests := []struct {
		severity    string
		minSeverity string
		expected    bool
	}{
		{"critical", "warning", true},
		{"warning", "warning", true},
		{"info", "warning", false},
		{"critical", "info", true},
		{"info", "info", true},
		{"info", "critical", false},
		{"unknown", "warning", true}, // unknown severities default to true
		{"warning", "unknown", true}, // unknown severities default to true
	}

	for _, tt := range tests {
		result := meetsSeverity(tt.severity, tt.minSeverity)
		if result != tt.expected {
			t.Errorf("meetsSeverity(%q, %q) = %v, expected %v", tt.severity, tt.minSeverity, result, tt.expected)
		}
	}
}

func TestReviewEngineStartStop(t *testing.T) {
	cfg := DefaultReviewConfig()
	cfg.ReviewInterval = 10 * time.Millisecond
	cfg.APIKey = "test-key"

	eventBus := agent.NewMemEventBus(200)
	logger := slog.Default()

	engine := NewReviewEngine(cfg, eventBus, logger)

	engine.Start()

	// Give it a moment to start
	time.Sleep(20 * time.Millisecond)

	engine.mu.Lock()
	if !engine.running {
		t.Error("expected engine to be running")
	}
	engine.mu.Unlock()

	engine.Stop()

	// Give it a moment to stop
	time.Sleep(20 * time.Millisecond)

	engine.mu.Lock()
	if engine.running {
		t.Error("expected engine to be stopped")
	}
	engine.mu.Unlock()
}

// MockChecker is a test double for Checker
type MockChecker struct {
	findings []ReviewFinding
	err      error
}

func (m *MockChecker) Check(ctx context.Context, chunk ReviewChunk) ([]ReviewFinding, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.findings, nil
}
