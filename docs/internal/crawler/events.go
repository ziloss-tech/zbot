package crawler

import "time"

// Event type constants — these match the existing AgentEvent pattern in internal/agent/ports.go
type EventType string

const (
	EventCrawlScreenshot EventType = "crawl_screenshot"
	EventCrawlAction     EventType = "crawl_action"
	EventCrawlStatus     EventType = "crawl_status"
	EventCrawlError      EventType = "crawl_error"
	EventCrawlGridUpdate EventType = "crawl_grid_update"
)

// CrawlerStatus represents the current state of a crawler session
type CrawlerStatus string

const (
	StatusIdle       CrawlerStatus = "idle"
	StatusNavigating CrawlerStatus = "navigating"
	StatusActing     CrawlerStatus = "acting"
	StatusWaiting    CrawlerStatus = "waiting"
	StatusStopped    CrawlerStatus = "stopped"
)

// ViewportSize defines browser viewport dimensions
type ViewportSize struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// Rect represents a bounding box
type Rect struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// ElementInfo describes a DOM element found during crawling
type ElementInfo struct {
	Tag         string            `json:"tag"`
	Text        string            `json:"text"`
	Attrs       map[string]string `json:"attrs"`
	BoundingBox *Rect             `json:"bounding_box,omitempty"`
	GridCells   []string          `json:"grid_cells"`
}

// CrawlEvent is the payload for crawl-related events emitted through the event bus
type CrawlEvent struct {
	SessionID   string        `json:"session_id"`
	Type        EventType     `json:"type"`
	Action      string        `json:"action"`
	GridCell    string        `json:"grid_cell,omitempty"`
	URL         string        `json:"url,omitempty"`
	ElementInfo *ElementInfo  `json:"element_info,omitempty"`
	Screenshot  string        `json:"screenshot,omitempty"`
	Status      CrawlerStatus `json:"status"`
	Error       string        `json:"error,omitempty"`
	Timestamp   time.Time     `json:"timestamp"`
	PageTitle   string        `json:"page_title,omitempty"`
}

// ClickResult is returned from Click() with info about what was clicked
type ClickResult struct {
	Element    *ElementInfo `json:"element"`
	BeforeShot string       `json:"before_shot,omitempty"`
	AfterShot  string       `json:"after_shot,omitempty"`
	GridCell   string       `json:"grid_cell"`
	PixelX     int          `json:"pixel_x"`
	PixelY     int          `json:"pixel_y"`
	Success    bool         `json:"success"`
}

// EventBus is the interface matching ZBOT's existing MemEventBus
// The crawler emits events through this interface
type EventBus interface {
	Publish(sessionID string, event interface{})
	Subscribe(sessionID string) <-chan interface{}
	Unsubscribe(sessionID string, ch <-chan interface{})
}

// NewCrawlEvent creates a properly timestamped CrawlEvent
func NewCrawlEvent(sessionID string, eventType EventType, action string) CrawlEvent {
	return CrawlEvent{
		SessionID: sessionID,
		Type:      eventType,
		Action:    action,
		Status:    StatusIdle,
		Timestamp: time.Now(),
	}
}
