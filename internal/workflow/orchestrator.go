// Package workflow implements the ZBOT workflow orchestrator.
// Core design: database IS the state. Goroutines are ephemeral workers.
// A workflow survives process restarts because its task graph lives in Postgres.
package workflow

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jeremylerwick-max/zbot/internal/agent"
	"github.com/jeremylerwick-max/zbot/internal/platform"
)

// Orchestrator manages workflow lifecycle: decompose → dispatch → collect.
type Orchestrator struct {
	store     agent.WorkflowStore
	dataStore agent.DataStore
	ag        *agent.Agent
	workerN   int           // number of parallel workers
	pollEvery time.Duration // how often to check for pending tasks
	logger    *slog.Logger
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
				o.logger.Error("worker error", "worker", workerID, "err", err)
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

	return o.runTask(ctx, workerID, task)
}

// runTask executes a single task in an isolated agent context.
// KEY: Each task gets a FRESH context — it does not see the full workflow history.
// It only receives: its specific instruction + the output of its direct dependency.
func (o *Orchestrator) runTask(ctx context.Context, workerID string, task *agent.Task) error {
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
		SessionID: fmt.Sprintf("workflow-%s-task-%s", task.WorkflowID, task.ID),
		UserMsg: agent.Message{
			Role:    agent.RoleUser,
			Content: instruction,
		},
	}

	result, err := o.ag.Run(taskCtx, input)
	if err != nil {
		failErr := o.store.FailTask(ctx, task.ID, err.Error())
		if failErr != nil {
			o.logger.Error("FailTask error", "task", task.ID, "err", failErr)
		}
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

	o.logger.Info("task complete",
		"worker", workerID,
		"task_id", task.ID,
		"task_name", task.Name,
		"tokens", result.TokensUsed,
		"output_ref", outputRef,
	)

	return nil
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
