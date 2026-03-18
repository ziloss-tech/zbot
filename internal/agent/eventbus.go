package agent

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// MemEventBus is an in-memory EventBus backed by per-session ring buffers.
// Thread-safe for concurrent Emit/Subscribe from agent goroutines.
type MemEventBus struct {
	mu          sync.RWMutex
	subscribers map[string][]chan AgentEvent // sessionID -> subscriber channels
	history     map[string][]AgentEvent     // sessionID -> recent events (ring buffer)
	maxHistory  int
	eventSeq    uint64
}

// NewMemEventBus creates an in-memory event bus.
// maxHistory controls how many events are retained per session for late joiners.
func NewMemEventBus(maxHistory int) *MemEventBus {
	if maxHistory <= 0 {
		maxHistory = 100
	}
	return &MemEventBus{
		subscribers: make(map[string][]chan AgentEvent),
		history:     make(map[string][]AgentEvent),
		maxHistory:  maxHistory,
	}
}

// Emit publishes an event to all subscribers and stores it in history.
func (b *MemEventBus) Emit(_ context.Context, event AgentEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Auto-assign ID and timestamp if not set.
	if event.ID == "" {
		b.eventSeq++
		event.ID = fmt.Sprintf("evt-%d-%d", event.Timestamp.UnixMilli(), b.eventSeq)
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	// Store in history ring buffer.
	hist := b.history[event.SessionID]
	if len(hist) >= b.maxHistory {
		hist = hist[1:] // drop oldest
	}
	b.history[event.SessionID] = append(hist, event)

	// Fan out to subscribers (non-blocking).
	for _, ch := range b.subscribers[event.SessionID] {
		select {
		case ch <- event:
		default:
			// Subscriber is slow — drop event rather than blocking Cortex.
		}
	}
}

// Subscribe returns a channel that receives events for a given session.
func (b *MemEventBus) Subscribe(sessionID string) <-chan AgentEvent {
	b.mu.Lock()
	defer b.mu.Unlock()

	ch := make(chan AgentEvent, 64) // buffered to absorb bursts
	b.subscribers[sessionID] = append(b.subscribers[sessionID], ch)
	return ch
}

// Unsubscribe removes a subscription channel.
func (b *MemEventBus) Unsubscribe(sessionID string, ch <-chan AgentEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()

	subs := b.subscribers[sessionID]
	for i, sub := range subs {
		if sub == ch {
			b.subscribers[sessionID] = append(subs[:i], subs[i+1:]...)
			close(sub)
			break
		}
	}
}

// Recent returns the last N events for a session.
func (b *MemEventBus) Recent(sessionID string, n int) []AgentEvent {
	b.mu.RLock()
	defer b.mu.RUnlock()

	hist := b.history[sessionID]
	if n >= len(hist) {
		result := make([]AgentEvent, len(hist))
		copy(result, hist)
		return result
	}
	result := make([]AgentEvent, n)
	copy(result, hist[len(hist)-n:])
	return result
}
