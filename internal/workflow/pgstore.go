package workflow

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jeremylerwick-max/zbot/internal/agent"
)

// PGWorkflowStore implements agent.WorkflowStore using Postgres.
// Uses SELECT FOR UPDATE SKIP LOCKED for concurrent-safe task claiming.
type PGWorkflowStore struct {
	db *pgxpool.Pool
}

func NewPGWorkflowStore(db *pgxpool.Pool) (*PGWorkflowStore, error) {
	return &PGWorkflowStore{db: db}, nil
}

func randomID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// CreateWorkflow inserts a workflow and all its tasks atomically.
func (s *PGWorkflowStore) CreateWorkflow(ctx context.Context, tasks []agent.Task) (string, error) {
	wfID := randomID()
	now := time.Now()

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("CreateWorkflow begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx,
		`INSERT INTO zbot_workflows (id, session_id, request, status, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		wfID, "", "", "running", now, now,
	)
	if err != nil {
		return "", fmt.Errorf("CreateWorkflow insert workflow: %w", err)
	}

	for _, t := range tasks {
		_, err = tx.Exec(ctx,
			`INSERT INTO zbot_tasks
			 (id, workflow_id, step, name, instruction, status, depends_on, created_at, updated_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			t.ID, wfID, t.Step, t.Name, t.Instruction, string(agent.TaskPending),
			t.DependsOn, now, now,
		)
		if err != nil {
			return "", fmt.Errorf("CreateWorkflow insert task %s: %w", t.ID, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("CreateWorkflow commit: %w", err)
	}

	return wfID, nil
}

// ClaimNextTask atomically claims the next runnable pending task using SKIP LOCKED.
// Returns nil (no error) when there are no available tasks.
func (s *PGWorkflowStore) ClaimNextTask(ctx context.Context, workerID string) (*agent.Task, error) {
	row := s.db.QueryRow(ctx, `
		UPDATE zbot_tasks SET status = 'running', claimed_by = $1, updated_at = NOW()
		WHERE id = (
			SELECT t.id FROM zbot_tasks t
			WHERE t.status = 'pending'
			  AND NOT EXISTS (
				  SELECT 1 FROM zbot_tasks dep
				  WHERE dep.id = ANY(t.depends_on)
				    AND dep.status != 'done'
			  )
			ORDER BY t.step ASC, t.created_at ASC
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		RETURNING id, workflow_id, step, name, instruction, status, depends_on,
		          input_ref, output_ref, created_at, updated_at
	`, workerID)

	t, err := scanTask(row)
	if err != nil {
		// pgx returns pgx.ErrNoRows when nothing to claim — that's normal.
		return nil, nil
	}
	return t, nil
}

// CompleteTask marks a task done and stores its output reference.
func (s *PGWorkflowStore) CompleteTask(ctx context.Context, taskID, outputRef string) error {
	_, err := s.db.Exec(ctx,
		`UPDATE zbot_tasks SET status = 'done', output_ref = $2, updated_at = NOW()
		 WHERE id = $1`,
		taskID, outputRef,
	)
	return err
}

// FailTask marks a task failed with an error message.
func (s *PGWorkflowStore) FailTask(ctx context.Context, taskID, errMsg string) error {
	_, err := s.db.Exec(ctx,
		`UPDATE zbot_tasks SET status = 'failed', error_msg = $2, updated_at = NOW()
		 WHERE id = $1`,
		taskID, errMsg,
	)
	return err
}

// GetWorkflowStatus returns all tasks for a workflow, ordered by step.
func (s *PGWorkflowStore) GetWorkflowStatus(ctx context.Context, workflowID string) ([]agent.Task, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id, workflow_id, step, name, instruction, status, depends_on,
		        input_ref, output_ref, created_at, updated_at
		 FROM zbot_tasks WHERE workflow_id = $1 ORDER BY step ASC`,
		workflowID,
	)
	if err != nil {
		return nil, fmt.Errorf("GetWorkflowStatus query: %w", err)
	}
	defer rows.Close()

	var tasks []agent.Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, fmt.Errorf("GetWorkflowStatus scan: %w", err)
		}
		tasks = append(tasks, *t)
	}
	return tasks, rows.Err()
}

// CancelWorkflow cancels all pending tasks in a workflow.
func (s *PGWorkflowStore) CancelWorkflow(ctx context.Context, workflowID string) error {
	_, err := s.db.Exec(ctx,
		`UPDATE zbot_tasks SET status = 'canceled', updated_at = NOW()
		 WHERE workflow_id = $1 AND status = 'pending'`,
		workflowID,
	)
	return err
}

// SetTaskOutputFiles records files created during a task. Sprint 13.
func (s *PGWorkflowStore) SetTaskOutputFiles(ctx context.Context, taskID string, files []string) error {
	_, err := s.db.Exec(ctx,
		`UPDATE zbot_tasks SET output_files = $2, updated_at = NOW() WHERE id = $1`,
		taskID, files,
	)
	return err
}

// scanner is a common interface for pgx Row and Rows.
type scanner interface {
	Scan(dest ...any) error
}

func scanTask(row scanner) (*agent.Task, error) {
	var t agent.Task
	var status string
	var inputRef, outputRef *string
	var dependsOn []string

	err := row.Scan(
		&t.ID, &t.WorkflowID, &t.Step, &t.Name, &t.Instruction,
		&status, &dependsOn, &inputRef, &outputRef,
		&t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	t.Status = agent.TaskStatus(status)
	t.DependsOn = dependsOn
	if inputRef != nil {
		t.InputRef = *inputRef
	}
	if outputRef != nil {
		t.OutputRef = *outputRef
	}

	return &t, nil
}

// Ensure PGWorkflowStore implements the port.
var _ agent.WorkflowStore = (*PGWorkflowStore)(nil)

// NoopWorkflowStore is a no-op fallback when Postgres is unavailable.
type NoopWorkflowStore struct{}

func (n *NoopWorkflowStore) CreateWorkflow(_ context.Context, _ []agent.Task) (string, error) {
	return "", fmt.Errorf("workflow store unavailable (no Postgres connection)")
}
func (n *NoopWorkflowStore) ClaimNextTask(_ context.Context, _ string) (*agent.Task, error) {
	return nil, nil
}
func (n *NoopWorkflowStore) CompleteTask(_ context.Context, _, _ string) error { return nil }
func (n *NoopWorkflowStore) FailTask(_ context.Context, _, _ string) error     { return nil }
func (n *NoopWorkflowStore) GetWorkflowStatus(_ context.Context, _ string) ([]agent.Task, error) {
	return nil, fmt.Errorf("workflow store unavailable")
}
func (n *NoopWorkflowStore) CancelWorkflow(_ context.Context, _ string) error          { return nil }
func (n *NoopWorkflowStore) SetTaskOutputFiles(_ context.Context, _ string, _ []string) error { return nil }

var _ agent.WorkflowStore = (*NoopWorkflowStore)(nil)
