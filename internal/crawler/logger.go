package crawler

import (
	"encoding/json"
	"sync"
)

// ActionLogger stores every crawler action, thread-safe.
type ActionLogger struct {
	entries []ActionEntry
	mu      sync.RWMutex
}

// NewActionLogger creates an empty logger.
func NewActionLogger() *ActionLogger {
	return &ActionLogger{}
}

// Log appends an entry.
func (l *ActionLogger) Log(entry ActionEntry) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, entry)
}

// Tail returns the last n entries.
func (l *ActionLogger) Tail(n int) []ActionEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if n <= 0 || len(l.entries) == 0 {
		return nil
	}
	start := len(l.entries) - n
	if start < 0 {
		start = 0
	}
	out := make([]ActionEntry, len(l.entries)-start)
	copy(out, l.entries[start:])
	return out
}

// Len returns the number of logged entries.
func (l *ActionLogger) Len() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.entries)
}

// Export returns the full log as JSON bytes.
func (l *ActionLogger) Export() ([]byte, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return json.Marshal(l.entries)
}
