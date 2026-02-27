package webui

import (
	"sync"
)

// Event represents an SSE event broadcast to connected clients.
type Event struct {
	WorkflowID string `json:"workflow_id"`
	TaskID     string `json:"task_id,omitempty"`
	Source     string `json:"source"`     // "planner" | "executor"
	Type       string `json:"type"`       // "token" | "status" | "handoff" | "complete" | "error"
	Payload    string `json:"payload"`
}

// Hub is a pub/sub hub for broadcasting SSE events to connected browser clients.
// Subscribers are keyed by workflow ID. Thread-safe.
type Hub struct {
	mu          sync.RWMutex
	subscribers map[string][]chan Event
}

// NewHub creates a new SSE hub.
func NewHub() *Hub {
	return &Hub{
		subscribers: make(map[string][]chan Event),
	}
}

// Subscribe registers a channel to receive events for a workflow.
// Returns a read-only channel and an unsubscribe function.
func (h *Hub) Subscribe(workflowID string) (<-chan Event, func()) {
	ch := make(chan Event, 64)
	h.mu.Lock()
	h.subscribers[workflowID] = append(h.subscribers[workflowID], ch)
	h.mu.Unlock()

	unsub := func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		subs := h.subscribers[workflowID]
		for i, s := range subs {
			if s == ch {
				h.subscribers[workflowID] = append(subs[:i], subs[i+1:]...)
				close(ch)
				break
			}
		}
		if len(h.subscribers[workflowID]) == 0 {
			delete(h.subscribers, workflowID)
		}
	}

	return ch, unsub
}

// Publish sends an event to all subscribers for a workflow.
// Non-blocking — slow subscribers drop events.
func (h *Hub) Publish(e Event) {
	h.mu.RLock()
	subs := h.subscribers[e.WorkflowID]
	h.mu.RUnlock()

	for _, ch := range subs {
		select {
		case ch <- e:
		default:
			// drop event for slow subscriber
		}
	}
}
