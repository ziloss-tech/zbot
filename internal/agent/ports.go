// Package agent defines the core domain interfaces (ports) for ZBOT.
// All adapters implement these interfaces. The core agent loop only
// depends on these — never on concrete implementations.
package agent

import (
	"context"
	"time"
)

// ─── DOMAIN TYPES ────────────────────────────────────────────────────────────

// Message represents a single message in a conversation.
type Message struct {
	ID        string
	SessionID string
	Role      Role
	Content   string
	Images    []ImageAttachment
	CreatedAt time.Time
}

// Role distinguishes user, assistant, and system messages.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleSystem    Role = "system"
	RoleTool      Role = "tool"
)

// ImageAttachment holds image data for multimodal messages.
type ImageAttachment struct {
	Data      []byte
	MediaType string // "image/jpeg", "image/png", etc.
}

// Fact is a unit of persistent memory.
type Fact struct {
	ID        string
	Content   string
	Source    string    // e.g. "conversation", "research", "user-explicit"
	Tags      []string
	CreatedAt time.Time
	Score     float32   // set during retrieval, not storage
}

// Task is a unit of work in a workflow.
type Task struct {
	ID         string
	WorkflowID string
	Step       int
	Name       string
	Instruction string
	Status     TaskStatus
	DependsOn  []string  // task IDs that must complete first
	InputRef   string    // reference to input data in store
	OutputRef  string    // reference to output data in store
	Error      string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// TaskStatus represents the lifecycle state of a task.
type TaskStatus string

const (
	TaskPending  TaskStatus = "pending"
	TaskRunning  TaskStatus = "running"
	TaskDone     TaskStatus = "done"
	TaskFailed   TaskStatus = "failed"
	TaskCanceled TaskStatus = "canceled"
)

// ToolCall represents a request from the model to invoke a tool.
type ToolCall struct {
	ID    string
	Name  string
	Input map[string]any
}

// ToolResult holds the output of a tool execution.
type ToolResult struct {
	ToolCallID string
	Content    string
	IsError    bool
}

// ─── PORTS (INTERFACES) ──────────────────────────────────────────────────────

// LLMClient is the port for any AI model provider.
// Adapters: Claude (Anthropic), GPT-4 (OpenAI).
type LLMClient interface {
	// Complete sends messages to the model and returns the response.
	// tools is nil for plain completion, non-nil to enable tool use.
	Complete(ctx context.Context, messages []Message, tools []ToolDefinition) (*CompletionResult, error)

	// CompleteStream streams the response token by token.
	CompleteStream(ctx context.Context, messages []Message, tools []ToolDefinition, out chan<- string) error

	// ModelName returns the active model identifier.
	ModelName() string
}

// CompletionResult holds the model's response.
type CompletionResult struct {
	Content   string
	ToolCalls []ToolCall
	StopReason string
	InputTokens  int
	OutputTokens int
}

// ToolDefinition describes a tool to the LLM.
type ToolDefinition struct {
	Name        string
	Description string
	InputSchema map[string]any // JSON Schema
}

// MemoryStore is the port for persistent semantic memory.
// Adapter: pgvector on GCP Cloud SQL (reuses existing Vertex AI infra).
type MemoryStore interface {
	// Save persists a fact to long-term memory.
	Save(ctx context.Context, fact Fact) error

	// Search retrieves facts semantically relevant to query.
	// Uses hybrid BM25 + vector scoring with time decay.
	Search(ctx context.Context, query string, limit int) ([]Fact, error)

	// Delete removes a fact by ID.
	Delete(ctx context.Context, id string) error
}

// WorkflowStore is the port for task graph persistence.
// Adapter: Postgres (same GCP Cloud SQL instance).
type WorkflowStore interface {
	// CreateWorkflow creates a new workflow and its task graph.
	CreateWorkflow(ctx context.Context, tasks []Task) (workflowID string, err error)

	// ClaimNextTask atomically claims the next runnable task.
	// Uses SELECT FOR UPDATE SKIP LOCKED — no external queue needed.
	ClaimNextTask(ctx context.Context, workerID string) (*Task, error)

	// CompleteTask marks a task done and stores its output reference.
	CompleteTask(ctx context.Context, taskID, outputRef string) error

	// FailTask marks a task failed with an error message.
	FailTask(ctx context.Context, taskID, errMsg string) error

	// GetWorkflowStatus returns all tasks for a workflow.
	GetWorkflowStatus(ctx context.Context, workflowID string) ([]Task, error)

	// CancelWorkflow cancels all pending tasks in a workflow.
	CancelWorkflow(ctx context.Context, workflowID string) error
}

// DataStore is the port for structured output storage between workflow steps.
type DataStore interface {
	// Put stores arbitrary JSON data, returns a reference key.
	Put(ctx context.Context, data any) (ref string, err error)

	// Get retrieves data by reference key.
	Get(ctx context.Context, ref string, dest any) error

	// Delete removes data by reference key.
	Delete(ctx context.Context, ref string) error
}

// Tool is the port that every agent tool implements.
type Tool interface {
	// Name returns the tool's identifier (must match ToolDefinition.Name).
	Name() string

	// Definition returns the schema exposed to the LLM.
	Definition() ToolDefinition

	// Execute runs the tool with the given input.
	// ctx carries cancellation — all tools must respect it.
	Execute(ctx context.Context, input map[string]any) (*ToolResult, error)
}

// Gateway is the port for inbound/outbound messaging channels.
// Adapters: Telegram, WebUI.
type Gateway interface {
	// Start begins listening for inbound messages.
	Start(ctx context.Context) error

	// Send delivers a response to the originating session.
	Send(ctx context.Context, sessionID, content string) error

	// SendFile delivers a file (document/image) to the originating session.
	SendFile(ctx context.Context, sessionID, filename string, data []byte) error
}

// SecretsManager is the port for retrieving credentials at runtime.
// Adapter: GCP Secret Manager.
type SecretsManager interface {
	// Get retrieves the latest version of a secret by name.
	Get(ctx context.Context, name string) (string, error)
}

// AuditLogger is the port for structured audit logging.
// Every tool call, model call, and workflow event is logged here.
type AuditLogger interface {
	// LogToolCall records a tool invocation and its result.
	LogToolCall(ctx context.Context, sessionID, toolName string, input map[string]any, result *ToolResult, durationMs int64)

	// LogModelCall records a model call with token usage.
	LogModelCall(ctx context.Context, sessionID, model string, inputTokens, outputTokens int, durationMs int64)

	// LogWorkflowEvent records a workflow lifecycle event.
	LogWorkflowEvent(ctx context.Context, workflowID, taskID, event, detail string)
}
