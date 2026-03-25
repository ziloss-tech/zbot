package crawler

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ziloss-tech/zbot/internal/agent"
)

var sessionCounter uint64

// SessionInfo provides a summary of a crawl session
type SessionInfo struct {
	SessionID   string        `json:"session_id"`
	Status      CrawlerStatus `json:"status"`
	CurrentURL  string        `json:"current_url"`
	PageTitle   string        `json:"page_title"`
	CreatedAt   time.Time     `json:"created_at"`
	ActionCount int           `json:"action_count"`
	Viewport    ViewportSize  `json:"viewport"`
}

// SessionManager manages multiple concurrent crawl sessions
type SessionManager struct {
	sessions map[string]*sessionEntry
	eventBus agent.EventBus
	mu       sync.RWMutex
}

type sessionEntry struct {
	crawler   *Crawler
	createdAt time.Time
	viewport  ViewportSize
}

// NewSessionManager creates a new SessionManager
func NewSessionManager(eventBus agent.EventBus) *SessionManager {
	return &SessionManager{
		sessions: make(map[string]*sessionEntry),
		eventBus: eventBus,
	}
}

// StartSession generates a new session ID, creates a Crawler, and starts the screenshot streamer
func (m *SessionManager) StartSession(viewport ViewportSize) (string, error) {
	// Generate session ID
	counter := atomic.AddUint64(&sessionCounter, 1)
	timestamp := time.Now().Unix()
	sessionID := fmt.Sprintf("crawl-%d-%d", timestamp, counter)

	// Create new crawler
	crawler, err := NewCrawler(m.eventBus, sessionID, viewport)
	if err != nil {
		return "", fmt.Errorf("failed to create crawler: %w", err)
	}

	// Store session entry
	m.mu.Lock()
	m.sessions[sessionID] = &sessionEntry{
		crawler:   crawler,
		createdAt: time.Now(),
		viewport:  viewport,
	}
	m.mu.Unlock()

	return sessionID, nil
}

// GetSession returns the Crawler for a given session ID
func (m *SessionManager) GetSession(sessionID string) (*Crawler, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entry, exists := m.sessions[sessionID]
	if !exists {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}

	return entry.crawler, nil
}

// StopSession closes the crawler and removes it from the session map
func (m *SessionManager) StopSession(sessionID string) error {
	m.mu.Lock()
	entry, exists := m.sessions[sessionID]
	if !exists {
		m.mu.Unlock()
		return fmt.Errorf("session not found: %s", sessionID)
	}
	delete(m.sessions, sessionID)
	m.mu.Unlock()

	// Close the crawler
	entry.crawler.Close()

	return nil
}

// ListSessions returns SessionInfo for all active sessions
func (m *SessionManager) ListSessions() []SessionInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sessions := make([]SessionInfo, 0, len(m.sessions))
	for sessionID, entry := range m.sessions {
		info := SessionInfo{
			SessionID:   sessionID,
			Status:      entry.crawler.Status(),
			CurrentURL:  entry.crawler.CurrentURL(),
			PageTitle:   entry.crawler.PageTitle(),
			CreatedAt:   entry.createdAt,
			ActionCount: entry.crawler.Logger().Count(),
			Viewport:    entry.viewport,
		}
		sessions = append(sessions, info)
	}

	return sessions
}

// SessionCount returns the number of active sessions
func (m *SessionManager) SessionCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return len(m.sessions)
}

// SetEventBus wires the event bus after construction.
// This is needed because the SessionManager is created before the event bus in wire.go.
func (m *SessionManager) SetEventBus(eb agent.EventBus) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.eventBus = eb
}
