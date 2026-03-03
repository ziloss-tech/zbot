package research

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PGResearchStore handles persistence for research sessions.
type PGResearchStore struct {
	db *pgxpool.Pool
}

// NewPGResearchStore creates the store and ensures the tables exist.
func NewPGResearchStore(ctx context.Context, db *pgxpool.Pool) (*PGResearchStore, error) {
	store := &PGResearchStore{db: db}
	if err := store.createTables(ctx); err != nil {
		return nil, fmt.Errorf("research store init: %w", err)
	}
	return store, nil
}

func (s *PGResearchStore) createTables(ctx context.Context) error {
	// Research sessions table.
	_, err := s.db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS zbot_research_sessions (
			id TEXT PRIMARY KEY,
			workflow_id TEXT,
			goal TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'running',
			iterations INT DEFAULT 0,
			confidence_score FLOAT DEFAULT 0,
			final_report TEXT,
			state_json JSONB,
			cost_usd FLOAT DEFAULT 0,
			error TEXT DEFAULT '',
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)`)
	if err != nil {
		return fmt.Errorf("create research_sessions: %w", err)
	}

	// Indexes.
	_, _ = s.db.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_research_sessions_status ON zbot_research_sessions(status)`)

	// Model spend table (budget tracking).
	_, err = s.db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS zbot_model_spend (
			id SERIAL PRIMARY KEY,
			session_id TEXT,
			model_id TEXT,
			prompt_tokens INT,
			completion_tokens INT,
			cost_usd FLOAT,
			recorded_at TIMESTAMPTZ DEFAULT NOW()
		)`)
	if err != nil {
		return fmt.Errorf("create model_spend: %w", err)
	}

	_, _ = s.db.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_spend_date ON zbot_model_spend(recorded_at)`)
	_, _ = s.db.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_spend_session ON zbot_model_spend(session_id)`)

	return nil
}

// CreateSession saves a new research session with status "running".
func (s *PGResearchStore) CreateSession(ctx context.Context, id, goal string) error {
	_, err := s.db.Exec(ctx,
		`INSERT INTO zbot_research_sessions (id, goal, status, created_at, updated_at)
		 VALUES ($1, $2, 'running', NOW(), NOW())`,
		id, goal,
	)
	return err
}

// UpdateSession updates session state (called after each iteration).
func (s *PGResearchStore) UpdateSession(ctx context.Context, id string, state *ResearchState) error {
	stateJSON, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}

	_, err = s.db.Exec(ctx,
		`UPDATE zbot_research_sessions SET
			iterations = $2,
			confidence_score = $3,
			cost_usd = $4,
			state_json = $5,
			updated_at = NOW()
		 WHERE id = $1`,
		id, state.Iteration, state.Critique.ConfidenceScore, state.CostUSD, stateJSON,
	)
	return err
}

// CompleteSession marks a session as complete with the final report.
func (s *PGResearchStore) CompleteSession(ctx context.Context, id, report string, state *ResearchState) error {
	stateJSON, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}

	_, err = s.db.Exec(ctx,
		`UPDATE zbot_research_sessions SET
			status = 'complete',
			iterations = $2,
			confidence_score = $3,
			final_report = $4,
			cost_usd = $5,
			state_json = $6,
			updated_at = NOW()
		 WHERE id = $1`,
		id, state.Iteration, state.Critique.ConfidenceScore, report, state.CostUSD, stateJSON,
	)
	return err
}

// FailSession marks a session as failed.
func (s *PGResearchStore) FailSession(ctx context.Context, id, errMsg string) error {
	_, err := s.db.Exec(ctx,
		`UPDATE zbot_research_sessions SET status = 'failed', error = $2, updated_at = NOW() WHERE id = $1`,
		id, errMsg,
	)
	return err
}

// GetSession retrieves a research session by ID.
func (s *PGResearchStore) GetSession(ctx context.Context, id string) (*ResearchSession, error) {
	var sess ResearchSession
	var stateJSON []byte
	err := s.db.QueryRow(ctx,
		`SELECT id, COALESCE(workflow_id, ''), goal, status, iterations, confidence_score,
		        COALESCE(final_report, ''), COALESCE(state_json::text, '{}'), cost_usd,
		        COALESCE(error, ''), created_at, updated_at
		 FROM zbot_research_sessions WHERE id = $1`, id,
	).Scan(
		&sess.ID, &sess.WorkflowID, &sess.Goal, &sess.Status,
		&sess.Iterations, &sess.ConfidenceScore,
		&sess.FinalReport, &stateJSON, &sess.CostUSD,
		&sess.Error, &sess.CreatedAt, &sess.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("get session %s: %w", id, err)
	}
	sess.StateJSON = string(stateJSON)
	return &sess, nil
}

// ListSessions returns recent research sessions (newest first).
func (s *PGResearchStore) ListSessions(ctx context.Context, limit int) ([]ResearchSession, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.Query(ctx,
		`SELECT id, COALESCE(workflow_id, ''), goal, status, iterations, confidence_score,
		        COALESCE(final_report, ''), cost_usd, COALESCE(error, ''), created_at, updated_at
		 FROM zbot_research_sessions ORDER BY created_at DESC LIMIT $1`, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()

	var sessions []ResearchSession
	for rows.Next() {
		var sess ResearchSession
		if err := rows.Scan(
			&sess.ID, &sess.WorkflowID, &sess.Goal, &sess.Status,
			&sess.Iterations, &sess.ConfidenceScore,
			&sess.FinalReport, &sess.CostUSD,
			&sess.Error, &sess.CreatedAt, &sess.UpdatedAt,
		); err != nil {
			continue
		}
		sessions = append(sessions, sess)
	}
	return sessions, nil
}

// GetSessionCost returns the total cost for a specific session.
func (s *PGResearchStore) GetSessionCost(ctx context.Context, sessionID string) (float64, error) {
	var cost float64
	err := s.db.QueryRow(ctx,
		`SELECT COALESCE(SUM(cost_usd), 0) FROM zbot_model_spend WHERE session_id = $1`,
		sessionID,
	).Scan(&cost)
	return cost, err
}

// GetDailyStats returns today's total spend and session count.
func (s *PGResearchStore) GetDailyStats(ctx context.Context) (totalCost float64, sessionCount int, err error) {
	today := time.Now().Truncate(24 * time.Hour)
	err = s.db.QueryRow(ctx,
		`SELECT COALESCE(SUM(cost_usd), 0), COUNT(DISTINCT session_id)
		 FROM zbot_model_spend WHERE recorded_at >= $1`,
		today,
	).Scan(&totalCost, &sessionCount)
	return
}
