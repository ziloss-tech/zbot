package workflow

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jeremylerwick-max/zbot/internal/agent"
)

// PGDataStore implements agent.DataStore using the zbot_data_refs table.
// Stores arbitrary JSON payloads between workflow task steps.
type PGDataStore struct {
	db *pgxpool.Pool
}

func NewPGDataStore(db *pgxpool.Pool) *PGDataStore {
	return &PGDataStore{db: db}
}

// Put stores any JSON-serializable value and returns a unique reference key.
func (s *PGDataStore) Put(ctx context.Context, data any) (string, error) {
	ref := randomID()
	_, err := s.db.Exec(ctx,
		`INSERT INTO zbot_data_refs (ref, data, created_at, expires_at)
		 VALUES ($1, $2::jsonb, NOW(), NOW() + INTERVAL '7 days')`,
		ref, fmt.Sprintf("%q", fmt.Sprint(data)),
	)
	if err != nil {
		return "", fmt.Errorf("DataStore.Put: %w", err)
	}
	return ref, nil
}

// Get retrieves the stored value by reference key into dest.
func (s *PGDataStore) Get(ctx context.Context, ref string, dest any) error {
	row := s.db.QueryRow(ctx,
		`SELECT data FROM zbot_data_refs WHERE ref = $1`, ref,
	)
	return row.Scan(dest)
}

// Delete removes the stored value by reference key.
func (s *PGDataStore) Delete(ctx context.Context, ref string) error {
	_, err := s.db.Exec(ctx,
		`DELETE FROM zbot_data_refs WHERE ref = $1`, ref,
	)
	return err
}

// Ensure PGDataStore implements the port.
var _ agent.DataStore = (*PGDataStore)(nil)

// WorkflowSummary holds a formatted status for Slack reporting.
type WorkflowSummary struct {
	WorkflowID string
	Tasks      []agent.Task
	Done       int
	Total      int
}

// GetSummary fetches a workflow's task list and computes totals.
func GetSummary(ctx context.Context, store agent.WorkflowStore, workflowID string) (*WorkflowSummary, error) {
	tasks, err := store.GetWorkflowStatus(ctx, workflowID)
	if err != nil {
		return nil, err
	}

	done := 0
	for _, t := range tasks {
		if t.Status == agent.TaskDone {
			done++
		}
	}

	return &WorkflowSummary{
		WorkflowID: workflowID,
		Tasks:      tasks,
		Done:       done,
		Total:      len(tasks),
	}, nil
}

// Format returns a Slack-friendly status string.
func (s *WorkflowSummary) Format() string {
	out := fmt.Sprintf("*Workflow `%s`* — %d/%d tasks complete\n", s.WorkflowID, s.Done, s.Total)
	for _, t := range s.Tasks {
		var icon string
		switch t.Status {
		case agent.TaskDone:
			icon = "✅"
		case agent.TaskRunning:
			icon = "🔄"
		case agent.TaskFailed:
			icon = "❌"
		case agent.TaskCanceled:
			icon = "🚫"
		default:
			icon = "⏳"
		}
		out += fmt.Sprintf("%s *Step %d: %s* — `%s`\n", icon, t.Step, t.Name, t.Status)
	}

	if s.Done < s.Total {
		remaining := s.Total - s.Done
		out += fmt.Sprintf("\n_%d task(s) remaining_", remaining)
	} else {
		out += "\n_All tasks complete_ 🎉"
	}

	return out
}
