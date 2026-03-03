package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Job is a scheduled task.
type Job struct {
	ID              string
	Name            string
	Goal            string    // Sprint 14: full goal text for the planner
	CronExpr        string    // standard cron: "0 8 * * 1" = every Monday 8am
	NaturalSchedule string    // Sprint 14: human-readable schedule ("every morning at 8am")
	Instruction     string    // what to tell the agent when it fires (legacy — use Goal for planner)
	SessionID       string    // which Slack session to reply to
	Status          string    // active, paused, running
	NextRun         time.Time
	LastRun         *time.Time // Sprint 14: last execution time
	RunCount        int       // Sprint 14: total execution count
	Active          bool      // legacy compat — derived from Status
	CreatedAt       time.Time
	UpdatedAt       time.Time
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
	if job.Status == "" {
		job.Status = "active"
	}
	if job.CreatedAt.IsZero() {
		job.CreatedAt = time.Now()
	}
	job.UpdatedAt = time.Now()

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

// Pause pauses a job by ID.
func (s *Scheduler) Pause(ctx context.Context, id string) error {
	s.mu.Lock()
	j, ok := s.jobs[id]
	if ok {
		j.Status = "paused"
		j.Active = false
		j.UpdatedAt = time.Now()
	}
	s.mu.Unlock()

	if !ok {
		return fmt.Errorf("job %q not found", id)
	}
	return s.store.UpdateStatus(ctx, id, "paused")
}

// Resume resumes a paused job.
func (s *Scheduler) Resume(ctx context.Context, id string) error {
	s.mu.Lock()
	j, ok := s.jobs[id]
	if ok {
		j.Status = "active"
		j.Active = true
		j.UpdatedAt = time.Now()
		// Recompute next run from now.
		parsed, err := ParseCron(j.CronExpr)
		if err == nil {
			j.NextRun = NextCronTime(parsed, time.Now())
			_ = s.store.UpdateNextRun(ctx, id, j.NextRun)
		}
	}
	s.mu.Unlock()

	if !ok {
		return fmt.Errorf("job %q not found", id)
	}
	return s.store.UpdateStatus(ctx, id, "active")
}

// Get returns a single job by ID.
func (s *Scheduler) Get(id string) (Job, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	j, ok := s.jobs[id]
	if !ok {
		return Job{}, false
	}
	return *j, true
}

// FireNow immediately fires a job using the handler. Runs in current goroutine.
func (s *Scheduler) FireNow(ctx context.Context, job Job) {
	instruction := job.Goal
	if instruction == "" {
		instruction = job.Instruction
	}

	s.logger.Info("scheduled job fired (manual)",
		"id", job.ID,
		"name", job.Name,
	)

	s.handler(ctx, job.SessionID, instruction)

	// Update run tracking.
	s.mu.Lock()
	if live, ok := s.jobs[job.ID]; ok {
		live.Status = "active"
		nowDone := time.Now()
		live.LastRun = &nowDone
		live.RunCount++

		parsed, err := ParseCron(live.CronExpr)
		if err == nil {
			live.NextRun = NextCronTime(parsed, nowDone)
			_ = s.store.UpdateNextRun(ctx, live.ID, live.NextRun)
		}
		_ = s.store.MarkRunComplete(ctx, live.ID, nowDone, live.RunCount)
	}
	s.mu.Unlock()
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
		if job.Status == "paused" || job.Status == "running" || !job.Active || now.Before(job.NextRun) {
			continue
		}

		s.logger.Info("scheduled job fired",
			"id", job.ID,
			"name", job.Name,
			"cron", job.CronExpr,
		)

		// Mark as running to prevent double-fire.
		job.Status = "running"
		_ = s.store.UpdateStatus(ctx, job.ID, "running")

		// Fire asynchronously so we don't block the tick loop.
		j := job
		go func() {
			// Use Goal for planner-based execution, fall back to Instruction.
			instruction := j.Goal
			if instruction == "" {
				instruction = j.Instruction
			}
			s.handler(ctx, j.SessionID, instruction)

			// After completion: update run tracking.
			s.mu.Lock()
			if live, ok := s.jobs[j.ID]; ok {
				live.Status = "active"
				nowDone := time.Now()
				live.LastRun = &nowDone
				live.RunCount++

				// Compute next run.
				parsed, err := ParseCron(live.CronExpr)
				if err == nil {
					live.NextRun = NextCronTime(parsed, nowDone)
					_ = s.store.UpdateNextRun(ctx, live.ID, live.NextRun)
				}
				_ = s.store.MarkRunComplete(ctx, live.ID, nowDone, live.RunCount)
			}
			s.mu.Unlock()
		}()
	}
}
