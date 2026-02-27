// Package workflow implements the ZBOT workflow orchestrator.
// Core design: database IS the state. Goroutines are ephemeral workers.
// A workflow survives process restarts because its task graph lives in Postgres.
package workflow

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jeremylerwick-max/zbot/internal/agent"
	"github.com/jeremylerwick-max/zbot/internal/platform"
)

// TaskEventFunc is a callback for task lifecycle events.
// Used by the webui SSE hub to broadcast task status changes.
type TaskEventFunc func(workflowID, taskID, eventType, payload string)

// CriticFunc reviews a completed task's output and returns a JSON verdict.
// The orchestrator calls this after a task completes, before moving on.
// Parameters: ctx, workflowID, taskID, instruction, output.
// Returns: verdict JSON string, corrected instruction (empty if pass), error.
type CriticFunc func(ctx context.Context, workflowID, taskID, instruction, output string) (verdictJSON string, correctedInstruction string, shouldRetry bool, err error)

// InsightExtractor is a lightweight LLM call (Haiku) for extracting saveable facts.
type InsightExtractor interface {
	Complete(ctx context.Context, messages []agent.Message, tools []agent.ToolDefinition) (*agent.CompletionResult, error)
}

// Orchestrator manages workflow lifecycle: decompose → dispatch → collect.
type Orchestrator struct {
	store       agent.WorkflowStore
	dataStore   agent.DataStore
	ag          *agent.Agent
	workerN     int           // number of parallel workers
	pollEvery   time.Duration // how often to check for pending tasks
	logger      *slog.Logger
	onTaskEvent TaskEventFunc // optional callback for SSE broadcasting
	criticFunc  CriticFunc    // optional GPT-4o critic review callback
	retried     map[string]bool // tracks tasks that have been retried (max 1 retry)

	// Sprint 12: Memory auto-save on workflow completion.
	memoryStore      agent.MemoryStore  // for saving extracted insights
	insightExtractor InsightExtractor   // Haiku LLM for cheap insight extraction
}

// NewOrchestrator constructs an Orchestrator.
// workerN controls max parallelism. For a personal agent 3-5 is plenty.
func NewOrchestrator(
	store agent.WorkflowStore,
	dataStore agent.DataStore,
	ag *agent.Agent,
	workerN int,
	logger *slog.Logger,
) *Orchestrator {
	if workerN <= 0 {
		workerN = 3
	}
	return &Orchestrator{
		store:     store,
		dataStore: dataStore,
		ag:        ag,
		workerN:   workerN,
		pollEvery: 2 * time.Second,
		logger:    logger,
		retried:   make(map[string]bool),
	}
}

// SetTaskEventHook registers a callback for task lifecycle events.
// Called when tasks start, complete, or fail — used by webui SSE hub.
func (o *Orchestrator) SetTaskEventHook(fn TaskEventFunc) {
	o.onTaskEvent = fn
}

// SetMemoryAutoSave enables auto-saving of workflow insights to memory.
// extractor should be a cheap/fast model (Haiku) for fact extraction.
func (o *Orchestrator) SetMemoryAutoSave(mem agent.MemoryStore, extractor InsightExtractor) {
	o.memoryStore = mem
	o.insightExtractor = extractor
}

// SetCriticFunc registers the GPT-4o critic review callback.
// Called after each task completes. If the critic returns shouldRetry=true
// and the task hasn't been retried yet, the task is re-run with the corrected instruction.
func (o *Orchestrator) SetCriticFunc(fn CriticFunc) {
	o.criticFunc = fn
}

// publishTaskEvent fires the event hook if set.
func (o *Orchestrator) publishTaskEvent(workflowID, taskID, eventType, payload string) {
	if o.onTaskEvent != nil {
		o.onTaskEvent(workflowID, taskID, eventType, payload)
	}
}

// Submit decomposes a natural-language request into a task graph and persists it.
// Returns the workflow ID. Execution begins asynchronously via Run().
func (o *Orchestrator) Submit(ctx context.Context, sessionID, request string) (string, error) {
	// Ask the agent to decompose the request into discrete steps.
	tasks, err := o.decompose(ctx, sessionID, request)
	if err != nil {
		return "", fmt.Errorf("workflow.Submit decompose: %w", err)
	}

	wfID, err := o.store.CreateWorkflow(ctx, tasks)
	if err != nil {
		return "", fmt.Errorf("workflow.Submit CreateWorkflow: %w", err)
	}

	o.logger.Info("workflow submitted",
		"workflow_id", wfID,
		"tasks", len(tasks),
		"session", sessionID,
	)
	return wfID, nil
}

// Run starts the worker pool. Blocks until ctx is cancelled.
// Call this in a goroutine from main.
func (o *Orchestrator) Run(ctx context.Context) error {
	o.logger.Info("workflow orchestrator starting", "workers", o.workerN)

	var wg sync.WaitGroup
	for i := 0; i < o.workerN; i++ {
		wg.Add(1)
		go func(workerID string) {
			defer wg.Done()
			o.workerLoop(ctx, workerID)
		}(fmt.Sprintf("worker-%d", i))
	}

	wg.Wait()
	o.logger.Info("workflow orchestrator stopped")
	return nil
}

// workerLoop polls for tasks and executes them.
// Each goroutine is an independent worker — they race for tasks via
// SELECT FOR UPDATE SKIP LOCKED so no two workers run the same task.
func (o *Orchestrator) workerLoop(ctx context.Context, workerID string) {
	ticker := time.NewTicker(o.pollEvery)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := o.tryClaimAndRun(ctx, workerID); err != nil {
				o.logger.Error("worker error", "component", "orchestrator", "worker", workerID, "err", err)
			}
		}
	}
}

// tryClaimAndRun claims one pending task and executes it.
// Returns nil if no tasks are available (normal state).
func (o *Orchestrator) tryClaimAndRun(ctx context.Context, workerID string) error {
	task, err := o.store.ClaimNextTask(ctx, workerID)
	if err != nil {
		return fmt.Errorf("ClaimNextTask: %w", err)
	}
	if task == nil {
		return nil // nothing to do
	}

	o.logger.Info("worker claimed task",
		"worker", workerID,
		"task_id", task.ID,
		"task_name", task.Name,
		"workflow", task.WorkflowID,
	)

	// Publish task started event.
	o.publishTaskEvent(task.WorkflowID, task.ID, "status", "running")

	return o.runTask(ctx, workerID, task)
}

// runTask executes a single task in an isolated agent context.
// KEY: Each task gets a FRESH context — it does not see the full workflow history.
// It only receives: its specific instruction + the output of its direct dependency.
func (o *Orchestrator) runTask(ctx context.Context, workerID string, task *agent.Task) error {
	taskStart := time.Now()
	taskCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	// Load upstream output (only the direct dependency, not the whole chain).
	upstreamContent := ""
	if task.InputRef != "" {
		var data string
		if err := o.dataStore.Get(taskCtx, task.InputRef, &data); err != nil {
			o.logger.Warn("could not load upstream output",
				"task", task.ID, "input_ref", task.InputRef, "err", err)
		} else {
			upstreamContent = data
		}
	}

	// Build the scoped instruction for this step only.
	// The worker sees: what to do + what the previous step produced.
	// It does NOT see the full workflow or other tasks.
	instruction := task.Instruction
	if upstreamContent != "" {
		instruction = fmt.Sprintf("%s\n\n## Input from Previous Step\n%s", instruction, upstreamContent)
	}

	input := agent.TurnInput{
		SessionID:  fmt.Sprintf("workflow-%s-task-%s", task.WorkflowID, task.ID),
		WorkflowID: task.WorkflowID,
		TaskID:     task.ID,
		UserMsg: agent.Message{
			Role:    agent.RoleUser,
			Content: instruction,
		},
	}

	result, err := o.ag.Run(taskCtx, input)
	if err != nil {
		o.logger.Error("task failed",
			"component", "orchestrator",
			"workflow_id", task.WorkflowID,
			"task_id", task.ID,
			"task_name", task.Name,
			"error", err.Error(),
			"duration_ms", time.Since(taskStart).Milliseconds(),
		)
		failErr := o.store.FailTask(ctx, task.ID, err.Error())
		if failErr != nil {
			o.logger.Error("FailTask error",
				"component", "orchestrator",
				"workflow_id", task.WorkflowID,
				"task_id", task.ID,
				"err", failErr,
			)
		}
		o.publishTaskEvent(task.WorkflowID, task.ID, "error", err.Error())
		return fmt.Errorf("runTask agent.Run task=%s: %w", task.ID, err)
	}

	// Store output externally — NOT in the context window.
	outputRef, err := o.dataStore.Put(ctx, result.Reply)
	if err != nil {
		return fmt.Errorf("runTask dataStore.Put task=%s: %w", task.ID, err)
	}

	if err := o.store.CompleteTask(ctx, task.ID, outputRef); err != nil {
		return fmt.Errorf("runTask CompleteTask task=%s: %w", task.ID, err)
	}

	// Publish task completion event with output snippet.
	outputSnippet := result.Reply
	if len(outputSnippet) > 500 {
		outputSnippet = outputSnippet[:500] + "..."
	}
	o.publishTaskEvent(task.WorkflowID, task.ID, "complete", outputSnippet)

	o.logger.Info("task complete",
		"component", "orchestrator",
		"worker", workerID,
		"workflow_id", task.WorkflowID,
		"task_id", task.ID,
		"task_name", task.Name,
		"input_tokens", result.InputTokens,
		"output_tokens", result.OutputTokens,
		"cost_usd", fmt.Sprintf("%.4f", result.CostUSD),
		"output_ref", outputRef,
		"duration_ms", time.Since(taskStart).Milliseconds(),
	)

	// ─── Sprint 13: Track files created during this task ─────────────
	o.scanAndRecordOutputFiles(ctx, task.ID, taskStart)

	// ─── GPT-4o Critic Review ──────────────────────────────────────
	if o.criticFunc != nil {
		o.runCriticReview(ctx, workerID, task, result.Reply)
	}

	// ─── Sprint 12: Auto-save workflow insights when all tasks are done ──
	o.checkAndAutoSave(ctx, task.WorkflowID)

	return nil
}

// scanAndRecordOutputFiles scans ~/zbot-workspace for files created/modified
// during task execution and records them as output_files.
func (o *Orchestrator) scanAndRecordOutputFiles(ctx context.Context, taskID string, taskStart time.Time) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	wsRoot := filepath.Join(home, "zbot-workspace")

	var outputFiles []string
	_ = filepath.Walk(wsRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		name := info.Name()
		if strings.HasPrefix(name, ".") {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() {
			return nil
		}
		// If file was modified after task started, it was created/written during this task.
		if info.ModTime().After(taskStart) {
			relPath, _ := filepath.Rel(wsRoot, path)
			outputFiles = append(outputFiles, relPath)
		}
		return nil
	})

	if len(outputFiles) == 0 {
		return
	}

	if setErr := o.store.SetTaskOutputFiles(ctx, taskID, outputFiles); setErr != nil {
		o.logger.Warn("failed to record output files",
			"task_id", taskID,
			"files", outputFiles,
			"err", setErr,
		)
		return
	}

	o.logger.Info("recorded task output files",
		"task_id", taskID,
		"files", outputFiles,
	)
}

// checkAndAutoSave checks if a workflow is fully complete and triggers insight extraction.
func (o *Orchestrator) checkAndAutoSave(ctx context.Context, workflowID string) {
	if o.memoryStore == nil {
		return
	}

	tasks, err := o.store.GetWorkflowStatus(ctx, workflowID)
	if err != nil {
		return
	}

	// Check if all tasks are in a terminal state.
	for _, t := range tasks {
		if t.Status == agent.TaskPending || t.Status == agent.TaskRunning {
			return // workflow still in progress
		}
	}

	// All tasks done — trigger auto-save in background.
	go o.autoSaveWorkflowInsights(context.Background(), workflowID)
}

// runCriticReview sends the completed task output to the GPT-4o critic.
// If the critic says "fail" and the task hasn't been retried, it re-runs the task
// with the corrected instruction.
func (o *Orchestrator) runCriticReview(ctx context.Context, workerID string, task *agent.Task, output string) {
	verdictJSON, correctedInstruction, shouldRetry, err := o.criticFunc(
		ctx, task.WorkflowID, task.ID, task.Instruction, output,
	)
	if err != nil {
		o.logger.Warn("critic review failed, skipping",
			"task_id", task.ID,
			"err", err,
		)
		return
	}

	o.logger.Info("critic review complete",
		"task_id", task.ID,
		"should_retry", shouldRetry,
	)

	// If retry requested and we haven't retried this task yet — re-run it.
	if shouldRetry && correctedInstruction != "" && !o.retried[task.ID] {
		o.retried[task.ID] = true
		o.logger.Info("critic requested retry",
			"task_id", task.ID,
			"worker", workerID,
		)

		// Create a modified task with the corrected instruction.
		retryTask := *task
		retryTask.Instruction = correctedInstruction

		// Reset task to running state for re-execution.
		o.publishTaskEvent(task.WorkflowID, task.ID, "status", "running")

		retryErr := o.runTask(ctx, workerID, &retryTask)
		if retryErr != nil {
			o.logger.Error("critic retry failed",
				"task_id", task.ID,
				"err", retryErr,
			)
		}
		return
	}

	// Publish verdict regardless (UI shows pass/partial/fail badge).
	_ = verdictJSON
}

// Status returns all tasks for a workflow (for /status command).
func (o *Orchestrator) Status(ctx context.Context, workflowID string) ([]agent.Task, error) {
	return o.store.GetWorkflowStatus(ctx, workflowID)
}

// Cancel cancels all pending tasks in a workflow.
func (o *Orchestrator) Cancel(ctx context.Context, workflowID string) error {
	return o.store.CancelWorkflow(ctx, workflowID)
}

// Store returns the underlying WorkflowStore (used by the planner to submit pre-built task graphs).
func (o *Orchestrator) Store() agent.WorkflowStore {
	return o.store
}

// autoSaveWorkflowInsights extracts key facts from workflow results and saves them to memory.
// Uses Haiku (cheap/fast) to decide what's worth saving.
func (o *Orchestrator) autoSaveWorkflowInsights(ctx context.Context, workflowID string) {
	if o.memoryStore == nil || o.insightExtractor == nil {
		return
	}

	// Gather task outputs for this workflow.
	tasks, err := o.store.GetWorkflowStatus(ctx, workflowID)
	if err != nil {
		o.logger.Warn("auto-save: failed to get workflow tasks", "workflow_id", workflowID, "err", err)
		return
	}

	var summary string
	doneCount := 0
	for _, t := range tasks {
		if t.Status != agent.TaskDone {
			continue
		}
		doneCount++
		output := ""
		if t.OutputRef != "" {
			var data string
			if getErr := o.dataStore.Get(ctx, t.OutputRef, &data); getErr == nil {
				output = data
				if len(output) > 500 {
					output = output[:500]
				}
			}
		}
		summary += fmt.Sprintf("Task: %s\nOutput: %s\n\n", t.Name, output)
	}

	if doneCount == 0 || summary == "" {
		return
	}

	// Truncate total summary to avoid expensive Haiku call.
	if len(summary) > 3000 {
		summary = summary[:3000]
	}

	// Ask Haiku to extract saveable facts.
	extractionPrompt := `Extract 1-3 important facts from this workflow result worth remembering long-term.
Return a JSON array of strings. Only include genuinely useful persistent facts about Jeremy's business, preferences, or important data discovered.
If nothing worth saving, return an empty array [].

Workflow results:
` + summary

	extractCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	result, err := o.insightExtractor.Complete(extractCtx, []agent.Message{
		{Role: agent.RoleUser, Content: extractionPrompt},
	}, nil)
	if err != nil {
		o.logger.Warn("auto-save: insight extraction failed", "workflow_id", workflowID, "err", err)
		return
	}

	// Parse the JSON array response.
	var facts []string
	raw := result.Content
	// Try to find JSON array in the response.
	start := -1
	end := -1
	for i, c := range raw {
		if c == '[' && start == -1 {
			start = i
		}
		if c == ']' {
			end = i + 1
		}
	}
	if start >= 0 && end > start {
		if jsonErr := json.Unmarshal([]byte(raw[start:end]), &facts); jsonErr != nil {
			o.logger.Warn("auto-save: failed to parse extracted facts", "raw", raw[:min(200, len(raw))], "err", jsonErr)
			return
		}
	}

	if len(facts) == 0 {
		o.logger.Debug("auto-save: no facts worth saving from workflow", "workflow_id", workflowID)
		return
	}

	// Save each extracted fact.
	saved := 0
	for _, factText := range facts {
		if factText == "" {
			continue
		}
		b := make([]byte, 8)
		rand.Read(b)
		fact := agent.Fact{
			ID:        hex.EncodeToString(b),
			Content:   factText,
			Source:    "workflow",
			Tags:      []string{"workflow_insight"},
			CreatedAt: time.Now(),
		}
		if saveErr := o.memoryStore.Save(ctx, fact); saveErr != nil {
			o.logger.Warn("auto-save: save failed", "fact", factText[:min(80, len(factText))], "err", saveErr)
			continue
		}
		saved++
	}

	o.logger.Info("auto-saved facts from workflow",
		"workflow_id", workflowID,
		"count", saved,
	)
}

// decompose asks the agent to break a request into a task graph.
// Returns a slice of Tasks with dependency edges set.
func (o *Orchestrator) decompose(ctx context.Context, sessionID, request string) ([]agent.Task, error) {
	decompositionPrompt := fmt.Sprintf(`You are a workflow planner. Break this request into discrete, atomic tasks.

REQUEST: %s

Rules:
- Each task must be completable independently given only the output of its direct dependency
- Tasks with no dependencies can run in parallel
- Maximum 20 tasks per workflow
- Each task instruction must be completely self-contained

Respond in JSON:
{
  "tasks": [
    {
      "name": "short name",
      "instruction": "complete instruction for this step",
      "depends_on_names": ["name of task this depends on"] // or [] for parallel
    }
  ]
}`, request)

	input := agent.TurnInput{
		SessionID: sessionID + "-decompose",
		UserMsg: agent.Message{
			Role:    agent.RoleUser,
			Content: decompositionPrompt,
		},
	}

	result, err := o.ag.Run(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("decompose agent.Run: %w", err)
	}

	return platform.ParseTaskGraph(result.Reply)
}
