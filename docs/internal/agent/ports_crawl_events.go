package agent

// ──────────────────────────────────────────────────────────
// Crawl Event Type Constants
// ──────────────────────────────────────────────────────────
// MERGE these constants into your existing ports.go file
// alongside the other EventType constants (EventTurnStart, etc.)
// ──────────────────────────────────────────────────────────

// Crawl event types — emitted by internal/crawler/ module.
// These flow through the existing MemEventBus → SSE → frontend pipeline.
const (
	EventCrawlScreenshot EventType = "crawl_screenshot"
	EventCrawlAction     EventType = "crawl_action"
	EventCrawlStatus     EventType = "crawl_status"
	EventCrawlError      EventType = "crawl_error"
	EventCrawlGridUpdate EventType = "crawl_grid_update"
)
