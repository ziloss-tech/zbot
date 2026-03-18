// Package agent defines the core domain interfaces (ports) for ZBOT.
// All adapters implement these interfaces. The core agent loop only
// depends on these — never on concrete implementations.
package agent

import (
	"context"
	"fmt"
	"time"
)

// ─── DOMAIN TYPES ────────────────────────────────────────────────────────────

// Message represents a single message in a conversation.
type Message struct {
	ID         string
	SessionID  string
	Role       Role
	Content    string
	Images     []ImageAttachment
	ToolCalls  []ToolCall // populated on assistant messages that request tool use
	ToolCallID string     // populated on tool-result messages (links to ToolCall.ID)
	IsError    bool       // true if this tool result represents an error
	CreatedAt  time.Time
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
	OutputRef   string    // reference to output data in store
	OutputFiles []string  // Sprint 13: files created during this task (relative paths)
	Error       string
	CreatedAt   time.Time
	UpdatedAt   time.Time
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

// ─── MODEL TIER ─────────────────────────────────────────────────────────────

// ModelTier represents a cost/capability tier for model selection.
// The router maps these to concrete model identifiers.
type ModelTier string

const (
	ModelTierHaiku  ModelTier = "haiku"  // Bulk extraction, lightweight classification
	ModelTierSonnet ModelTier = "sonnet" // Default brain — planning, execution, self-critique
	ModelTierOpus   ModelTier = "opus"   // Complex reasoning escalation
	ModelTierAuto   ModelTier = "auto"   // Let the router decide based on content
)

// ─── PORTS (INTERFACES) ──────────────────────────────────────────────────────

// LLMClient is the port for any AI model provider.
// v2: Single-brain architecture — one interface replaces the old
// PlannerClient + ExecutorClient + CriticClient split.
// Adapters: Claude (Anthropic), OpenAI-compatible.
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
	Content      string
	ToolCalls    []ToolCall
	StopReason   string
	Model        string // actual model used (may differ from default due to routing)
	InputTokens  int
	OutputTokens int
	CacheRead    int // prompt caching hits (v2)
	CacheWrite   int // prompt caching writes (v2)
}

// ToolDefinition describes a tool to the LLM.
type ToolDefinition struct {
	Name        string
	Description string
	InputSchema map[string]any // JSON Schema
}

// MemoryStore is the port for persistent semantic memory.
// v2: Extended with daily notes, diversity re-ranking, and context flush.
// Adapter: pgvector on GCP Cloud SQL (reuses existing Vertex AI infra).
type MemoryStore interface {
	// Save persists a fact to long-term memory.
	Save(ctx context.Context, fact Fact) error

	// Search retrieves facts semantically relevant to query.
	// Uses hybrid BM25 + vector scoring with time decay.
	// v2: Results are diversity-reranked (threshold 0.92) to avoid near-duplicates.
	Search(ctx context.Context, query string, limit int) ([]Fact, error)

	// Delete removes a fact by ID.
	Delete(ctx context.Context, id string) error
}

// MemoryFlusher is an optional extension for stores that support context window flush.
// v2: Extracts critical facts before context compaction to prevent information loss.
type MemoryFlusher interface {
	// FlushContext extracts and saves critical facts from a conversation
	// before the context window is compacted.
	FlushContext(ctx context.Context, conversation []Message) error

	// WriteDailyNote appends an entry to today's daily notes file.
	WriteDailyNote(ctx context.Context, entry string) error
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

	// SetTaskOutputFiles records files created during a task. Sprint 13.
	SetTaskOutputFiles(ctx context.Context, taskID string, files []string) error
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
// v2: Extended to support full CRUD for credentialed site research.
// Adapters: macOS Keychain (default), GCP Secret Manager, env vars.
type SecretsManager interface {
	// Get retrieves the latest version of a secret by name.
	Get(ctx context.Context, name string) (string, error)

	// Store saves a credential. Overwrites if key exists.
	// Optional — adapters that don't support writes return ErrNotSupported.
	Store(ctx context.Context, key string, value []byte) error

	// Delete removes a credential by key.
	// Optional — adapters that don't support deletes return ErrNotSupported.
	Delete(ctx context.Context, key string) error

	// List returns all keys matching the prefix.
	// Optional — adapters that don't support listing return ErrNotSupported.
	List(ctx context.Context, prefix string) ([]string, error)
}

// ErrNotSupported is returned by SecretsManager adapters that don't support write operations.
var ErrNotSupported = fmt.Errorf("operation not supported by this secrets backend")

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

// ─── EVENT BUS ──────────────────────────────────────────────────────────────

// EventType classifies agent events for the Thalamus oversight engine.
type EventType string

const (
	EventTurnStart     EventType = "turn_start"
	EventMemoryLoaded  EventType = "memory_loaded"
	EventToolCalled    EventType = "tool_called"
	EventToolResult    EventType = "tool_result"
	EventToolError     EventType = "tool_error"
	EventLLMCall       EventType = "llm_call"
	EventLLMResult     EventType = "llm_result"
	EventTurnComplete  EventType = "turn_complete"
	EventCostUpdate    EventType = "cost_update"
	EventFileRead      EventType = "file_read"
	EventFileWrite     EventType = "file_write"
	EventWebSearch     EventType = "web_search"
	EventFetchURL      EventType = "fetch_url"
	EventConfirmNeeded EventType = "confirm_needed"
	EventSecurityFlag  EventType = "security_flag"
)

// AgentEvent is a structured event emitted by Cortex as it works.
// Thalamus reads these — NOT raw LLM tokens — to observe at low cost.
// Each event is ~50-100 tokens of metadata, not the full context.
type AgentEvent struct {
	ID        string            `json:"id"`
	SessionID string            `json:"session_id"`
	Type      EventType         `json:"type"`
	Summary   string            `json:"summary"`     // human-readable one-liner
	Detail    map[string]any    `json:"detail,omitempty"` // structured metadata
	Timestamp time.Time         `json:"timestamp"`
}

// EventBus is the port for the agent event stream.
// Cortex emits events; Thalamus and the UI consume them.
// Implementations: in-memory channel bus (v0.1), Redis pub/sub (future).
type EventBus interface {
	// Emit publishes an event to all subscribers.
	Emit(ctx context.Context, event AgentEvent)

	// Subscribe returns a channel that receives events for a given session.
	// The caller must close the subscription when done via Unsubscribe.
	Subscribe(sessionID string) <-chan AgentEvent

	// Unsubscribe removes a subscription.
	Unsubscribe(sessionID string, ch <-chan AgentEvent)

	// Recent returns the last N events for a session (for late-joining Thalamus).
	Recent(sessionID string, n int) []AgentEvent
}
