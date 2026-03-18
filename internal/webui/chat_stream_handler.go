package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// chatStreamEvent represents one SSE event in the chat stream.
type chatStreamEvent struct {
	Type    string `json:"type"`    // "token", "tool_start", "tool_done", "done", "error"
	Content string `json:"content"` // token text, tool name, error message
	Detail  any    `json:"detail,omitempty"`
}

// handleChatStreamAPI handles POST /api/chat/stream — streaming agentic chat.
// Instead of waiting for the full response, this endpoint:
// 1. Runs agent.Run() in a goroutine
// 2. Streams event bus events as they happen (tool calls, etc.)
// 3. Streams the final reply as a "done" event
// The frontend subscribes to this via EventSource-like fetch + ReadableStream.
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

	// Set SSE headers for streaming response.
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

	// Subscribe to event bus for this session to relay tool events.
	var eventCh <-chan any
	if s.eventBus != nil {
		ch := s.eventBus.Subscribe("web-chat")
		defer s.eventBus.Unsubscribe("web-chat", ch)

		// Relay events in background until chat completes.
		doneCh := make(chan struct{})
		go func() {
			for {
				select {
				case evt, ok := <-ch:
					if !ok {
						return
					}
					data, _ := json.Marshal(chatStreamEvent{
						Type:    string(evt.Type),
						Content: evt.Summary,
						Detail:  evt.Detail,
					})
					fmt.Fprintf(w, "data: %s\n\n", data)
					flusher.Flush()
				case <-doneCh:
					return
				case <-r.Context().Done():
					return
				}
			}
		}()
		defer close(doneCh)
		_ = eventCh // suppress unused
	}

	// Run the full agent loop (synchronous — tool calls happen here).
	reply, err := s.quickChat(r.Context(), req.Message)

	if err != nil {
		data, _ := json.Marshal(chatStreamEvent{
			Type:    "error",
			Content: err.Error(),
		})
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
		return
	}

	// Send the final reply.
	data, _ := json.Marshal(chatStreamEvent{
		Type:    "done",
		Content: reply,
	})
	fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()
}
