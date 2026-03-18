package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// handleEventBusSSE streams agent events via SSE for real-time UI updates.
// GET /api/events/:sessionID
// The frontend subscribes to this when a chat session is active.
// Events are lightweight JSON — Thalamus, tool chips, and the
// adaptive UI all consume these to know what Cortex is doing.
func (s *Server) handleEventBusSSE(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.eventBus == nil {
		http.Error(w, "event bus not available", http.StatusServiceUnavailable)
		return
	}

	sessionID := strings.TrimPrefix(r.URL.Path, "/api/events/")
	if sessionID == "" {
		sessionID = "web-chat" // default session for the web UI
	}

	// Set SSE headers.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("X-Accel-Buffering", "no") // disable nginx buffering

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// Replay recent events for late joiners (e.g. page refresh).
	recent := s.eventBus.Recent(sessionID, 20)
	for _, evt := range recent {
		data, _ := json.Marshal(evt)
		fmt.Fprintf(w, "data: %s\n\n", data)
	}
	flusher.Flush()

	// Subscribe to live events.
	ch := s.eventBus.Subscribe(sessionID)
	defer s.eventBus.Unsubscribe(sessionID, ch)

	// Keep-alive ticker to prevent proxy timeouts.
	// Send a comment every 15 seconds if no events.
	ctx := r.Context()

	for {
		select {
		case <-ctx.Done():
			// Client disconnected.
			return
		case evt, ok := <-ch:
			if !ok {
				// Channel closed.
				return
			}
			data, err := json.Marshal(evt)
			if err != nil {
				s.logger.Warn("event bus SSE: marshal error", "err", err)
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}
