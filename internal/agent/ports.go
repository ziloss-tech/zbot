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
	ID                string
	SessionID         string
	Role              Role
	Content           string
	Thinking          string // extended thinking content (assistant messages only)
	ThinkingSignature string // signature for echoing thinking blocks in multi-turn
	Images            []ImageAttachment
	ToolCalls         []ToolCall // populated on assistant messages that request tool use
	ToolCallID        string     // populated on tool-result messages (links to ToolCall.ID)
	IsError           bool       // true if this tool result represents an error
	CreatedAt         time.Time
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
	ModelTierCheap  ModelTier = "cheap"  // DeepSeek V3.2: Frontal Lobe, Thalamus, Hypothalamus ($0.14/$0.28 per M)
	ModelTierHaiku  ModelTier = "haiku"  // Anthropic Haiku: fallback cheap model ($0.25/$1.25 per M)
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
	Content           string
	Thinking          string // extended thinking content (if enabled)
	ThinkingSignature string // signature for echoing thinking in multi-turn
	ToolCalls         []ToolCall
	StopReason        string
	Model             string // actual model used (may differ from default due to routing)
	InputTokens       int
	OutputTokens      int
	CacheRead         int // prompt caching hits (v2)
	CacheWrite        int // prompt caching writes (v2)
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

// ─── THOUGHT PACKAGES (Memory Overhaul Phase 2) ─────────────────────────────

// PackagePriority determines when a ThoughtPackage is injected.
type PackagePriority int

const (
	PackageAlways   PackagePriority = 0 // identity, instructions, current priorities
	PackageAuto     PackagePriority = 1 // auto-matched by keywords + plan context
	PackageOnDemand PackagePriority = 2 // only when search_memory tool is called
)

// ThoughtPackage is a compressed, pre-organized block of memories.
// Instead of searching 10K raw facts at runtime, the agent matches
// against ~50-100 packages using keyword match (< 1ms, zero LLM cost).
// Built nightly by the batch builder (Phase 3) from raw facts.
type ThoughtPackage struct {
	ID         string          `json:"id"`
	Label      string          `json:"label"`       // e.g. "ghl/esler-cst", "projects/zbot"
	Keywords   []string        `json:"keywords"`    // fast matching — no LLM needed
	Embedding  []float32       `json:"-"`           // fallback similarity match
	Content    string          `json:"content"`     // compressed, ready to inject
	TokenCount int             `json:"token_count"` // pre-counted for budget
	MemoryIDs  []string        `json:"memory_ids"`  // source facts
	Priority   PackagePriority `json:"priority"`
	Freshness  time.Time       `json:"freshness"`   // last refreshed
	Version    int             `json:"version"`     // incremented on rebuild
	CreatedAt  time.Time       `json:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
}

// PackageMatch is a scored result from MatchPackages.
type PackageMatch struct {
	Package ThoughtPackage
	Score   float32 // 0.0-1.0, keyword + embedding fusion
	Method  string  // "keyword", "embedding", or "priority"
}

// PackageStore is the port for Thought Package persistence and retrieval.
// Phase 2: CRUD + keyword matching. Phase 4: wired into agent runtime.
type PackageStore interface {
	// SavePackage creates or updates a thought package.
	SavePackage(ctx context.Context, pkg ThoughtPackage) error

	// GetPackage retrieves a single package by ID.
	GetPackage(ctx context.Context, id string) (*ThoughtPackage, error)

	// ListPackages returns all packages, ordered by priority then freshness.
	ListPackages(ctx context.Context) ([]ThoughtPackage, error)

	// DeletePackage removes a package by ID.
	DeletePackage(ctx context.Context, id string) error

	// MatchPackages returns packages relevant to a query, scored and sorted.
	// Uses keyword match first (fast), falls back to embedding similarity.
	// tokenBudget limits total injected tokens (0 = no limit).
	MatchPackages(ctx context.Context, query string, tokenBudget int) ([]PackageMatch, error)

	// AlwaysPackages returns all Priority=0 packages (always injected).
	AlwaysPackages(ctx context.Context) ([]ThoughtPackage, error)
}

// ─── EXPERIENTIAL LEARNING (Memory Overhaul Phase 5) ────────────────────────

// Lesson captures a mistake→correction pattern from Thalamus rejection.
// When Thalamus rejects a response and revision succeeds, the pattern is
// saved so ZBOT doesn't repeat the same mistake.
type Lesson struct {
	ID           string
	Mistake      string    // what was wrong
	Correction   string    // what fixed it
	Context      string    // user query that triggered this
	SessionID    string
	TriggerCount int       // how many times this lesson matched
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// LessonStore is the port for experiential learning persistence.
type LessonStore interface {
	SaveLesson(ctx context.Context, lesson Lesson) error
	SearchLessons(ctx context.Context, query string, limit int) ([]Lesson, error)
	IncrementTrigger(ctx context.Context, id string) error
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

// ModelRouter selects the optimal LLM based on task classification and benchmarks.
// Adapter: internal/router/ (benchmark-based model selection).
type ModelRouter interface {
	// ClassifyTask maps a natural language description to a task category string.
	ClassifyTask(description string) string

	// BestModel returns the recommended model ID for a task category.
	// preferQuality=true picks highest score; false picks best cost-efficiency.
	BestModel(category string, preferQuality bool) *ModelRecommendation
}

// ModelRecommendation is the Router's output.
type ModelRecommendation struct {
	ModelID        string
	ModelName      string
	TaskScore      float64
	CostEfficiency float64
	Reason         string
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
	EventPlanStart     EventType = "plan_start"
	EventPlanComplete  EventType = "plan_complete"
	EventVerifyStart   EventType = "verify_start"
	EventVerifyComplete EventType = "verify_complete"
	EventMemoryEnrich    EventType = "memory_enrich"
	EventStallDetected   EventType = "stall_detected"
	EventStallRecovered  EventType = "stall_recovered"
	EventThinking        EventType = "thinking"

	// Crawl event types — emitted by internal/crawler/ module
	EventCrawlScreenshot EventType = "crawl_screenshot"
	EventCrawlAction     EventType = "crawl_action"
	EventCrawlStatus     EventType = "crawl_status"
	EventCrawlError      EventType = "crawl_error"
	EventCrawlGridUpdate EventType = "crawl_grid_update"
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
