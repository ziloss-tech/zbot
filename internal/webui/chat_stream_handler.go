package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

// chatStreamEvent represents one SSE event in the chat stream.
type chatStreamEvent struct {
	Type    string `json:"type"`    // "token", "tool_start", "tool_done", "done", "error"
	Content string `json:"content"` // token text, tool name, error message
	Detail  any    `json:"detail,omitempty"`
}

// handleChatStreamAPI handles POST /api/chat/stream — streaming agentic chat.
func (s *Server) handleChatStreamAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.quickChat == nil {
		http.Error(w, "chat not available", http.StatusServiceUnavailable)
		return
	}

	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Message == "" {
		http.Error(w, "message is required", http.StatusBadRequest)
		return
	}

	// Set SSE headers.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// Thread-safe SSE writer — protects against concurrent writes
	// from the event relay goroutine and the main handler.
	var writeMu sync.Mutex
	closed := false

	writeSSE := func(evt chatStreamEvent) {
		writeMu.Lock()
		defer writeMu.Unlock()
		if closed {
			return
		}
		data, _ := json.Marshal(evt)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	// Subscribe to event bus for real-time tool events.
	doneCh := make(chan struct{})
	if s.eventBus != nil {
		ch := s.eventBus.Subscribe("web-chat")

		go func() {
			defer s.eventBus.Unsubscribe("web-chat", ch)
			for {
				select {
				case evt, ok := <-ch:
					if !ok {
						return
					}
					writeSSE(chatStreamEvent{
						Type:    string(evt.Type),
						Content: evt.Summary,
						Detail:  evt.Detail,
					})
				case <-doneCh:
					return
				case <-r.Context().Done():
					return
				}
			}
		}()
	}

	// Run the full agent loop.
	reply, err := s.quickChat(r.Context(), req.Message)

	// Signal the relay goroutine to stop BEFORE writing final events.
	close(doneCh)

	if err != nil {
		writeSSE(chatStreamEvent{Type: "error", Content: err.Error()})
		writeMu.Lock()
		closed = true
		writeMu.Unlock()
		return
	}

	// Send the final reply.
	writeSSE(chatStreamEvent{Type: "done", Content: reply})

	// Mark closed so no more writes happen.
	writeMu.Lock()
	closed = true
	writeMu.Unlock()
}
