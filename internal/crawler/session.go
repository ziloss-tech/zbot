package crawler

import (
	"fmt"
	"sync"
)

// SessionManager manages concurrent crawler sessions.
type SessionManager struct {
	sessions map[string]*Crawler
	mu       sync.RWMutex
}

// NewSessionManager creates an empty session manager.
func NewSessionManager() *SessionManager {
	return &SessionManager{sessions: make(map[string]*Crawler)}
}

// Start creates a new crawler session and returns it.
func (m *SessionManager) Start(viewportW, viewportH, cellSize int, emitter EventEmitter) (*Crawler, error) {
	c, err := NewCrawler(viewportW, viewportH, cellSize, emitter)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.sessions[c.SessionID()] = c
	m.mu.Unlock()
	return c, nil
}

// Get returns a session by ID.
func (m *SessionManager) Get(sessionID string) (*Crawler, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c, ok := m.sessions[sessionID]
	if !ok {
		return nil, fmt.Errorf("crawler session %q not found", sessionID)
	}
	return c, nil
}

// Stop closes and removes a session.
func (m *SessionManager) Stop(sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.sessions[sessionID]
	if !ok {
		return fmt.Errorf("crawler session %q not found", sessionID)
	}
	c.Close()
	delete(m.sessions, sessionID)
	return nil
}

// List returns info about all active sessions.
func (m *SessionManager) List() []SessionInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]SessionInfo, 0, len(m.sessions))
	for _, c := range m.sessions {
		out = append(out, SessionInfo{
			SessionID:   c.SessionID(),
			Status:      c.Status(),
			CurrentURL:  c.CurrentURL(),
			Grid:        c.Grid(),
			ActionCount: c.Logger().Len(),
			CreatedAt:   c.CreatedAt(),
		})
	}
	return out
}
