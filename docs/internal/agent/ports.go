// Package agent defines the core domain interfaces (ports) for ZBOT.
// This is a STUB of the real ports.go — only includes types needed by the crawler module.
package agent

import (
	"context"
	"time"
)

// EventType classifies agent events.
type EventType string

const (
	EventTurnStart      EventType = "turn_start"
	EventToolCalled     EventType = "tool_called"
	EventToolResult     EventType = "tool_result"
	EventToolError      EventType = "tool_error"
	EventTurnComplete   EventType = "turn_complete"
	EventCostUpdate     EventType = "cost_update"
	EventPlanStart      EventType = "plan_start"
	EventPlanComplete   EventType = "plan_complete"
	EventVerifyStart    EventType = "verify_start"
	EventVerifyComplete EventType = "verify_complete"
)

// AgentEvent is a structured event emitted by Cortex as it works.
type AgentEvent struct {
	ID        string         `json:"id"`
	SessionID string         `json:"session_id"`
	Type      EventType      `json:"type"`
	Summary   string         `json:"summary"`
	Detail    map[string]any `json:"detail,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
}

// EventBus is the port for the agent event stream.
type EventBus interface {
	Emit(ctx context.Context, event AgentEvent)
	Subscribe(sessionID string) <-chan AgentEvent
	Unsubscribe(sessionID string, ch <-chan AgentEvent)
	Recent(sessionID string, n int) []AgentEvent
}

// Tool is the port that every agent tool implements.
type Tool interface {
	Name() string
	Definition() ToolDefinition
	Execute(ctx context.Context, input map[string]any) (*ToolResult, error)
}

// ToolDefinition describes a tool to the LLM.
type ToolDefinition struct {
	Name        string
	Description string
	InputSchema map[string]any
}

// ToolResult holds the output of a tool execution.
type ToolResult struct {
	ToolCallID string
	Content    string
	IsError    bool
}
