package crawler

import (
	"time"

	"github.com/ziloss-tech/zbot/internal/agent"
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

// NewCrawlEvent creates a properly timestamped AgentEvent with crawl data
func NewCrawlEvent(sessionID string, eventType agent.EventType, summary string, detail map[string]any) agent.AgentEvent {
	return agent.AgentEvent{
		ID:        "",
		SessionID: sessionID,
		Type:      eventType,
		Summary:   summary,
		Detail:    detail,
		Timestamp: time.Now(),
	}
}
