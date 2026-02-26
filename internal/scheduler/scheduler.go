package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Job is a scheduled task.
type Job struct {
	ID          string
	Name        string
	CronExpr    string    // standard cron: "0 8 * * 1" = every Monday 8am
	Instruction string    // what to tell the agent when it fires
	SessionID   string    // which Slack session to reply to
	NextRun     time.Time
	Active      bool
	CreatedAt   time.Time
}

// Handler is called when a scheduled job fires.
type Handler func(ctx context.Context, sessionID, instruction string)

// Scheduler ticks every 30 seconds and fires any due jobs.
type Scheduler struct {
	mu      sync.RWMutex
	jobs    map[string]*Job
	handler Handler
	store   JobStore
	logger  *slog.Logger
}

// New creates a Scheduler. Call Start() to begin the tick loop.
func New(store JobStore, handler Handler, logger *slog.Logger) *Scheduler {
	return &Scheduler{
		jobs:    make(map[string]*Job),
		handler: handler,
		store:   store,
		logger:  logger,
	}
}

// Start loads persisted jobs from DB and launches the background tick loop.
func (s *Scheduler) Start(ctx context.Context) {
	// Load all active jobs from the database so we survive restarts.
	jobs, err := s.store.Load(ctx)
	if err != nil {
		s.logger.Error("scheduler: failed to load jobs from DB", "err", err)
	} else {
		s.mu.Lock()
		for i := range jobs {
			j := jobs[i]
			s.jobs[j.ID] = &j
		}
		s.mu.Unlock()
		s.logger.Info("scheduler: loaded persisted jobs", "count", len(jobs))
	}

	go s.tick(ctx)
	s.logger.Info("scheduler started — checking every 30s")
}

// Add creates a new scheduled job and persists it.
func (s *Scheduler) Add(ctx context.Context, job Job) error {
	// Validate and compute next run.
	parsed, err := ParseCron(job.CronExpr)
	if err != nil {
		return err
	}
	job.NextRun = NextCronTime(parsed, time.Now())
	job.Active = true
	if job.CreatedAt.IsZero() {
		job.CreatedAt = time.Now()
	}

	if err := s.store.Save(ctx, job); err != nil {
		return err
	}

	s.mu.Lock()
	s.jobs[job.ID] = &job
	s.mu.Unlock()

	s.logger.Info("scheduler: job added",
		"id", job.ID,
		"name", job.Name,
		"cron", job.CronExpr,
		"next_run", job.NextRun.Format(time.RFC3339),
	)
	return nil
}

// Remove deletes a job by ID.
func (s *Scheduler) Remove(ctx context.Context, id string) error {
	if err := s.store.Delete(ctx, id); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.jobs, id)
	s.mu.Unlock()

	s.logger.Info("scheduler: job removed", "id", id)
	return nil
}

// List returns all active jobs.
func (s *Scheduler) List() []Job {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]Job, 0, len(s.jobs))
	for _, j := range s.jobs {
		result = append(result, *j)
	}
	return result
}

// tick is the background loop that checks for due jobs every 30 seconds.
func (s *Scheduler) tick(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			s.logger.Info("scheduler stopped")
			return
		case <-time.After(30 * time.Second):
			s.fireDue(ctx)
		}
	}
}

// fireDue fires all jobs whose NextRun has passed.
func (s *Scheduler) fireDue(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for _, job := range s.jobs {
		if !job.Active || now.Before(job.NextRun) {
			continue
		}

		s.logger.Info("scheduler: firing job",
			"id", job.ID,
			"name", job.Name,
		)

		// Fire asynchronously so we don't block the tick loop.
		j := job
		go s.handler(ctx, j.SessionID, j.Instruction)

		// Compute next run.
		parsed, err := ParseCron(job.CronExpr)
		if err != nil {
			s.logger.Error("scheduler: bad cron on fire", "id", job.ID, "err", err)
			continue
		}
		job.NextRun = NextCronTime(parsed, now)
		_ = s.store.UpdateNextRun(ctx, job.ID, job.NextRun)
	}
}
