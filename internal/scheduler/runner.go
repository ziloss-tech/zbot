// Sprint 14: Runner connects the scheduler to the planner+orchestrator.
// When a scheduled job fires, the runner submits the goal to the planner,
// creates a workflow, and optionally sends results via Slack.
package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// Planner plans a goal into a task graph (mirrors planner.Planner interface).
type Planner interface {
	Plan(ctx context.Context, goal string) (PlanResult, error)
}

// PlanResult holds a planner's output — the task graph.
type PlanResult struct {
	Tasks []PlannedTask
}

// PlannedTask is a single task in the planned graph.
type PlannedTask struct {
	Title       string
	Instruction string
	DependsOn   []string
	Parallel    bool
	ToolHints   []string
	Priority    int
}

// WorkflowSubmitter can submit a plan to the orchestrator.
type WorkflowSubmitter interface {
	SubmitPlan(ctx context.Context, sessionID string, tasks []PlannedTask) (string, error)
	WaitForCompletion(ctx context.Context, workflowID string, timeout time.Duration) (string, error)
}

// SlackNotifier sends formatted messages to Slack.
type SlackNotifier interface {
	SendScheduledResult(ctx context.Context, channelID, jobName, summary string, taskCount int, durationSec int, files []string) error
}

// Runner is the bridge between the scheduler and the planner+orchestrator.
type Runner struct {
	sched             *Scheduler
	store             JobStore
	planner           Planner
	submitter         WorkflowSubmitter
	slack             SlackNotifier
	logger            *slog.Logger
	interval          time.Duration
	fallbackChannelID string // used when job.SessionID is empty (web-UI created jobs)
}

// RunnerConfig holds optional configuration for the Runner.
type RunnerConfig struct {
	CheckInterval time.Duration // how often to check for due jobs, default 30s
}

// NewRunner creates a new scheduler runner.
func NewRunner(sched *Scheduler, store JobStore, logger *slog.Logger) *Runner {
	return &Runner{
		sched:    sched,
		store:    store,
		logger:   logger,
		interval: 30 * time.Second,
	}
}

// SetPlanner wires the planner for dual-brain scheduled execution.
func (r *Runner) SetPlanner(p Planner) {
	r.planner = p
}

// SetSubmitter wires the workflow submitter.
func (r *Runner) SetSubmitter(s WorkflowSubmitter) {
	r.submitter = s
}

// SetSlackNotifier wires Slack notifications for completed jobs.
func (r *Runner) SetSlackNotifier(s SlackNotifier) {
	r.slack = s
}

// SetFallbackChannel sets the channel/DM ID to use for jobs that have no SessionID
// (i.e. jobs created from the web UI rather than via a Slack command).
func (r *Runner) SetFallbackChannel(channelID string) {
	r.fallbackChannelID = channelID
}

// Start begins the runner background loop. It replaces the default scheduler handler
// with planner+orchestrator execution when those are available.
func (r *Runner) Start(ctx context.Context) {
	r.logger.Info("scheduler runner started — checking every 30s")
	go r.loop(ctx)
}

func (r *Runner) loop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			r.logger.Info("scheduler runner stopped")
			return
		case <-time.After(r.interval):
			r.checkDueJobs(ctx)
		}
	}
}

func (r *Runner) checkDueJobs(ctx context.Context) {
	jobs, err := r.store.Load(ctx)
	if err != nil {
		r.logger.Error("runner: failed to load jobs", "err", err)
		return
	}

	now := time.Now()
	for i := range jobs {
		job := jobs[i]

		if job.Status != "active" || now.Before(job.NextRun) {
			continue
		}

		// Missed run recovery: if lastRun is more than 2 intervals ago, fire once and update.
		// Do NOT fire multiple times for missed runs.
		r.logger.Info("scheduled job fired",
			"id", job.ID,
			"name", job.Name,
			"cron", job.CronExpr,
		)

		// Mark as running.
		_ = r.store.UpdateStatus(ctx, job.ID, "running")

		go r.executeJob(ctx, job)
	}
}

func (r *Runner) executeJob(ctx context.Context, job Job) {
	startTime := time.Now()

	goal := job.Goal
	if goal == "" {
		goal = job.Instruction
	}

	var summary string
	var taskCount int

	// If planner + submitter are wired, use dual-brain execution.
	if r.planner != nil && r.submitter != nil {
		planResult, planErr := r.planner.Plan(ctx, goal)
		if planErr != nil {
			r.logger.Error("runner: planning failed",
				"job_id", job.ID,
				"err", planErr,
			)
			summary = fmt.Sprintf("Planning failed: %v", planErr)
			r.finishJob(ctx, job, startTime, summary, 0, nil)
			return
		}

		taskCount = len(planResult.Tasks)

		wfID, submitErr := r.submitter.SubmitPlan(ctx, job.SessionID, planResult.Tasks)
		if submitErr != nil {
			r.logger.Error("runner: workflow submission failed",
				"job_id", job.ID,
				"err", submitErr,
			)
			summary = fmt.Sprintf("Workflow submission failed: %v", submitErr)
			r.finishJob(ctx, job, startTime, summary, taskCount, nil)
			return
		}

		r.logger.Info("runner: workflow submitted",
			"job_id", job.ID,
			"workflow_id", wfID,
		)

		// Wait for completion (max 10 minutes for scheduled jobs).
		result, waitErr := r.submitter.WaitForCompletion(ctx, wfID, 10*time.Minute)
		if waitErr != nil {
			summary = fmt.Sprintf("Workflow timed out or failed: %v", waitErr)
		} else {
			summary = result
		}
	} else {
		// Fallback: fire via the scheduler's handler (direct agent.Run).
		r.logger.Info("runner: using legacy handler (no planner/submitter wired)",
			"job_id", job.ID,
		)
		// The scheduler's fireDue already handles legacy execution.
		summary = "Executed via legacy handler"
	}

	r.finishJob(ctx, job, startTime, summary, taskCount, nil)
}

func (r *Runner) finishJob(ctx context.Context, job Job, startTime time.Time, summary string, taskCount int, files []string) {
	duration := time.Since(startTime)
	nowDone := time.Now()

	// Update DB: mark complete, update last_run, increment run_count.
	_ = r.store.MarkRunComplete(ctx, job.ID, nowDone, job.RunCount+1)

	// Compute next run.
	parsed, err := ParseCron(job.CronExpr)
	if err == nil {
		nextRun := NextCronTime(parsed, nowDone)
		_ = r.store.UpdateNextRun(ctx, job.ID, nextRun)
	}

	r.logger.Info("scheduled job complete",
		"job_id", job.ID,
		"name", job.Name,
		"duration", duration.Round(time.Second),
		"tasks", taskCount,
	)

	// Send Slack notification if wired.
	// Use job.SessionID (set when job was created via Slack DM) or fall back to
	// fallbackChannelID (set from wire.go for jobs created via the web UI).
	if r.slack != nil {
		channelID := job.SessionID
		if channelID == "" {
			channelID = r.fallbackChannelID
		}
		if channelID != "" {
			truncSummary := summary
			if len(truncSummary) > 500 {
				truncSummary = truncSummary[:500] + "..."
			}
			durationSec := int(duration.Seconds())
			if notifyErr := r.slack.SendScheduledResult(ctx, channelID, job.Name, truncSummary, taskCount, durationSec, files); notifyErr != nil {
				r.logger.Error("runner: Slack notification failed",
					"job_id", job.ID,
					"channel", channelID,
					"err", notifyErr,
				)
			} else {
				r.logger.Info("runner: Slack notification sent",
					"job_id", job.ID,
					"channel", channelID,
				)
			}
		}
	}
}

// FormatSlackMessage creates a formatted Slack message for a scheduled job result.
func FormatSlackMessage(jobName, summary string, taskCount int, duration time.Duration, files []string) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("🤖 *Scheduled: %s*\n", jobName))
	sb.WriteString(fmt.Sprintf("_Ran at %s • %d tasks • %s_\n\n",
		time.Now().Format("3:04 PM"),
		taskCount,
		duration.Round(time.Second),
	))

	if summary != "" {
		sb.WriteString(summary)
		sb.WriteString("\n")
	}

	if len(files) > 0 {
		sb.WriteString("\n")
		for _, f := range files {
			sb.WriteString(fmt.Sprintf("📄 `%s`\n", f))
		}
	}

	sb.WriteString("\n[View in ZBOT UI](http://localhost:18790)")

	return sb.String()
}
