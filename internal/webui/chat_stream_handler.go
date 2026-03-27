package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/ziloss-tech/zbot/internal/memory"
)

// chatStreamEvent represents one SSE event in the chat stream.
type chatStreamEvent struct {
	Type    string `json:"type"`    // "turn_start", "tool_called", "tool_result", "done", "error"
	Content string `json:"content"` // token text, tool name, error message, or final reply
	Detail  any    `json:"detail,omitempty"`
}

// handleChatStreamAPI handles POST /api/chat/stream — streaming agentic chat.
// Runs agent.Run() and streams event bus events + final reply as SSE.
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

	// Helper to write + flush an SSE event.
	writeSSE := func(evt chatStreamEvent) {
		data, _ := json.Marshal(evt)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	// Run agent in a goroutine so we can relay events while it runs.
	type agentResult struct {
		reply string
		err   error
	}
	resultCh := make(chan agentResult, 1)

	go func() {
		reply, err := s.quickChat(r.Context(), req.Message)
		resultCh <- agentResult{reply: reply, err: err}
	}()

	// Subscribe to event bus and relay events until agent finishes.
	var eventCh <-chan interface{}
	if s.eventBus != nil {
		ch := s.eventBus.Subscribe("web-chat")
		defer s.eventBus.Unsubscribe("web-chat", ch)

		// Relay loop: forward events until we get the agent result.
		for {
			select {
			case evt, ok := <-ch:
				if !ok {
					goto done
				}
				writeSSE(chatStreamEvent{
					Type:    string(evt.Type),
					Content: evt.Summary,
					Detail:  evt.Detail,
				})
				// If this was turn_complete, agent is about to finish.
				if evt.Type == "turn_complete" {
					goto done
				}
			case res := <-resultCh:
				// Agent finished (possibly without any events for very fast responses).
				if res.err != nil {
					writeSSE(chatStreamEvent{Type: "error", Content: res.err.Error()})
				} else {
					writeSSE(chatStreamEvent{Type: "done", Content: res.reply})
					// Auto-save substantial replies to long-term memory.
					if pgStore, ok := s.memStore.(*memory.Store); ok {
						pgStore.AutoSave(r.Context(), "web-chat", res.reply)
					}
				}
				return
			case <-r.Context().Done():
				return
			}
		}
	}

done:
	_ = eventCh // suppress unused

	// Wait for the agent result (should be immediate after turn_complete).
	select {
	case res := <-resultCh:
		if res.err != nil {
			writeSSE(chatStreamEvent{Type: "error", Content: res.err.Error()})
		} else {
			writeSSE(chatStreamEvent{Type: "done", Content: res.reply})
			// Auto-save substantial replies to long-term memory.
			if pgStore, ok := s.memStore.(*memory.Store); ok {
				pgStore.AutoSave(r.Context(), "web-chat", res.reply)
			}
		}
	case <-time.After(30 * time.Second):
		writeSSE(chatStreamEvent{Type: "error", Content: "agent timeout"})
	case <-r.Context().Done():
		return
	}
}
