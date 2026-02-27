// Package audit implements audit logging for ZBOT.
// PGAuditLogger writes tool calls, model calls, and workflow events
// to Postgres asynchronously via a buffered channel.
package audit

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync/atomic"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jeremylerwick-max/zbot/internal/agent"
)

// auditKind distinguishes the type of audit write.
type auditKind int

const (
	kindToolCall auditKind = iota
	kindModelCall
	kindWorkflowEvent
)

// auditWrite is a single audit entry to write.
type auditWrite struct {
	kind auditKind

	// Tool call fields.
	sessionID string
	toolName  string
	input     map[string]any
	result    *agent.ToolResult
	duration  int64

	// Model call fields.
	model        string
	inputTokens  int
	outputTokens int

	// Workflow event fields.
	workflowID string
	taskID     string
	event      string
	detail     string
}

// PGAuditLogger implements agent.AuditLogger with async Postgres writes.
type PGAuditLogger struct {
	db           *pgxpool.Pool
	writes       chan auditWrite
	logger       *slog.Logger
	droppedTotal int64 // atomic counter for dropped audit events
}

// NewPGAuditLogger creates a Postgres-backed audit logger.
// Call Start() to begin the background writer.
func NewPGAuditLogger(db *pgxpool.Pool, logger *slog.Logger) *PGAuditLogger {
	return &PGAuditLogger{
		db:     db,
		writes: make(chan auditWrite, 256),
		logger: logger,
	}
}

// Start the background writer goroutine. Returns when ctx is cancelled.
func (a *PGAuditLogger) Start(ctx context.Context) {
	go a.run(ctx)
}

func (a *PGAuditLogger) run(ctx context.Context) {
	// Create tables on first start.
	a.ensureTables(ctx)

	for {
		select {
		case <-ctx.Done():
			// Drain remaining writes.
			for {
				select {
				case w := <-a.writes:
					a.write(context.Background(), w)
				default:
					return
				}
			}
		case w := <-a.writes:
			a.write(ctx, w)
		}
	}
}

func (a *PGAuditLogger) write(ctx context.Context, w auditWrite) {
	var err error
	switch w.kind {
	case kindToolCall:
		inputJSON, _ := json.Marshal(w.input)
		output := ""
		isError := false
		if w.result != nil {
			output = w.result.Content
			isError = w.result.IsError
		}
		_, err = a.db.Exec(ctx,
			`INSERT INTO zbot_audit_tool_calls (session_id, tool_name, input, output, is_error, duration_ms)
			 VALUES ($1, $2, $3, $4, $5, $6)`,
			w.sessionID, w.toolName, inputJSON, output, isError, w.duration,
		)

	case kindModelCall:
		_, err = a.db.Exec(ctx,
			`INSERT INTO zbot_audit_model_calls (session_id, model, input_tokens, output_tokens, duration_ms)
			 VALUES ($1, $2, $3, $4, $5)`,
			w.sessionID, w.model, w.inputTokens, w.outputTokens, w.duration,
		)

	case kindWorkflowEvent:
		_, err = a.db.Exec(ctx,
			`INSERT INTO zbot_audit_workflow_events (workflow_id, task_id, event, detail)
			 VALUES ($1, $2, $3, $4)`,
			w.workflowID, w.taskID, w.event, w.detail,
		)
	}

	if err != nil {
		a.logger.Error("audit write failed", "kind", w.kind, "err", err)
	}
}

// LogToolCall records a tool invocation.
func (a *PGAuditLogger) LogToolCall(ctx context.Context, sessionID, toolName string, input map[string]any, result *agent.ToolResult, durationMs int64) {
	a.logger.Info("tool call",
		"session", sessionID,
		"tool", toolName,
		"duration_ms", durationMs,
	)
	select {
	case a.writes <- auditWrite{
		kind:      kindToolCall,
		sessionID: sessionID,
		toolName:  toolName,
		input:     input,
		result:    result,
		duration:  durationMs,
	}:
	default:
		total := atomic.AddInt64(&a.droppedTotal, 1)
		a.logger.Warn("audit event dropped", "component", "audit", "kind", "tool_call", "total_dropped", total)
	}
}

// LogModelCall records a model invocation.
func (a *PGAuditLogger) LogModelCall(ctx context.Context, sessionID, model string, inputTokens, outputTokens int, durationMs int64) {
	a.logger.Info("model call",
		"session", sessionID,
		"model", model,
		"input_tokens", inputTokens,
		"output_tokens", outputTokens,
		"duration_ms", durationMs,
	)
	select {
	case a.writes <- auditWrite{
		kind:         kindModelCall,
		sessionID:    sessionID,
		model:        model,
		inputTokens:  inputTokens,
		outputTokens: outputTokens,
		duration:     durationMs,
	}:
	default:
		total := atomic.AddInt64(&a.droppedTotal, 1)
		a.logger.Warn("audit event dropped", "component", "audit", "kind", "model_call", "total_dropped", total)
	}
}

// LogWorkflowEvent records a workflow lifecycle event.
func (a *PGAuditLogger) LogWorkflowEvent(ctx context.Context, workflowID, taskID, event, detail string) {
	a.logger.Info("workflow event",
		"workflow", workflowID,
		"task", taskID,
		"event", event,
	)
	select {
	case a.writes <- auditWrite{
		kind:       kindWorkflowEvent,
		workflowID: workflowID,
		taskID:     taskID,
		event:      event,
		detail:     detail,
	}:
	default:
		total := atomic.AddInt64(&a.droppedTotal, 1)
		a.logger.Warn("audit event dropped", "component", "audit", "kind", "workflow_event", "total_dropped", total)
	}
}

// ensureTables creates audit tables if they don't exist.
func (a *PGAuditLogger) ensureTables(ctx context.Context) {
	ddl := `
	CREATE TABLE IF NOT EXISTS zbot_audit_tool_calls (
		id           TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
		session_id   TEXT NOT NULL,
		tool_name    TEXT NOT NULL,
		input        JSONB NOT NULL,
		output       TEXT,
		is_error     BOOLEAN NOT NULL DEFAULT FALSE,
		duration_ms  BIGINT NOT NULL,
		created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);
	CREATE TABLE IF NOT EXISTS zbot_audit_model_calls (
		id            TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
		session_id    TEXT NOT NULL,
		model         TEXT NOT NULL,
		input_tokens  INTEGER NOT NULL,
		output_tokens INTEGER NOT NULL,
		duration_ms   BIGINT NOT NULL,
		created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);
	CREATE TABLE IF NOT EXISTS zbot_audit_workflow_events (
		id           TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
		workflow_id  TEXT NOT NULL,
		task_id      TEXT,
		event        TEXT NOT NULL,
		detail       TEXT,
		created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);
	CREATE INDEX IF NOT EXISTS audit_tool_session_idx ON zbot_audit_tool_calls(session_id, created_at DESC);
	CREATE INDEX IF NOT EXISTS audit_model_session_idx ON zbot_audit_model_calls(session_id, created_at DESC);
	CREATE INDEX IF NOT EXISTS audit_workflow_idx ON zbot_audit_workflow_events(workflow_id, created_at DESC);
	`
	if _, err := a.db.Exec(ctx, ddl); err != nil {
		a.logger.Error("audit ensureTables failed", "err", err)
	}
}

// Verify interface compliance.
var _ agent.AuditLogger = (*PGAuditLogger)(nil)
