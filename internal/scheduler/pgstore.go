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
	LoadAll(ctx context.Context) ([]Job, error) // Sprint 14: load all jobs including paused
	UpdateNextRun(ctx context.Context, id string, nextRun time.Time) error
	UpdateStatus(ctx context.Context, id string, status string) error
	MarkRunComplete(ctx context.Context, id string, lastRun time.Time, runCount int) error
	Delete(ctx context.Context, id string) error
	HardDelete(ctx context.Context, id string) error // Sprint 14: actually remove from DB
}

// PGJobStore implements JobStore using Postgres.
type PGJobStore struct {
	db *pgxpool.Pool
}

// NewPGJobStore creates a job store backed by Postgres.
// Creates the table if it doesn't exist and runs Sprint 14 migrations.
func NewPGJobStore(ctx context.Context, db *pgxpool.Pool) (*PGJobStore, error) {
	_, err := db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS zbot_scheduled_jobs (
			id                TEXT PRIMARY KEY,
			name              TEXT NOT NULL,
			goal              TEXT NOT NULL DEFAULT '',
			cron_expr         TEXT NOT NULL,
			natural_schedule  TEXT NOT NULL DEFAULT '',
			instruction       TEXT NOT NULL DEFAULT '',
			session_id        TEXT NOT NULL DEFAULT '',
			status            TEXT NOT NULL DEFAULT 'active',
			next_run          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			last_run          TIMESTAMPTZ,
			run_count         INT NOT NULL DEFAULT 0,
			active            BOOLEAN NOT NULL DEFAULT TRUE,
			created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`)
	if err != nil {
		return nil, fmt.Errorf("create scheduled_jobs table: %w", err)
	}

	// Sprint 14: Add new columns if missing (idempotent migration).
	migrations := []string{
		`ALTER TABLE zbot_scheduled_jobs ADD COLUMN IF NOT EXISTS goal TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE zbot_scheduled_jobs ADD COLUMN IF NOT EXISTS natural_schedule TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE zbot_scheduled_jobs ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'active'`,
		`ALTER TABLE zbot_scheduled_jobs ADD COLUMN IF NOT EXISTS last_run TIMESTAMPTZ`,
		`ALTER TABLE zbot_scheduled_jobs ADD COLUMN IF NOT EXISTS run_count INT NOT NULL DEFAULT 0`,
		`ALTER TABLE zbot_scheduled_jobs ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()`,
	}
	for _, m := range migrations {
		_, _ = db.Exec(ctx, m) // Ignore errors from columns that already exist.
	}

	return &PGJobStore{db: db}, nil
}

// Save persists a job to the database.
func (s *PGJobStore) Save(ctx context.Context, job Job) error {
	status := job.Status
	if status == "" {
		status = "active"
	}
	_, err := s.db.Exec(ctx, `
		INSERT INTO zbot_scheduled_jobs (id, name, goal, cron_expr, natural_schedule, instruction, session_id, status, next_run, active, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NOW())
		ON CONFLICT (id) DO UPDATE SET
			name = EXCLUDED.name,
			goal = EXCLUDED.goal,
			cron_expr = EXCLUDED.cron_expr,
			natural_schedule = EXCLUDED.natural_schedule,
			instruction = EXCLUDED.instruction,
			session_id = EXCLUDED.session_id,
			status = EXCLUDED.status,
			next_run = EXCLUDED.next_run,
			active = EXCLUDED.active,
			updated_at = NOW()
	`, job.ID, job.Name, job.Goal, job.CronExpr, job.NaturalSchedule, job.Instruction, job.SessionID, status, job.NextRun, job.Active, job.CreatedAt)
	return err
}

// Load retrieves all active/running jobs from the database (for the scheduler tick loop).
func (s *PGJobStore) Load(ctx context.Context) ([]Job, error) {
	return s.loadJobs(ctx, `WHERE status IN ('active', 'running')`)
}

// LoadAll retrieves all jobs including paused (for the UI panel).
func (s *PGJobStore) LoadAll(ctx context.Context) ([]Job, error) {
	return s.loadJobs(ctx, `WHERE status != 'deleted'`)
}

func (s *PGJobStore) loadJobs(ctx context.Context, whereClause string) ([]Job, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, name, COALESCE(goal, ''), cron_expr, COALESCE(natural_schedule, ''),
		       COALESCE(instruction, ''), COALESCE(session_id, ''),
		       COALESCE(status, 'active'), next_run, last_run,
		       COALESCE(run_count, 0), COALESCE(active, TRUE), created_at, COALESCE(updated_at, created_at)
		FROM zbot_scheduled_jobs `+whereClause+`
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("load jobs: %w", err)
	}
	defer rows.Close()

	var jobs []Job
	for rows.Next() {
		var j Job
		if err := rows.Scan(&j.ID, &j.Name, &j.Goal, &j.CronExpr, &j.NaturalSchedule,
			&j.Instruction, &j.SessionID, &j.Status, &j.NextRun, &j.LastRun,
			&j.RunCount, &j.Active, &j.CreatedAt, &j.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan job: %w", err)
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

// UpdateNextRun updates the next run time for a job.
func (s *PGJobStore) UpdateNextRun(ctx context.Context, id string, nextRun time.Time) error {
	_, err := s.db.Exec(ctx, `
		UPDATE zbot_scheduled_jobs SET next_run = $2, updated_at = NOW() WHERE id = $1
	`, id, nextRun)
	return err
}

// UpdateStatus changes a job's status (active, paused, running).
func (s *PGJobStore) UpdateStatus(ctx context.Context, id string, status string) error {
	active := status == "active" || status == "running"
	_, err := s.db.Exec(ctx, `
		UPDATE zbot_scheduled_jobs SET status = $2, active = $3, updated_at = NOW() WHERE id = $1
	`, id, status, active)
	return err
}

// MarkRunComplete updates last_run, run_count, and resets status to active.
func (s *PGJobStore) MarkRunComplete(ctx context.Context, id string, lastRun time.Time, runCount int) error {
	_, err := s.db.Exec(ctx, `
		UPDATE zbot_scheduled_jobs SET status = 'active', last_run = $2, run_count = $3, active = TRUE, updated_at = NOW() WHERE id = $1
	`, id, lastRun, runCount)
	return err
}

// Delete marks a job as inactive (soft delete).
func (s *PGJobStore) Delete(ctx context.Context, id string) error {
	_, err := s.db.Exec(ctx, `
		UPDATE zbot_scheduled_jobs SET active = FALSE, status = 'paused', updated_at = NOW() WHERE id = $1
	`, id)
	return err
}

// HardDelete actually removes a job from the database.
func (s *PGJobStore) HardDelete(ctx context.Context, id string) error {
	_, err := s.db.Exec(ctx, `DELETE FROM zbot_scheduled_jobs WHERE id = $1`, id)
	return err
}

// ─── Sprint 14: URL Monitor Store ───────────────────────────────────────────

// MonitorEntry represents a saved URL monitor.
type MonitorEntry struct {
	ID                   string
	Name                 string
	URL                  string
	CheckIntervalMinutes int
	NotifyOnChange       string
	Active               bool
	CreatedAt            time.Time
}

// CreateMonitorsTable ensures the monitors table exists.
func (s *PGJobStore) CreateMonitorsTable(ctx context.Context) error {
	_, err := s.db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS zbot_monitors (
			id                     TEXT PRIMARY KEY,
			name                   TEXT NOT NULL,
			url                    TEXT NOT NULL,
			check_interval_minutes INT NOT NULL DEFAULT 60,
			notify_on_change       TEXT NOT NULL DEFAULT '',
			active                 BOOLEAN NOT NULL DEFAULT TRUE,
			created_at             TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`)
	return err
}

// SaveMonitor persists a monitor entry.
func (s *PGJobStore) SaveMonitor(ctx context.Context, m MonitorEntry) error {
	_, err := s.db.Exec(ctx, `
		INSERT INTO zbot_monitors (id, name, url, check_interval_minutes, notify_on_change, active, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (id) DO UPDATE SET
			name = EXCLUDED.name, url = EXCLUDED.url,
			check_interval_minutes = EXCLUDED.check_interval_minutes,
			notify_on_change = EXCLUDED.notify_on_change,
			active = EXCLUDED.active
	`, m.ID, m.Name, m.URL, m.CheckIntervalMinutes, m.NotifyOnChange, m.Active, m.CreatedAt)
	return err
}

// LoadMonitors retrieves all active monitors.
func (s *PGJobStore) LoadMonitors(ctx context.Context) ([]MonitorEntry, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, name, url, check_interval_minutes, notify_on_change, active, created_at
		FROM zbot_monitors WHERE active = TRUE ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var monitors []MonitorEntry
	for rows.Next() {
		var m MonitorEntry
		if err := rows.Scan(&m.ID, &m.Name, &m.URL, &m.CheckIntervalMinutes, &m.NotifyOnChange, &m.Active, &m.CreatedAt); err != nil {
			return nil, err
		}
		monitors = append(monitors, m)
	}
	return monitors, rows.Err()
}

// DeleteMonitor soft-deletes a monitor.
func (s *PGJobStore) DeleteMonitor(ctx context.Context, id string) error {
	_, err := s.db.Exec(ctx, `UPDATE zbot_monitors SET active = FALSE WHERE id = $1`, id)
	return err
}
