package crawler

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// ActionEntry represents a single logged crawler action
type ActionEntry struct {
	ID            string            `json:"id"`
	Timestamp     time.Time         `json:"timestamp"`
	Action        string            `json:"action"`
	GridCell      string            `json:"grid_cell,omitempty"`
	PixelX        int               `json:"pixel_x,omitempty"`
	PixelY        int               `json:"pixel_y,omitempty"`
	URL           string            `json:"url"`
	Input         string            `json:"input,omitempty"`
	ElementTag    string            `json:"element_tag,omitempty"`
	ElementText   string            `json:"element_text,omitempty"`
	ElementAttrs  map[string]string `json:"element_attrs,omitempty"`
	ScreenshotB64 string            `json:"screenshot_b64,omitempty"`
	Success       bool              `json:"success"`
	Error         string            `json:"error,omitempty"`
	DurationMs    int64             `json:"duration_ms"`
	PageTitle     string            `json:"page_title,omitempty"`
}

// ActionLogger is a thread-safe logger for crawler actions
type ActionLogger struct {
	sessionID     string
	entries       []ActionEntry
	eventBus      EventBus
	mu            sync.RWMutex
	entryCounter  int
	sessionStart  time.Time
}

// NewActionLogger creates a new action logger for a session
func NewActionLogger(sessionID string, eventBus EventBus) *ActionLogger {
	return &ActionLogger{
		sessionID:    sessionID,
		entries:      make([]ActionEntry, 0),
		eventBus:     eventBus,
		entryCounter: 0,
		sessionStart: time.Now(),
	}
}

// Log records an action, generates ID if needed, emits event, and clears screenshot from memory
func (l *ActionLogger) Log(entry ActionEntry) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Generate ID if empty
	if entry.ID == "" {
		l.entryCounter++
		entry.ID = fmt.Sprintf("%s-%d", l.sessionID, l.entryCounter)
	}

	// Set timestamp if zero
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}

	// Store screenshot reference temporarily for event emission
	screenshotForEvent := entry.ScreenshotB64

	// Clear screenshot from in-memory entry to avoid bloating memory
	entry.ScreenshotB64 = ""

	// Append to entries
	l.entries = append(l.entries, entry)

	// Emit event through event bus
	if l.eventBus != nil {
		event := CrawlEvent{
			SessionID:  l.sessionID,
			Type:       EventCrawlAction,
			Action:     entry.Action,
			GridCell:   entry.GridCell,
			URL:        entry.URL,
			Screenshot: screenshotForEvent,
			Status:     StatusActing,
			Timestamp:  entry.Timestamp,
			PageTitle:  entry.PageTitle,
		}
		l.eventBus.Publish(l.sessionID, event)
	}
}

// Export returns the full log as JSON
func (l *ActionLogger) Export() ([]byte, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	return json.Marshal(l.entries)
}

// Tail returns the last N entries
func (l *ActionLogger) Tail(n int) []ActionEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if n <= 0 {
		return []ActionEntry{}
	}

	count := len(l.entries)
	if n > count {
		n = count
	}

	result := make([]ActionEntry, n)
	copy(result, l.entries[count-n:])
	return result
}

// Count returns the number of logged entries
func (l *ActionLogger) Count() int {
	l.mu.RLock()
	defer l.mu.RUnlock()

	return len(l.entries)
}

// Clear removes all entries from the log
func (l *ActionLogger) Clear() {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.entries = make([]ActionEntry, 0)
	l.entryCounter = 0
}
