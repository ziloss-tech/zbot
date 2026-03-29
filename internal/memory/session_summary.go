// Package memory — Session Continuity (working memory across sessions).
// Phase 7 of the Memory Overhaul.
//
// End-of-session: extracts topics discussed, decisions made, open items.
// Start-of-session: loads recent session summaries so ZBOT picks up where
// you left off without re-explaining context.
//
// Session summaries are stored as ThoughtPackages with Priority=0 (always injected).
package memory

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ziloss-tech/zbot/internal/agent"
)

// SessionSummary captures what happened in a conversation session.
type SessionSummary struct {
	SessionID  string    `json:"session_id"`
	Topics     []string  `json:"topics"`
	Decisions  []string  `json:"decisions"`
	OpenItems  []string  `json:"open_items"`
	RawSummary string    `json:"raw_summary"`
	CreatedAt  time.Time `json:"created_at"`
}

// SessionSummarizer extracts and saves session summaries.
type SessionSummarizer struct {
	pkgStore agent.PackageStore
	llm      agent.LLMClient
	logger   *slog.Logger
}

// NewSessionSummarizer creates a session summarizer.
func NewSessionSummarizer(pkgStore agent.PackageStore, llm agent.LLMClient, logger *slog.Logger) *SessionSummarizer {
	return &SessionSummarizer{pkgStore: pkgStore, llm: llm, logger: logger}
}

// SummarizeSession extracts a summary from conversation history and saves it
// as a ThoughtPackage. Called when a session ends (idle timeout or explicit close).
func (ss *SessionSummarizer) SummarizeSession(ctx context.Context, sessionID string, messages []agent.Message) error {
	if len(messages) < 4 {
		ss.logger.Debug("session too short to summarize", "session", sessionID, "messages", len(messages))
		return nil // not enough conversation to summarize
	}

	// Build conversation text for the LLM
	var sb strings.Builder
	for _, m := range messages {
		if m.Role == agent.RoleUser || m.Role == agent.RoleAssistant {
			role := "User"
			if m.Role == agent.RoleAssistant {
				role = "ZBOT"
			}
			content := m.Content
			if len(content) > 300 {
				content = content[:300] + "..."
			}
			sb.WriteString(fmt.Sprintf("%s: %s\n", role, content))
		}
	}

	prompt := fmt.Sprintf(`Summarize this conversation session concisely. Extract:
1. Topics discussed (brief list)
2. Decisions made or conclusions reached
3. Open items or next steps

Keep the summary under 200 words. Be specific — include project names, file names, numbers.
Output ONLY the summary text, no JSON, no markdown headers.

Conversation:
%s`, sb.String())

	resp, err := ss.llm.Complete(ctx, []agent.Message{
		{Role: agent.RoleUser, Content: prompt},
	}, nil)
	if err != nil {
		return fmt.Errorf("session summary LLM call: %w", err)
	}

	summary := strings.TrimSpace(resp.Content)
	if len(summary) < 20 {
		return fmt.Errorf("session summary too short (%d chars)", len(summary))
	}

	// Save as a ThoughtPackage with Priority=0 (always injected)
	pkg := agent.ThoughtPackage{
		ID:         "pkg-session-" + sessionID,
		Label:      "sessions/latest",
		Keywords:   []string{"session", "context", "where", "were", "last", "previous", "continue"},
		Content:    fmt.Sprintf("Session %s (%s):\n%s", sessionID, time.Now().Format("2006-01-02 15:04"), summary),
		TokenCount: estimateTokens(summary),
		MemoryIDs:  []string{sessionID},
		Priority:   agent.PackageAlways,
		Freshness:  time.Now(),
		Version:    1,
	}

	// Check if previous session package exists — increment version
	existing, _ := ss.pkgStore.GetPackage(ctx, pkg.ID)
	if existing != nil {
		pkg.Version = existing.Version + 1
	}

	if err := ss.pkgStore.SavePackage(ctx, pkg); err != nil {
		return fmt.Errorf("session summary save: %w", err)
	}

	ss.logger.Info("session summary saved",
		"session", sessionID,
		"tokens", pkg.TokenCount,
		"summary_len", len(summary),
	)
	return nil
}

// CleanOldSessions removes session summary packages older than maxAge.
// Keeps the most recent `keep` summaries regardless of age.
func (ss *SessionSummarizer) CleanOldSessions(ctx context.Context, maxAge time.Duration, keep int) error {
	pkgs, err := ss.pkgStore.ListPackages(ctx)
	if err != nil {
		return err
	}

	var sessionPkgs []agent.ThoughtPackage
	for _, p := range pkgs {
		if p.Label == "sessions/latest" {
			sessionPkgs = append(sessionPkgs, p)
		}
	}

	// Keep the most recent `keep` sessions
	if len(sessionPkgs) <= keep {
		return nil
	}

	// Session packages are ordered by freshness DESC from ListPackages
	for i := keep; i < len(sessionPkgs); i++ {
		if time.Since(sessionPkgs[i].Freshness) > maxAge {
			if err := ss.pkgStore.DeletePackage(ctx, sessionPkgs[i].ID); err != nil {
				ss.logger.Warn("failed to clean old session", "id", sessionPkgs[i].ID, "err", err)
			}
		}
	}
	return nil
}
