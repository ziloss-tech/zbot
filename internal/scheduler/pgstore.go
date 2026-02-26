package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// JobStore persists scheduled jobs to survive restarts.
type JobStore interface {
	Save(ctx context.Context, job Job) error
	Load(ctx context.Context) ([]Job, error)
	UpdateNextRun(ctx context.Context, id string, nextRun time.Time) error
	Delete(ctx context.Context, id string) error
}

// PGJobStore implements JobStore using Postgres.
type PGJobStore struct {
	db *pgxpool.Pool
}

// NewPGJobStore creates a job store backed by Postgres.
// Creates the table if it doesn't exist.
func NewPGJobStore(ctx context.Context, db *pgxpool.Pool) (*PGJobStore, error) {
	_, err := db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS zbot_scheduled_jobs (
			id           TEXT PRIMARY KEY,
			name         TEXT NOT NULL,
			cron_expr    TEXT NOT NULL,
			instruction  TEXT NOT NULL,
			session_id   TEXT NOT NULL,
			next_run     TIMESTAMPTZ NOT NULL,
			active       BOOLEAN NOT NULL DEFAULT TRUE,
			created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`)
	if err != nil {
		return nil, fmt.Errorf("create scheduled_jobs table: %w", err)
	}
	return &PGJobStore{db: db}, nil
}

// Save persists a job to the database.
func (s *PGJobStore) Save(ctx context.Context, job Job) error {
	_, err := s.db.Exec(ctx, `
		INSERT INTO zbot_scheduled_jobs (id, name, cron_expr, instruction, session_id, next_run, active, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (id) DO UPDATE SET
			name = EXCLUDED.name,
			cron_expr = EXCLUDED.cron_expr,
			instruction = EXCLUDED.instruction,
			session_id = EXCLUDED.session_id,
			next_run = EXCLUDED.next_run,
			active = EXCLUDED.active
	`, job.ID, job.Name, job.CronExpr, job.Instruction, job.SessionID, job.NextRun, job.Active, job.CreatedAt)
	return err
}

// Load retrieves all active jobs from the database.
func (s *PGJobStore) Load(ctx context.Context) ([]Job, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, name, cron_expr, instruction, session_id, next_run, active, created_at
		FROM zbot_scheduled_jobs WHERE active = TRUE
	`)
	if err != nil {
		return nil, fmt.Errorf("load jobs: %w", err)
	}
	defer rows.Close()

	var jobs []Job
	for rows.Next() {
		var j Job
		if err := rows.Scan(&j.ID, &j.Name, &j.CronExpr, &j.Instruction,
			&j.SessionID, &j.NextRun, &j.Active, &j.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan job: %w", err)
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

// UpdateNextRun updates the next run time for a job.
func (s *PGJobStore) UpdateNextRun(ctx context.Context, id string, nextRun time.Time) error {
	_, err := s.db.Exec(ctx, `
		UPDATE zbot_scheduled_jobs SET next_run = $2 WHERE id = $1
	`, id, nextRun)
	return err
}

// Delete marks a job as inactive (soft delete).
func (s *PGJobStore) Delete(ctx context.Context, id string) error {
	_, err := s.db.Exec(ctx, `
		UPDATE zbot_scheduled_jobs SET active = FALSE WHERE id = $1
	`, id)
	return err
}
