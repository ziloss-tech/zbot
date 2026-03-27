package audit

import (
	"context"
	"log/slog"
	"testing"

	"github.com/ziloss-tech/zbot/internal/agent"
)

func TestNoopLoggerImplementsInterface(t *testing.T) {
	logger := NewNoopLogger(slog.Default())
	var _ agent.AuditLogger = logger // compile-time check
}

func TestNoopLoggerDoesNotPanic(t *testing.T) {
	logger := NewNoopLogger(slog.Default())
	ctx := context.Background()

	// These should all be no-ops — just verify no panics.
	logger.LogToolCall(ctx, "sess1", "web_search", map[string]any{"q": "test"}, &agent.ToolResult{Content: "ok"}, 100)
	logger.LogModelCall(ctx, "sess1", "claude-sonnet", 500, 200, 1500)
	logger.LogWorkflowEvent(ctx, "wf1", "task1", "started", "running task")
}
