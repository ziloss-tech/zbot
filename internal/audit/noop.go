// Package audit provides audit logging adapters.
// Sprint 1: No-op logger. Sprint 2: Postgres-backed audit log.
package audit

import (
	"context"
	"log/slog"

	"github.com/jeremylerwick-max/zbot/internal/agent"
)

// NoopLogger logs audit events to slog but doesn't persist them.
// Good enough for Sprint 1. Sprint 2 will add Postgres persistence.
type NoopLogger struct {
	logger *slog.Logger
}

// NewNoopLogger creates a no-op audit logger that logs to slog.
func NewNoopLogger(logger *slog.Logger) *NoopLogger {
	return &NoopLogger{logger: logger}
}

func (n *NoopLogger) LogToolCall(_ context.Context, sessionID, toolName string, input map[string]any, result *agent.ToolResult, durationMs int64) {
	isErr := false
	if result != nil {
		isErr = result.IsError
	}
	n.logger.Debug("tool call",
		"session", sessionID,
		"tool", toolName,
		"duration_ms", durationMs,
		"is_error", isErr,
	)
}

func (n *NoopLogger) LogModelCall(_ context.Context, sessionID, model string, inputTokens, outputTokens int, durationMs int64) {
	n.logger.Debug("model call",
		"session", sessionID,
		"model", model,
		"input_tokens", inputTokens,
		"output_tokens", outputTokens,
		"duration_ms", durationMs,
	)
}

func (n *NoopLogger) LogWorkflowEvent(_ context.Context, workflowID, taskID, event, detail string) {
	n.logger.Debug("workflow event",
		"workflow", workflowID,
		"task", taskID,
		"event", event,
		"detail", detail,
	)
}

// Ensure NoopLogger implements the port.
var _ agent.AuditLogger = (*NoopLogger)(nil)
