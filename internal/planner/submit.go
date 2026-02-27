package planner

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/jeremylerwick-max/zbot/internal/agent"
)

// Submit converts a TaskGraph into agent.Tasks and persists them via the WorkflowStore.
// The existing WorkflowOrchestrator picks up and executes the tasks automatically.
// Returns the new workflow ID.
func Submit(ctx context.Context, store agent.WorkflowStore, graph *TaskGraph, sessionID string) (string, error) {
	if len(graph.Tasks) == 0 {
		return "", fmt.Errorf("cannot submit empty task graph")
	}

	// Build a map from planner task ID → agent task ID so we can resolve depends_on
	idMap := make(map[string]string, len(graph.Tasks))
	for i, pt := range graph.Tasks {
		idMap[pt.ID] = fmt.Sprintf("task-%s-%d", randomShort(), i+1)
	}

	tasks := make([]agent.Task, 0, len(graph.Tasks))
	for i, pt := range graph.Tasks {
		// Resolve depends_on planner IDs → agent task IDs
		deps := make([]string, 0, len(pt.DependsOn))
		for _, dep := range pt.DependsOn {
			if agentID, ok := idMap[dep]; ok {
				deps = append(deps, agentID)
			}
		}

		// Build a rich instruction that includes tool hints
		instruction := pt.Instruction
		if len(pt.ToolHints) > 0 {
			instruction += fmt.Sprintf("\n\n[Suggested tools: %v]", pt.ToolHints)
		}

		tasks = append(tasks, agent.Task{
			ID:          idMap[pt.ID],
			Step:        i + 1,
			Name:        pt.Title,
			Instruction: instruction,
			DependsOn:   deps,
			Status:      agent.TaskPending,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		})
	}

	wfID, err := store.CreateWorkflow(ctx, tasks)
	if err != nil {
		return "", fmt.Errorf("planner.Submit CreateWorkflow: %w", err)
	}

	return wfID, nil
}

func randomShort() string {
	b := make([]byte, 3)
	rand.Read(b)
	return hex.EncodeToString(b)
}
