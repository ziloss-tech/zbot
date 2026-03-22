package crawler

import "time"

// CrawlerStatus represents the current state of the crawler.
type CrawlerStatus string

const (
	StatusIdle       CrawlerStatus = "idle"
	StatusNavigating CrawlerStatus = "navigating"
	StatusActing     CrawlerStatus = "acting"
	StatusWaiting    CrawlerStatus = "waiting"
	StatusStopped    CrawlerStatus = "stopped"
)

// CrawlEventType identifies the kind of crawl event emitted to the event bus.
type CrawlEventType string

const (
	EventCrawlScreenshot CrawlEventType = "crawl_screenshot"
	EventCrawlAction     CrawlEventType = "crawl_action"
	EventCrawlStatus     CrawlEventType = "crawl_status"
	EventCrawlError      CrawlEventType = "crawl_error"
	EventCrawlGrid       CrawlEventType = "crawl_grid_update"
)

// CrawlEvent is the payload for crawl-related events.
type CrawlEvent struct {
	SessionID   string         `json:"session_id"`
	Type        CrawlEventType `json:"type"`
	Action      string         `json:"action,omitempty"`
	GridCell    string         `json:"grid_cell,omitempty"`
	URL         string         `json:"url,omitempty"`
	ElementInfo *ElementInfo   `json:"element_info,omitempty"`
	Screenshot  string         `json:"screenshot,omitempty"` // base64 JPEG
	Status      CrawlerStatus  `json:"status,omitempty"`
	Error       string         `json:"error,omitempty"`
	DurationMs  int64          `json:"duration_ms,omitempty"`
	PageTitle   string         `json:"page_title,omitempty"`
	Timestamp   time.Time      `json:"timestamp"`
}

// ElementInfo describes the DOM element at a grid position.
type ElementInfo struct {
	Tag       string            `json:"tag"`
	Text      string            `json:"text"`
	Attrs     map[string]string `json:"attrs,omitempty"`
	GridCells []string          `json:"grid_cells,omitempty"`
}

// ActionEntry is a single logged crawler action with full metadata.
type ActionEntry struct {
	ID           string            `json:"id"`
	Timestamp    time.Time         `json:"timestamp"`
	Action       string            `json:"action"`
	GridCell     string            `json:"grid_cell,omitempty"`
	PixelX       int               `json:"pixel_x,omitempty"`
	PixelY       int               `json:"pixel_y,omitempty"`
	URL          string            `json:"url"`
	Input        string            `json:"input,omitempty"`
	ElementTag   string            `json:"element_tag,omitempty"`
	ElementText  string            `json:"element_text,omitempty"`
	ElementAttrs map[string]string `json:"element_attrs,omitempty"`
	Success      bool              `json:"success"`
	Error        string            `json:"error,omitempty"`
	DurationMs   int64             `json:"duration_ms"`
	PageTitle    string            `json:"page_title,omitempty"`
}

// SessionInfo is metadata about a crawler session for listing.
type SessionInfo struct {
	SessionID  string        `json:"session_id"`
	Status     CrawlerStatus `json:"status"`
	CurrentURL string        `json:"current_url"`
	Grid       *Grid         `json:"grid"`
	ActionCount int          `json:"action_count"`
	CreatedAt  time.Time     `json:"created_at"`
}
