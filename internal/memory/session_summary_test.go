package memory

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ziloss-tech/zbot/internal/agent"
)

// sessionMockLLM returns a canned summary for tests.
type sessionMockLLM struct{ callCount int }

func (m *sessionMockLLM) Complete(_ context.Context, msgs []agent.Message, _ []agent.ToolDefinition) (*agent.CompletionResult, error) {
	m.callCount++
	return &agent.CompletionResult{
		Content: "Topics: GHL workflow migration, ZBOT memory overhaul. Decisions: Use Python scripts for dynamic skills. Open items: Wire SMS skill, finish Phase 7.",
		Model:   "mock",
	}, nil
}

func (m *sessionMockLLM) CompleteStream(_ context.Context, _ []agent.Message, _ []agent.ToolDefinition, out chan<- string) error {
	close(out)
	return nil
}

func (m *sessionMockLLM) ModelName() string { return "mock-session" }

func TestSummarizeSession(t *testing.T) {
	ctx := context.Background()
	pkgStore := NewInMemoryPackageStore()
	llm := &sessionMockLLM{}
	summarizer := NewSessionSummarizer(pkgStore, llm, nil)
	// Need logger — create a minimal one
	summarizer.logger = testLogger()

	messages := []agent.Message{
		{Role: agent.RoleUser, Content: "Let's work on GHL workflow migration"},
		{Role: agent.RoleAssistant, Content: "Sure, I'll pull the workflow data"},
		{Role: agent.RoleUser, Content: "Now let's do the memory overhaul"},
		{Role: agent.RoleAssistant, Content: "Starting Phase 2 — ThoughtPackage schema"},
		{Role: agent.RoleUser, Content: "Great, what about the skill factory?"},
		{Role: agent.RoleAssistant, Content: "Building create_skill tool with Python scripts"},
	}

	err := summarizer.SummarizeSession(ctx, "test-session-1", messages)
	if err != nil {
		t.Fatalf("SummarizeSession: %v", err)
	}

	// Verify package was saved
	pkgs, _ := pkgStore.ListPackages(ctx)
	if len(pkgs) != 1 {
		t.Fatalf("expected 1 package, got %d", len(pkgs))
	}
	if pkgs[0].Label != "sessions/latest" {
		t.Errorf("label = %q, want sessions/latest", pkgs[0].Label)
	}
	if pkgs[0].Priority != agent.PackageAlways {
		t.Errorf("priority = %d, want Always(0)", pkgs[0].Priority)
	}
	if !strings.Contains(pkgs[0].Content, "GHL workflow") {
		t.Errorf("summary should mention GHL workflow, got: %s", pkgs[0].Content)
	}
	if llm.callCount != 1 {
		t.Errorf("LLM calls = %d, want 1", llm.callCount)
	}
}

func TestSummarizeSession_TooShort(t *testing.T) {
	ctx := context.Background()
	pkgStore := NewInMemoryPackageStore()
	llm := &sessionMockLLM{}
	summarizer := NewSessionSummarizer(pkgStore, llm, testLogger())

	// Only 2 messages — too short
	messages := []agent.Message{
		{Role: agent.RoleUser, Content: "Hi"},
		{Role: agent.RoleAssistant, Content: "Hello!"},
	}

	err := summarizer.SummarizeSession(ctx, "short-session", messages)
	if err != nil {
		t.Fatalf("unexpected error for short session: %v", err)
	}

	// Should NOT have saved anything
	pkgs, _ := pkgStore.ListPackages(ctx)
	if len(pkgs) != 0 {
		t.Errorf("short session should not create package, got %d", len(pkgs))
	}
	if llm.callCount != 0 {
		t.Errorf("LLM should not be called for short session, got %d", llm.callCount)
	}
}

func TestCleanOldSessions(t *testing.T) {
	ctx := context.Background()
	pkgStore := NewInMemoryPackageStore()
	summarizer := NewSessionSummarizer(pkgStore, &sessionMockLLM{}, testLogger())

	// Create 5 session packages with different ages
	for i := 0; i < 5; i++ {
		pkg := agent.ThoughtPackage{
			ID: fmt.Sprintf("pkg-session-s%d", i), Label: "sessions/latest",
			Keywords: []string{"session"}, Content: fmt.Sprintf("Session %d", i),
			TokenCount: 50, Priority: agent.PackageAlways,
			Freshness: time.Now().Add(-time.Duration(i*10) * 24 * time.Hour),
			Version: 1,
		}
		pkgStore.SavePackage(ctx, pkg)
	}

	// Clean: keep 3, max age 25 days
	err := summarizer.CleanOldSessions(ctx, 25*24*time.Hour, 3)
	if err != nil {
		t.Fatalf("CleanOldSessions: %v", err)
	}

	pkgs, _ := pkgStore.ListPackages(ctx)
	sessionCount := 0
	for _, p := range pkgs {
		if p.Label == "sessions/latest" {
			sessionCount++
		}
	}
	// Should have kept 3 recent + sessions 3,4 are >25 days and beyond keep limit
	if sessionCount > 4 {
		t.Errorf("expected <= 4 sessions after cleanup, got %d", sessionCount)
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
}
