// Package main — Message handler for Slack/webhook/web gateway.
// Extracted from wire.go for readability. Pure refactoring — zero behavior change.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/ziloss-tech/zbot/internal/agent"
	"github.com/ziloss-tech/zbot/internal/gateway"
	"github.com/ziloss-tech/zbot/internal/memory"
	"github.com/ziloss-tech/zbot/internal/research"
	"github.com/ziloss-tech/zbot/internal/scheduler"
	"github.com/ziloss-tech/zbot/internal/security"
	"github.com/ziloss-tech/zbot/internal/workflow"
)

// MessageHandler holds all dependencies needed to process an incoming message.
// This replaces the 240-line closure in wire.go with a testable struct.
type MessageHandler struct {
	logger        *slog.Logger
	ag            *agent.Agent
	confirmStore  *security.ConfirmationStore
	cmdHandler    *SlackCommands
	orch          *workflow.Orchestrator
	researchOrch  *research.ResearchOrchestrator
	researchStore *research.PGResearchStore
	sched         *scheduler.Scheduler
	memStore      agent.MemoryStore

	histMu  sync.Mutex
	history map[string][]agent.Message
}

// Handle processes an incoming message from any gateway (Slack, webhook, web).
func (h *MessageHandler) Handle(ctx context.Context, sessionID, userID, text string, attachments []gateway.Attachment) (string, error) {
	h.logger.Info("message received",
		"session", sessionID,
		"user", userID,
		"text_len", len(text),
		"attachments", len(attachments),
	)

	trimmed := strings.TrimSpace(text)

	// ── Prompt injection detection + sanitization ──────────────────────
	if security.IsLikelyInjection(trimmed, h.logger, sessionID, userID) {
		trimmed = security.SanitizeInput(trimmed)
		text = trimmed
	}

	// ── Handle pending destructive operation confirmations ─────────────
	if h.confirmStore.HasPending(sessionID) {
		if security.IsConfirmation(trimmed) {
			pending := h.confirmStore.GetPending(sessionID)
			if pending != nil {
				tool, ok := h.ag.GetTool(pending.ToolName)
				if ok {
					result, execErr := tool.Execute(ctx, pending.Input)
					if execErr != nil {
						return fmt.Sprintf("❌ Execution failed: %v", execErr), nil
					}
					return fmt.Sprintf("✅ Executed *%s*:\n%s", pending.ToolName, result.Content), nil
				}
				return "❌ Tool no longer available.", nil
			}
		}
		if security.IsCancellation(trimmed) {
			h.confirmStore.GetPending(sessionID)
			return "🚫 Action cancelled.", nil
		}
		h.confirmStore.GetPending(sessionID)
	}

	// ── Route slash commands ──────────────────────────────────────────
	if reply, handled := h.cmdHandler.Handle(ctx, sessionID, trimmed); handled {
		return reply, nil
	}

	// ── plan: <goal> — single-brain workflow decomposition ────────────
	if strings.HasPrefix(trimmed, "plan: ") && h.orch != nil {
		goal := strings.TrimSpace(strings.TrimPrefix(trimmed, "plan: "))
		if goal == "" {
			return "Usage: `plan: <goal>`\nExample: `plan: research top 5 GoHighLevel competitors and write a comparison report`", nil
		}
		h.logger.Info("plan requested (v2 single-brain)", "goal", goal)
		wfID, submitErr := h.orch.Submit(ctx, sessionID, goal)
		if submitErr != nil {
			return fmt.Sprintf("❌ Failed to plan + submit: %v", submitErr), nil
		}
		return fmt.Sprintf("🧠 *Claude decomposed and started workflow:*\n\n🚀 Workflow `%s` started.\nTrack progress: `//status %s`", wfID, wfID), nil
	}

	// ── research: <goal> — deep multi-model research pipeline ────────
	if strings.HasPrefix(trimmed, "research: ") {
		goal := strings.TrimSpace(strings.TrimPrefix(trimmed, "research: "))
		if goal == "" {
			return "Usage: `research: <goal>`\nExample: `research: what are the top GoHighLevel competitors and how do they compare?`", nil
		}
		if h.researchOrch == nil {
			return "❌ Deep Research not available — needs OpenRouter + Postgres.", nil
		}
		resID := "res_" + randomID()
		if h.researchStore != nil {
			_ = h.researchStore.CreateSession(ctx, resID, goal)
		}
		go func() {
			bgCtx := context.Background()
			state, resErr := h.researchOrch.RunDeepResearch(bgCtx, goal, resID)
			if resErr != nil {
				h.logger.Error("deep research failed", "session_id", resID, "err", resErr)
				if h.researchStore != nil {
					_ = h.researchStore.FailSession(bgCtx, resID, resErr.Error())
				}
				return
			}
			if h.researchStore != nil {
				_ = h.researchStore.CompleteSession(bgCtx, resID, state.FinalReport, state)
			}
			h.logger.Info("deep research completed", "session_id", resID, "iterations", state.Iteration, "cost", fmt.Sprintf("$%.4f", state.CostUSD))
		}()
		return fmt.Sprintf("🔬 *Deep Research started* — `%s`\nSession: `%s`\n\n5 AI models collaborating. Track: `/research status %s`\nUI: http://localhost:18790", goal, resID, resID[:12]), nil
	}

	// ── plan: without workflow engine ──────────────────────────────────
	if strings.HasPrefix(trimmed, "plan: ") && h.orch == nil {
		return "❌ Workflow engine not available — Postgres required.", nil
	}

	// ── //status <workflow_id> — check workflow progress ─────────────
	if strings.HasPrefix(trimmed, "//status ") && h.orch != nil {
		wfID := strings.TrimSpace(strings.TrimPrefix(trimmed, "//status "))
		tasks, err := h.orch.Status(ctx, wfID)
		if err != nil {
			return fmt.Sprintf("❌ Could not get workflow status: %v", err), nil
		}
		if len(tasks) == 0 {
			return fmt.Sprintf("No tasks found for workflow `%s`.", wfID), nil
		}
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("📋 *Workflow `%s`* — %d tasks:\n", wfID, len(tasks)))
		for _, t := range tasks {
			icon := "⏳"
			switch t.Status {
			case agent.TaskDone:
				icon = "✅"
			case agent.TaskRunning:
				icon = "🔄"
			case agent.TaskFailed:
				icon = "❌"
			case agent.TaskCanceled:
				icon = "🚫"
			}
			sb.WriteString(fmt.Sprintf("%s Step %d: %s — _%s_\n", icon, t.Step, t.Name, t.Status))
		}
		return sb.String(), nil
	}

	// ── //workflow <instruction> — submit a multi-step workflow ────────
	if (strings.HasPrefix(trimmed, "//workflow ") || isWorkflowRequest(trimmed)) && h.orch != nil {
		instruction := trimmed
		if strings.HasPrefix(trimmed, "//workflow ") {
			instruction = strings.TrimSpace(strings.TrimPrefix(trimmed, "//workflow "))
		}
		wfID, err := h.orch.Submit(ctx, sessionID, instruction)
		if err != nil {
			return fmt.Sprintf("❌ Failed to start workflow: %v", err), nil
		}
		return fmt.Sprintf("🚀 Workflow `%s` started — use `//status %s` to check progress.", wfID, wfID), nil
	}

	// ── //schedule <cron> | <instruction> — add a scheduled job ───────
	if strings.HasPrefix(trimmed, "//schedule ") && h.sched != nil {
		parts := strings.SplitN(strings.TrimPrefix(trimmed, "//schedule "), " | ", 2)
		if len(parts) != 2 {
			return "Usage: `/schedule <cron_expr> | <instruction>`\nExample: `/schedule 0 8 * * 1 | Check open GHL leads`", nil
		}
		cronExpr := strings.TrimSpace(parts[0])
		instruction := strings.TrimSpace(parts[1])
		id := randomID()
		err := h.sched.Add(ctx, scheduler.Job{
			ID:          id,
			Name:        truncateStr(instruction, 60),
			CronExpr:    cronExpr,
			Instruction: instruction,
			SessionID:   sessionID,
		})
		if err != nil {
			return fmt.Sprintf("❌ Schedule failed: %v", err), nil
		}
		return fmt.Sprintf("⏰ Scheduled `%s` — cron: `%s`\nUse `/schedule list` to see all.", id, cronExpr), nil
	}

	// ── Process attachments: images → multimodal, PDFs → text ─────────
	var images []agent.ImageAttachment
	pdfText := ""

	for _, att := range attachments {
		switch att.MediaType {
		case "image/jpeg", "image/png", "image/gif", "image/webp":
			images = append(images, agent.ImageAttachment{
				Data:      att.Data,
				MediaType: att.MediaType,
			})
		case "application/pdf":
			extracted, pdfErr := extractPDF(att.Data)
			if pdfErr == nil && extracted != "" {
				pdfText += "\n\n[PDF: " + att.Filename + "]\n" + extracted
			} else if pdfErr != nil {
				h.logger.Warn("PDF extraction failed", "file", att.Filename, "err", pdfErr)
			}
		}
	}

	// Build user message.
	userMsg := agent.Message{
		Role:      agent.RoleUser,
		SessionID: sessionID,
		Content:   text + pdfText,
		Images:    images,
		CreatedAt: time.Now(),
	}

	// Get conversation history for this session.
	h.histMu.Lock()
	hist := h.history[sessionID]
	h.histMu.Unlock()

	// Run the agent.
	input := agent.TurnInput{
		SessionID: sessionID,
		History:   hist,
		UserMsg:   userMsg,
	}

	output, err := h.ag.Run(ctx, input)
	if err != nil {
		return "", fmt.Errorf("agent.Run: %w", err)
	}

	// Auto-save substantial replies to long-term memory.
	if pgStore, ok := h.memStore.(*memory.Store); ok {
		pgStore.AutoSave(ctx, sessionID, output.Reply)
	}

	// Update conversation history.
	h.histMu.Lock()
	h.history[sessionID] = append(h.history[sessionID], userMsg, agent.Message{
		Role:      agent.RoleAssistant,
		SessionID: sessionID,
		Content:   output.Reply,
		CreatedAt: time.Now(),
	})
	if len(h.history[sessionID]) > 50 {
		h.history[sessionID] = h.history[sessionID][len(h.history[sessionID])-50:]
	}
	h.histMu.Unlock()

	h.logger.Info("agent turn complete",
		"session", sessionID,
		"tokens", output.TokensUsed,
		"tools", output.ToolsInvoked,
		"reply_len", len(output.Reply),
	)

	return output.Reply, nil
}
