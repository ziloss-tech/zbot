// Package webui serves the ZBOT web dashboard.
package webui

import (
	"context"
	"embed"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/zbot-ai/zbot/internal/agent"
	"github.com/zbot-ai/zbot/internal/research"
	"github.com/zbot-ai/zbot/internal/scheduler"
	"github.com/zbot-ai/zbot/internal/workflow"
)

// ResearchSlackNotifier sends a Slack message when deep research completes.
type ResearchSlackNotifier interface {
	Send(ctx context.Context, channelID, content string) error
}

//go:embed static/*
var staticFiles embed.FS

// QuickChatFunc handles non-plan messages with memory context.
type QuickChatFunc func(ctx context.Context, message string) (string, error)

// PersistentChatFunc calls Claude with full conversation history.
type PersistentChatFunc func(ctx context.Context, history []ChatMessage, message string) (string, error)

// Server serves the ZBOT web dashboard on loopback only.
type Server struct {
	port      int
	db        *pgxpool.Pool
	logger    *slog.Logger
	mux       *http.ServeMux
	hub       *Hub
	orch      *workflow.Orchestrator
	memStore  agent.MemoryStore // Sprint 12: memory panel + quick chat
	quickChat QuickChatFunc     // Sprint 12: memory-aware quick chat handler
	sched     *scheduler.Scheduler // Sprint 14: scheduler for schedule panel
	jobStore  scheduler.JobStore   // Sprint 14: direct DB access for schedule API
	llmClient agent.LLMClient      // Sprint 14: for ParseSchedule NL→cron
	researchOrch    *research.ResearchOrchestrator // Deep Research: multi-agent pipeline
	researchStore   *research.PGResearchStore     // Deep Research: session persistence
	slackNotifier   ResearchSlackNotifier         // Deep Research: Slack notify on completion
	notifyChannelID string                        // Deep Research: channel/DM to notify
	// Sprint 20: Persistent Claude chat (Slack ↔ UI).
	chatStore      *ChatStore
	eventBus       agent.EventBus
	persistentChat PersistentChatFunc
}

// New creates a web UI server on port 18790.
func New(db *pgxpool.Pool, logger *slog.Logger) *Server {
	s := &Server{
		port:   18790,
		db:     db,
		logger: logger,
		mux:    http.NewServeMux(),
		hub:    NewHub(),
	}
	s.routes()
	return s
}

// SetOrchestrator sets the orchestrator for workflow management.
func (s *Server) SetOrchestrator(o *workflow.Orchestrator) {
	s.orch = o
}

// SetMemoryStore sets the memory store for the memory panel API.
func (s *Server) SetMemoryStore(mem agent.MemoryStore) {
	s.memStore = mem
}

// SetQuickChat sets the handler for memory-aware quick chat messages.
func (s *Server) SetQuickChat(fn QuickChatFunc) {
	s.quickChat = fn
}

// SetEventBus sets the event bus for real-time SSE streaming to the UI.
func (s *Server) SetEventBus(eb agent.EventBus) {
	s.eventBus = eb
}

// SetScheduler sets the scheduler for the schedule panel API.
func (s *Server) SetScheduler(sc *scheduler.Scheduler, store scheduler.JobStore) {
	s.sched = sc
	s.jobStore = store
}

// SetLLMClient sets the LLM client for NL→cron parsing.
func (s *Server) SetLLMClient(c agent.LLMClient) {
	s.llmClient = c
}

// SetResearch sets the deep research orchestrator and store.
func (s *Server) SetResearch(orch *research.ResearchOrchestrator, store *research.PGResearchStore) {
	s.researchOrch = orch
	s.researchStore = store
}

// SetSlackNotifier wires up Slack DM notification when research completes.
func (s *Server) SetSlackNotifier(n ResearchSlackNotifier, channelID string) {
	s.slackNotifier = n
	s.notifyChannelID = channelID
	s.logger.Info("research Slack notifier wired", "channel", channelID)
}

// SetChatStore wires the persistent chat store (Sprint 20).
func (s *Server) SetChatStore(cs *ChatStore) {
	s.chatStore = cs
}

// SetPersistentChat wires the Claude chat function with history (Sprint 20).
func (s *Server) SetPersistentChat(fn PersistentChatFunc) {
	s.persistentChat = fn
}

// Hub returns the SSE hub for external event publishing (e.g., from orchestrator).
func (s *Server) Hub() *Hub {
	return s.hub
}

// StartMetricsCollector subscribes to the event bus and accumulates
// in-memory metrics from turn_complete events. Call this after SetEventBus.
func (s *Server) StartMetricsCollector(ctx context.Context) {
	if s.eventBus == nil {
		return
	}
	ch := s.eventBus.Subscribe("web-chat")
	go func() {
		defer s.eventBus.Unsubscribe("web-chat", ch)
		for {
			select {
			case <-ctx.Done():
				return
			case evt, ok := <-ch:
				if !ok {
					return
				}
				if string(evt.Type) == "turn_complete" && evt.Detail != nil {
					inputTokens, _ := evt.Detail["input_tokens"].(float64)
					outputTokens, _ := evt.Detail["output_tokens"].(float64)
					costUSD, _ := evt.Detail["cost_usd"].(float64)
					RecordTurn(int(inputTokens), int(outputTokens), costUSD)
				}
			}
		}
	}()
	s.logger.Info("in-memory metrics collector started")
}

// routes registers all HTTP handlers.
func (s *Server) routes() {
	// Static assets.
	s.mux.Handle("/static/", http.FileServer(http.FS(staticFiles)))

	// Pages (existing dashboard).
	s.mux.HandleFunc("/conversations", s.handleConversations)
	s.mux.HandleFunc("/conversations/", s.handleConversationDetail)
	s.mux.HandleFunc("/memory", s.handleMemory)
	s.mux.HandleFunc("/memory/", s.handleMemoryAction)
	s.mux.HandleFunc("/workflows", s.handleWorkflows)
	s.mux.HandleFunc("/workflows/", s.handleWorkflowAction)
	s.mux.HandleFunc("/audit", s.handleAudit)
	s.mux.HandleFunc("/api/stats", s.handleAPIStats)

	// Sprint 11 — Dual Brain Command Center API.
	s.mux.HandleFunc("/api/stream/", s.handleSSEStream)
	s.mux.HandleFunc("/api/plan", s.handlePlanAPI)
	s.mux.HandleFunc("/api/workflows/list", s.handleWorkflowsAPI)
	s.mux.HandleFunc("/api/workflow/", s.handleWorkflowDetailAPI)
	s.mux.HandleFunc("/api/metrics", s.handleMetricsAPI)
	s.mux.HandleFunc("/api/file", s.handleFilePreview)

	// Sprint 12 — Memory Panel API.
	s.mux.HandleFunc("/api/memories", s.handleMemoriesAPI)
	s.mux.HandleFunc("/api/memory/", s.handleMemoryDeleteAPI)

	// Sprint 13 — Workspace File Panel API.
	s.mux.HandleFunc("/api/workspace", s.handleWorkspaceAPI)
	s.mux.HandleFunc("/api/workspace/download", s.handleWorkspaceDownloadAPI)
	s.mux.HandleFunc("/api/workspace/preview", s.handleWorkspacePreviewAPI)
	s.mux.HandleFunc("/api/workspace/file", s.handleWorkspaceDeleteAPI)

	// Sprint 14 — Schedule API.
	s.mux.HandleFunc("/api/schedule", s.handleScheduleCreateAPI)
	s.mux.HandleFunc("/api/schedules", s.handleScheduleListAPI)
	s.mux.HandleFunc("/api/schedule/", s.handleScheduleActionAPI)

	// Sprint 14 — Monitor API.
	s.mux.HandleFunc("/api/monitor", s.handleMonitorCreateAPI)
	s.mux.HandleFunc("/api/monitors", s.handleMonitorListAPI)
	s.mux.HandleFunc("/api/monitor/", s.handleMonitorDeleteAPI)

	// Deep Research API.
	s.mux.HandleFunc("/api/research", s.handleResearchCreateAPI)
	s.mux.HandleFunc("/api/research/list", s.handleResearchListAPI)
	s.mux.HandleFunc("/api/research/", s.handleResearchDetailAPI)
	s.mux.HandleFunc("/api/research/stream/", s.handleResearchStreamAPI)
	s.mux.HandleFunc("/api/research/budget", s.handleResearchBudgetAPI)

	// Sprint 12 — Quick Chat API.
	s.mux.HandleFunc("/api/chat", s.handleQuickChatAPI)
	s.mux.HandleFunc("/api/chat/stream", s.handleChatStreamAPI)
	s.mux.HandleFunc("/api/thalamus", s.handleThalamusAPI)
	s.mux.HandleFunc("/api/events/", s.handleEventBusSSE)

	// Sprint 20 — Persistent Claude Chat.
	s.mux.HandleFunc("/api/claude/chat", s.handleClaudeChatAPI)
	s.mux.HandleFunc("/api/claude/history", s.handleClaudeHistoryAPI)
	s.mux.HandleFunc("/api/claude/stream", s.handleClaudeStreamAPI)

	// Health check for Cloud Run / load balancers.
	s.mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	// Index — serve React app for command center (falls through to static for /app),
	// or redirect to old dashboard for non-API paths.
	s.mux.HandleFunc("/", s.handleIndex)
}

// Start begins listening. Blocks until ctx is cancelled.
// Binds to 0.0.0.0 if ZBOT_BIND_ALL=true or ZBOT_ENV=production (for Docker/Coolify).
// Otherwise binds to 127.0.0.1 for local dev safety.
func (s *Server) Start(ctx context.Context) error {
	host := "127.0.0.1"
	if os.Getenv("ZBOT_BIND_ALL") == "true" || os.Getenv("ZBOT_ENV") == "production" {
		host = "0.0.0.0"
	}
	addr := fmt.Sprintf("%s:%d", host, s.port)
	srv := &http.Server{Addr: addr, Handler: s.mux}

	go func() {
		<-ctx.Done()
		srv.Close()
	}()

	s.logger.Info("web UI listening", "url", "http://"+addr)
	err := srv.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}
