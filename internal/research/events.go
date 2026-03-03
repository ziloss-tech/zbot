package research

import "time"

// ResearchEvent represents a progress update streamed to the UI via SSE.
type ResearchEvent struct {
	SessionID  string  `json:"session_id"`
	Stage      string  `json:"stage"` // planning, searching, extracting, critiquing, evaluated, synthesizing, complete, error
	Iteration  int     `json:"iteration"`
	Model      string  `json:"model"`        // display name of the model performing this stage
	ModelID    string  `json:"model_id"`     // raw model ID
	Message    string  `json:"message"`      // short natural language summary
	Confidence float64 `json:"confidence"`   // 0.0-1.0, only set for critiquing/evaluated
	Passed     bool    `json:"passed"`       // only set for evaluated stage
	Sources    int     `json:"sources"`      // cumulative source count
	Claims     int     `json:"claims"`       // cumulative claim count
	Report     string  `json:"report"`       // only set for complete stage
	CostUSD    float64 `json:"cost_usd"`     // cumulative cost
	Error      string  `json:"error"`        // only set for error stage
	Timestamp  string  `json:"timestamp"`
}

// EventEmitter sends research events to subscribers.
type EventEmitter struct {
	ch chan ResearchEvent
}

// NewEventEmitter creates an emitter with a buffered channel.
func NewEventEmitter(bufSize int) *EventEmitter {
	return &EventEmitter{
		ch: make(chan ResearchEvent, bufSize),
	}
}

// Emit sends an event, filling in the timestamp.
func (e *EventEmitter) Emit(evt ResearchEvent) {
	if evt.Timestamp == "" {
		evt.Timestamp = time.Now().Format(time.RFC3339)
	}
	select {
	case e.ch <- evt:
	default:
		// Drop event if buffer full — UI will catch up on next poll.
	}
}

// Events returns the read-only channel for subscribers (SSE handler).
func (e *EventEmitter) Events() <-chan ResearchEvent {
	return e.ch
}

// Close closes the event channel.
func (e *EventEmitter) Close() {
	close(e.ch)
}
