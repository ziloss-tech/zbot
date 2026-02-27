// Package webui serves the ZBOT web dashboard.
// CRITICAL: Binds to 127.0.0.1 ONLY — never 0.0.0.0.
package webui

import (
	"context"
	"embed"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jeremylerwick-max/zbot/internal/agent"
	"github.com/jeremylerwick-max/zbot/internal/planner"
	"github.com/jeremylerwick-max/zbot/internal/workflow"
)

//go:embed static/*
var staticFiles embed.FS

// QuickChatFunc handles non-plan messages with memory context.
type QuickChatFunc func(ctx context.Context, message string) (string, error)

// Server serves the ZBOT web dashboard on loopback only.
type Server struct {
	port      int
	db        *pgxpool.Pool
	logger    *slog.Logger
	mux       *http.ServeMux
	hub       *Hub
	planner   *planner.Planner
	orch      *workflow.Orchestrator
	memStore  agent.MemoryStore // Sprint 12: memory panel + quick chat
	quickChat QuickChatFunc     // Sprint 12: memory-aware quick chat handler
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

// SetPlanner sets the planner for streaming plan creation.
func (s *Server) SetPlanner(p *planner.Planner) {
	s.planner = p
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

// Hub returns the SSE hub for external event publishing (e.g., from orchestrator).
func (s *Server) Hub() *Hub {
	return s.hub
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

	// Sprint 12 — Quick Chat API.
	s.mux.HandleFunc("/api/chat", s.handleQuickChatAPI)

	// Index — serve React app for command center (falls through to static for /app),
	// or redirect to old dashboard for non-API paths.
	s.mux.HandleFunc("/", s.handleIndex)
}

// Start begins listening. Blocks until ctx is cancelled.
func (s *Server) Start(ctx context.Context) error {
	addr := fmt.Sprintf("127.0.0.1:%d", s.port)
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
