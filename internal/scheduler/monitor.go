package scheduler

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/zbot-ai/zbot/internal/agent"
)

// URLMonitor watches a URL for content changes.
// Fires the handler when content differs from the last-seen hash.
type URLMonitor struct {
	url       string
	interval  time.Duration // how often to check (default 60 min)
	lastHash  string
	sessionID string
	handler   func(ctx context.Context, sessionID, change string)
	llm       agent.LLMClient
	logger    *slog.Logger
}

// NewURLMonitor creates a new URL monitor.
func NewURLMonitor(
	url, sessionID string,
	interval time.Duration,
	handler func(ctx context.Context, sessionID, change string),
	llm agent.LLMClient,
	logger *slog.Logger,
) *URLMonitor {
	if interval <= 0 {
		interval = 60 * time.Minute
	}
	return &URLMonitor{
		url:       url,
		interval:  interval,
		sessionID: sessionID,
		handler:   handler,
		llm:       llm,
		logger:    logger,
	}
}

// Start begins the monitoring loop. Blocks until ctx is cancelled.
func (m *URLMonitor) Start(ctx context.Context) {
	m.logger.Info("URL monitor started", "url", m.url, "interval", m.interval)

	// Do an initial fetch to set the baseline hash.
	m.check(ctx, true)

	for {
		select {
		case <-ctx.Done():
			m.logger.Info("URL monitor stopped", "url", m.url)
			return
		case <-time.After(m.interval):
			m.check(ctx, false)
		}
	}
}

// check fetches the URL and compares the hash to the last seen value.
func (m *URLMonitor) check(ctx context.Context, initial bool) {
	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, "GET", m.url, nil)
	if err != nil {
		m.logger.Error("URL monitor: build request failed", "url", m.url, "err", err)
		return
	}
	req.Header.Set("User-Agent", "ZBOT-Monitor/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		m.logger.Error("URL monitor: fetch failed", "url", m.url, "err", err)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB limit
	if err != nil {
		m.logger.Error("URL monitor: read body failed", "url", m.url, "err", err)
		return
	}

	hash := fmt.Sprintf("%x", sha256.Sum256(body))

	if initial {
		m.lastHash = hash
		m.logger.Info("URL monitor: baseline set", "url", m.url, "hash", hash[:16])
		return
	}

	if hash == m.lastHash {
		m.logger.Debug("URL monitor: no change", "url", m.url)
		return
	}

	m.logger.Info("URL monitor: change detected!", "url", m.url, "old_hash", m.lastHash[:16], "new_hash", hash[:16])

	// Use Claude to summarize the change.
	summary := fmt.Sprintf("The content at %s has changed.", m.url)
	if m.llm != nil {
		prompt := fmt.Sprintf("A web page has been updated. URL: %s\n\nNew content (first 2000 chars):\n%s\n\nSummarize what this page contains in 2-3 sentences.",
			m.url, truncate(string(body), 2000))
		msgs := []agent.Message{{Role: agent.RoleUser, Content: prompt}}
		if result, err := m.llm.Complete(ctx, msgs, nil); err == nil {
			summary = fmt.Sprintf("🔔 Change detected at %s\n\n%s", m.url, result.Content)
		}
	}

	m.lastHash = hash
	go m.handler(ctx, m.sessionID, summary)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
