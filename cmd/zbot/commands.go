// Package main — Sprint 17: Slack Command Handler
// All slash commands live here. wire.go calls cmdHandler.Handle() before
// falling through to the conversational agent. Clean separation of concerns.
package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ziloss-tech/zbot/internal/agent"
	"github.com/ziloss-tech/zbot/internal/research"
	"github.com/ziloss-tech/zbot/internal/scheduler"
	"github.com/ziloss-tech/zbot/internal/workflow"
)

// SlackCommands handles all slash command routing.
// Returned bool = true means the message was handled as a command.
type SlackCommands struct {
	orch          *workflow.Orchestrator
	sched         *scheduler.Scheduler
	schedJobStore scheduler.JobStore
	researchOrch  *research.ResearchOrchestrator
	researchStore *research.PGResearchStore
	memStore      agent.MemoryStore
	claimMemory   *research.ClaimMemory
	// Sprint 20: persistent Claude chat handler.
	claudeChat func(ctx context.Context, message, source string) (string, error)
}

// Handle routes a Slack message to the correct command handler.
// Returns (reply, handled). If handled=false, wire.go falls through to the agent.
func (c *SlackCommands) Handle(ctx context.Context, sessionID, text string) (string, bool) {
	t := strings.TrimSpace(text)
	lower := strings.ToLower(t)

	switch {
	// ── Help ────────────────────────────────────────────────────────────────
	case lower == "//help" || lower == "//zbot" || lower == "//zbot help":
		return c.handleHelp(), true

	// ── Research commands ────────────────────────────────────────────────────
	case lower == "//research list" || lower == "//research":
		return c.handleResearchList(ctx), true

	case strings.HasPrefix(lower, "//research status "):
		id := strings.TrimSpace(t[len("//research status "):])
		return c.handleResearchStatus(ctx, id), true

	case strings.HasPrefix(lower, "//research report "):
		id := strings.TrimSpace(t[len("//research report "):])
		return c.handleResearchReport(ctx, id), true

	case strings.HasPrefix(lower, "//research cancel "):
		id := strings.TrimSpace(t[len("//research cancel "):])
		return c.handleResearchCancel(ctx, id), true

	case lower == "//budget" || lower == "//research budget":
		return c.handleBudget(ctx), true

	// ── Memory commands ──────────────────────────────────────────────────────
	case strings.HasPrefix(lower, "//memory "):
		query := strings.TrimSpace(t[len("//memory "):])
		return c.handleMemorySearch(ctx, query), true

	case strings.HasPrefix(lower, "//claims "):
		query := strings.TrimSpace(t[len("//claims "):])
		return c.handleClaimsSearch(ctx, query), true

	// ── Schedule commands ────────────────────────────────────────────────────
	case lower == "//schedule list" || lower == "//schedules":
		return c.handleScheduleList(ctx), true

	case strings.HasPrefix(lower, "//schedule run "):
		id := strings.TrimSpace(t[len("//schedule run "):])
		return c.handleScheduleRun(ctx, id), true

	case strings.HasPrefix(lower, "//schedule pause "):
		id := strings.TrimSpace(t[len("//schedule pause "):])
		return c.handleSchedulePause(ctx, id), true

	case strings.HasPrefix(lower, "//schedule resume "):
		id := strings.TrimSpace(t[len("//schedule resume "):])
		return c.handleScheduleResume(ctx, id), true

	case strings.HasPrefix(lower, "//schedule delete ") || strings.HasPrefix(lower, "//unschedule "):
		id := t
		if strings.HasPrefix(lower, "//schedule delete ") {
			id = strings.TrimSpace(t[len("//schedule delete "):])
		} else {
			id = strings.TrimSpace(t[len("//unschedule "):])
		}
		return c.handleScheduleDelete(ctx, id), true

	// ── Status / cancel workflow ─────────────────────────────────────────────
	case strings.HasPrefix(lower, "//status "):
		wfID := strings.TrimSpace(t[len("//status "):])
		return c.handleWorkflowStatus(ctx, wfID), true

	case strings.HasPrefix(lower, "//cancel "):
		wfID := strings.TrimSpace(t[len("//cancel "):])
		return c.handleWorkflowCancel(ctx, wfID), true

	// ── Sprint 20: Direct Claude chat ───────────────────────────────────────
	case strings.HasPrefix(lower, "//claude "):
		msg := strings.TrimSpace(t[len("//claude "):])
		return c.handleClaudeChat(ctx, msg), true

	case lower == "//claude":
		return "Usage: `//claude <message>` — chat directly with Claude\nExample: `//claude what's the status of my lead sim?`", true
	}

	return "", false
}

// ── HELP ─────────────────────────────────────────────────────────────────────

func (c *SlackCommands) handleHelp() string {
	b := "`"
	return "🤖 *ZBOT Command Reference*\n\n" +
		"*Deep Research*\n" +
		"• " + b + "research: <goal>" + b + " — fire a new research session (5 models)\n" +
		"• " + b + "//research list" + b + " — list recent sessions\n" +
		"• " + b + "//research status <id>" + b + " — check progress\n" +
		"• " + b + "//research report <id>" + b + " — get the full report\n" +
		"• " + b + "//research cancel <id>" + b + " — cancel a running session\n" +
		"• " + b + "//budget" + b + " — today's spend vs daily limit\n\n" +
		"*Memory*\n" +
		"• " + b + "//memory <query>" + b + " — search long-term memory\n" +
		"• " + b + "//claims <query>" + b + " — search verified research claims\n\n" +
		"*Workflows*\n" +
		"• " + b + "plan: <goal>" + b + " — Claude decomposes and executes\n" +
		"• " + b + "//status <id>" + b + " — check workflow progress\n" +
		"• " + b + "//cancel <id>" + b + " — cancel pending tasks\n\n" +
		"*Schedules*\n" +
		"• " + b + "//schedule list" + b + " — all scheduled jobs\n" +
		"• " + b + "//schedule run <id>" + b + " — fire a job right now\n" +
		"• " + b + "//schedule pause <id>" + b + " — pause a job\n" +
		"• " + b + "//schedule resume <id>" + b + " — resume a paused job\n" +
		"• " + b + "//schedule delete <id>" + b + " — remove permanently\n" +
		"• " + b + "//schedule 0 8 * * 1 | Check GHL leads" + b + " — create a new job\n\n" +
		"*Web UI* → http://localhost:18790\n\n" +
		"*Claude Chat*\n" +
		"• " + b + "//claude <message>" + b + " — chat with Claude (syncs to UI)"
}

// ── RESEARCH ─────────────────────────────────────────────────────────────────

func (c *SlackCommands) handleResearchList(ctx context.Context) string {
	if c.researchStore == nil {
		return "❌ Research store not available."
	}
	sessions, err := c.researchStore.ListSessions(ctx, 10)
	if err != nil {
		return fmt.Sprintf("❌ Error: %v", err)
	}
	if len(sessions) == 0 {
		return "No research sessions yet. Start one with: `research: <your goal>`"
	}

	var sb strings.Builder
	sb.WriteString("🔬 *Recent Research Sessions*\n\n")
	for _, s := range sessions {
		icon := statusIcon(s.Status)
		goal := s.Goal
		if len(goal) > 60 {
			goal = goal[:60] + "..."
		}
		conf := ""
		if s.ConfidenceScore > 0 {
			conf = fmt.Sprintf(" · %.0f%% conf", s.ConfidenceScore*100)
		}
		cost := ""
		if s.CostUSD > 0 {
			cost = fmt.Sprintf(" · $%.3f", s.CostUSD)
		}
		running := ""
		if c.researchOrch != nil && c.researchOrch.IsRunning(s.ID) {
			running = " _(running)_"
		}
		sb.WriteString(fmt.Sprintf("%s `%s`%s\n_%s_%s%s\n\n", icon, s.ID[:12], running, goal, conf, cost))
	}
	return sb.String()
}

func (c *SlackCommands) handleResearchStatus(ctx context.Context, id string) string {
	if c.researchStore == nil {
		return "❌ Research store not available."
	}
	s, err := c.researchStore.GetSession(ctx, id)
	if err != nil {
		// Try prefix match — allow short IDs
		sessions, _ := c.researchStore.ListSessions(ctx, 50)
		for _, sess := range sessions {
			if strings.HasPrefix(sess.ID, id) {
				s = &sess
				break
			}
		}
		if s == nil {
			return fmt.Sprintf("❌ Session `%s` not found.", id)
		}
	}

	icon := statusIcon(s.Status)
	running := ""
	if c.researchOrch != nil && c.researchOrch.IsRunning(s.ID) {
		running = " _(actively running)_"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s *Research Session* `%s`%s\n\n", icon, s.ID[:12], running))
	sb.WriteString(fmt.Sprintf("*Goal:* %s\n", s.Goal))
	sb.WriteString(fmt.Sprintf("*Status:* %s | *Iterations:* %d | *Confidence:* %.0f%%\n",
		s.Status, s.Iterations, s.ConfidenceScore*100))
	sb.WriteString(fmt.Sprintf("*Cost:* $%.4f | *Started:* %s\n",
		s.CostUSD, s.CreatedAt.Format("Jan 2 15:04")))

	if s.Error != "" {
		sb.WriteString(fmt.Sprintf("*Error:* %s\n", s.Error))
	}
	if s.Status == "completed" {
		sb.WriteString(fmt.Sprintf("\nUse `/research report %s` to get the full report.", s.ID[:12]))
	}
	return sb.String()
}

func (c *SlackCommands) handleResearchReport(ctx context.Context, id string) string {
	if c.researchStore == nil {
		return "❌ Research store not available."
	}
	s, err := c.researchStore.GetSession(ctx, id)
	if err != nil {
		// Try prefix match
		sessions, _ := c.researchStore.ListSessions(ctx, 50)
		for _, sess := range sessions {
			if strings.HasPrefix(sess.ID, id) {
				s = &sess
				break
			}
		}
		if s == nil {
			return fmt.Sprintf("❌ Session `%s` not found.", id)
		}
	}

	if s.Status != "completed" {
		return fmt.Sprintf("⏳ Session `%s` is `%s` — report not ready yet.\nIter: %d | Conf: %.0f%%",
			s.ID[:12], s.Status, s.Iterations, s.ConfidenceScore*100)
	}

	report := s.FinalReport
	if report == "" {
		return fmt.Sprintf("❓ Session `%s` completed but has no report stored.", s.ID[:12])
	}

	// Slack message limit is ~4000 chars for a DM. Split if needed.
	header := fmt.Sprintf("📄 *Research Report* — `%s`\n_%s_\n\n", s.ID[:12], s.Goal)
	full := header + report

	if len(full) <= 3800 {
		return full
	}

	// Too long for one message — send first ~3500 chars + note
	truncated := full[:3500]
	// Find last newline to avoid mid-sentence cut
	if idx := strings.LastIndex(truncated, "\n"); idx > 2000 {
		truncated = truncated[:idx]
	}
	return truncated + fmt.Sprintf("\n\n_[Report truncated — %d chars total. Full report in UI: http://localhost:18790]_", len(report))
}

func (c *SlackCommands) handleResearchCancel(ctx context.Context, id string) string {
	if c.researchOrch == nil {
		return "❌ Research orchestrator not available."
	}

	// Try exact then prefix
	cancelled := c.researchOrch.CancelSession(id)
	if !cancelled {
		// Try prefix match against running sessions
		if c.researchStore != nil {
			sessions, _ := c.researchStore.ListSessions(ctx, 20)
			for _, s := range sessions {
				if strings.HasPrefix(s.ID, id) && c.researchOrch.IsRunning(s.ID) {
					cancelled = c.researchOrch.CancelSession(s.ID)
					if cancelled {
						id = s.ID
						break
					}
				}
			}
		}
	}

	if !cancelled {
		return fmt.Sprintf("❓ No active session found matching `%s`.\nUse `/research list` to see running sessions.", id)
	}

	// Mark as failed in DB
	if c.researchStore != nil {
		_ = c.researchStore.FailSession(ctx, id, "cancelled by user via Slack")
	}

	return fmt.Sprintf("🛑 Research session `%s` cancelled.", id[:min(12, len(id))])
}

func (c *SlackCommands) handleBudget(ctx context.Context) string {
	if c.researchStore == nil {
		return "❌ Research store not available."
	}
	totalCost, sessionCount, err := c.researchStore.GetDailyStats(ctx)
	if err != nil {
		return fmt.Sprintf("❌ Budget query failed: %v", err)
	}
	remaining := research.DailyBudgetUSD - totalCost
	pct := 0.0
	if research.DailyBudgetUSD > 0 {
		pct = (totalCost / research.DailyBudgetUSD) * 100
	}

	bar := budgetBar(pct)

	return fmt.Sprintf("💰 *Today's Research Budget*\n\n%s\n*Spent:* $%.3f / $%.2f (%.0f%%)\n*Sessions:* %d\n*Remaining:* $%.3f",
		bar, totalCost, research.DailyBudgetUSD, pct, sessionCount, remaining)
}

// ── MEMORY ───────────────────────────────────────────────────────────────────

func (c *SlackCommands) handleMemorySearch(ctx context.Context, query string) string {
	if c.memStore == nil {
		return "❌ Memory store not available."
	}
	if query == "" {
		return "Usage: `/memory <query>` — e.g. `/memory GoHighLevel pricing`"
	}

	facts, err := c.memStore.Search(ctx, query, 8)
	if err != nil {
		return fmt.Sprintf("❌ Memory search failed: %v", err)
	}
	if len(facts) == 0 {
		return fmt.Sprintf("🧠 Nothing found in memory for: _%s_", query)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("🧠 *Memory — \"%s\"* (%d results)\n\n", query, len(facts)))
	for i, f := range facts {
		score := ""
		if f.Score > 0 {
			score = fmt.Sprintf(" _(%.0f%% match)_", f.Score*100)
		}
		content := f.Content
		if len(content) > 200 {
			content = content[:200] + "..."
		}
		sb.WriteString(fmt.Sprintf("%d. %s%s\n", i+1, content, score))
		if f.Source != "" && f.Source != "general" {
			sb.WriteString(fmt.Sprintf("   _source: %s_\n", f.Source))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func (c *SlackCommands) handleClaimsSearch(ctx context.Context, query string) string {
	if c.claimMemory == nil {
		return "❌ Claim memory not available (Sprint 16)."
	}
	if query == "" {
		return "Usage: `/claims <query>` — searches verified research claims\ne.g. `/claims HubSpot pricing`"
	}

	prior, err := c.claimMemory.SearchPriorKnowledge(ctx, query)
	if err != nil {
		return fmt.Sprintf("❌ Claims search failed: %v", err)
	}
	if prior == "" {
		return fmt.Sprintf("🔍 No claims found matching: _%s_\n\nRun a research session to build claim memory: `research: %s`", query, query)
	}

	return fmt.Sprintf("📋 *Research Claims — \"%s\"*\n\n%s", query, prior)
}

// ── SCHEDULES ────────────────────────────────────────────────────────────────

func (c *SlackCommands) handleScheduleList(ctx context.Context) string {
	if c.sched == nil {
		return "❌ Scheduler not available."
	}
	jobs := c.sched.List()
	if len(jobs) == 0 {
		return "No scheduled jobs.\n\nCreate one: `/schedule 0 8 * * 1 | Check GHL leads`"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("⏰ *Scheduled Jobs* (%d)\n\n", len(jobs)))
	for _, j := range jobs {
		icon := "✅"
		if !j.Active {
			icon = "⏸️"
		}
		name := j.Name
		if name == "" {
			name = truncateStr(j.Instruction, 50)
		}
		sb.WriteString(fmt.Sprintf("%s `%s` — *%s*\n", icon, j.ID, name))
		sb.WriteString(fmt.Sprintf("   _Cron:_ `%s` | _Next:_ %s | _Runs:_ %d\n\n",
			j.CronExpr,
			j.NextRun.Format("Jan 2 15:04 MST"),
			j.RunCount,
		))
	}
	return sb.String()
}

func (c *SlackCommands) handleScheduleRun(ctx context.Context, id string) string {
	if c.sched == nil {
		return "❌ Scheduler not available."
	}
	job, ok := c.sched.Get(id)
	if !ok {
		// Try prefix match
		jobs := c.sched.List()
		for _, j := range jobs {
			if strings.HasPrefix(j.ID, id) {
				job = j
				ok = true
				break
			}
		}
		if !ok {
			return fmt.Sprintf("❌ Job `%s` not found. Use `/schedule list` to see all.", id)
		}
	}
	go c.sched.FireNow(ctx, job)
	return fmt.Sprintf("🚀 Job `%s` triggered.\n_%s_\n\nWatch for completion DM.", job.ID, job.Name)
}

func (c *SlackCommands) handleSchedulePause(ctx context.Context, id string) string {
	if c.sched == nil {
		return "❌ Scheduler not available."
	}
	if err := c.sched.Pause(ctx, id); err != nil {
		return fmt.Sprintf("❌ Pause failed: %v", err)
	}
	return fmt.Sprintf("⏸️ Job `%s` paused.", id)
}

func (c *SlackCommands) handleScheduleResume(ctx context.Context, id string) string {
	if c.sched == nil {
		return "❌ Scheduler not available."
	}
	if err := c.sched.Resume(ctx, id); err != nil {
		return fmt.Sprintf("❌ Resume failed: %v", err)
	}
	return fmt.Sprintf("▶️ Job `%s` resumed.", id)
}

func (c *SlackCommands) handleScheduleDelete(ctx context.Context, id string) string {
	if c.sched == nil {
		return "❌ Scheduler not available."
	}
	if err := c.sched.Remove(ctx, id); err != nil {
		return fmt.Sprintf("❌ Delete failed: %v", err)
	}
	return fmt.Sprintf("🗑️ Job `%s` deleted.", id)
}

// ── WORKFLOWS ────────────────────────────────────────────────────────────────

func (c *SlackCommands) handleWorkflowStatus(ctx context.Context, wfID string) string {
	if c.orch == nil {
		return "❌ Workflow engine not available."
	}
	tasks, err := c.orch.Status(ctx, wfID)
	if err != nil {
		return fmt.Sprintf("❌ Could not get status for `%s`: %v", wfID, err)
	}
	done := 0
	for _, t := range tasks {
		if t.Status == agent.TaskDone {
			done++
		}
	}
	sum := &workflow.WorkflowSummary{WorkflowID: wfID, Tasks: tasks, Done: done, Total: len(tasks)}
	return sum.Format()
}

func (c *SlackCommands) handleWorkflowCancel(ctx context.Context, wfID string) string {
	if c.orch == nil {
		return "❌ Workflow engine not available."
	}
	if err := c.orch.Cancel(ctx, wfID); err != nil {
		return fmt.Sprintf("❌ Cancel failed: %v", err)
	}
	return fmt.Sprintf("🚫 Workflow `%s` — pending tasks cancelled.", wfID)
}

// ── HELPERS ──────────────────────────────────────────────────────────────────

func statusIcon(status string) string {
	switch status {
	case "completed":
		return "✅"
	case "running":
		return "⏳"
	case "failed":
		return "❌"
	default:
		return "⚪"
	}
}

func budgetBar(pct float64) string {
	filled := int(pct / 10)
	if filled > 10 {
		filled = 10
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", 10-filled)
	return fmt.Sprintf("`[%s]`", bar)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Ensure time is imported (used in schedule list format).
var _ = time.Now

// ── Sprint 20: CLAUDE CHAT ───────────────────────────────────────────────────

func (c *SlackCommands) handleClaudeChat(ctx context.Context, message string) string {
	if c.claudeChat == nil {
		return "❌ Claude chat not available."
	}
	if message == "" {
		return "Usage: `//claude <message>`\nExample: `//claude what's the status of my lead sim?`"
	}
	reply, err := c.claudeChat(ctx, message, "slack")
	if err != nil {
		return fmt.Sprintf("❌ Claude error: %v", err)
	}
	return "🤖 *Claude:* " + reply
}
