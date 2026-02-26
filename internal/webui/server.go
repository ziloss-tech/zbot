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
)

//go:embed static/*
var staticFiles embed.FS

// Server serves the ZBOT web dashboard on loopback only.
type Server struct {
	port   int
	db     *pgxpool.Pool
	logger *slog.Logger
	mux    *http.ServeMux
}

// New creates a web UI server on port 18790.
func New(db *pgxpool.Pool, logger *slog.Logger) *Server {
	s := &Server{
		port:   18790,
		db:     db,
		logger: logger,
		mux:    http.NewServeMux(),
	}
	s.routes()
	return s
}

// routes registers all HTTP handlers.
func (s *Server) routes() {
	// Static assets.
	s.mux.Handle("/static/", http.FileServer(http.FS(staticFiles)))

	// Pages.
	s.mux.HandleFunc("/", s.handleIndex)
	s.mux.HandleFunc("/conversations", s.handleConversations)
	s.mux.HandleFunc("/conversations/", s.handleConversationDetail)
	s.mux.HandleFunc("/memory", s.handleMemory)
	s.mux.HandleFunc("/memory/", s.handleMemoryAction)
	s.mux.HandleFunc("/workflows", s.handleWorkflows)
	s.mux.HandleFunc("/workflows/", s.handleWorkflowAction)
	s.mux.HandleFunc("/audit", s.handleAudit)
	s.mux.HandleFunc("/api/stats", s.handleAPIStats)
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
