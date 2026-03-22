package agent

// ──────────────────────────────────────────────────────────
// Crawl Event Type Constants
// ──────────────────────────────────────────────────────────
// ADD these constants to your existing ports.go file alongside
// the other EventType constants (turn_start, plan_complete, etc.)
//
// These event types are emitted by the crawler module and flow
// through the existing MemEventBus → SSE → frontend pipeline.
// ──────────────────────────────────────────────────────────

// Crawl event types — emitted by internal/crawler/ module
const (
	// EventCrawlScreenshot is emitted at ~2fps with base64 JPEG screenshot data.
	// Payload: CrawlEvent with Screenshot field populated.
	// Frontend consumer: BrowserPane (updates the live screenshot viewer).
	EventCrawlScreenshot = "crawl_screenshot"

	// EventCrawlAction is emitted after every crawler action completes.
	// Payload: CrawlEvent with Action, GridCell, ElementInfo, URL, etc.
	// Frontend consumer: CrawlLogPane (appends action to scrolling log).
	EventCrawlAction = "crawl_action"

	// EventCrawlStatus is emitted when the crawler status changes.
	// Payload: CrawlEvent with Status field (idle/navigating/acting/waiting/stopped).
	// Frontend consumer: BrowserPane status indicator, ChatPane activity strip.
	EventCrawlStatus = "crawl_status"

	// EventCrawlError is emitted when a crawler action fails.
	// Payload: CrawlEvent with Error field populated.
	// Frontend consumer: CrawlLogPane (shows error entry), BrowserPane (status → error).
	EventCrawlError = "crawl_error"

	// EventCrawlGridUpdate is emitted when the grid configuration changes.
	// Payload: CrawlEvent with grid overlay JSON.
	// Frontend consumer: BrowserPane (updates grid overlay dimensions).
	EventCrawlGridUpdate = "crawl_grid_update"
)

// NOTE: The SSE endpoint (internal/webui/events_handler.go) already streams
// ALL event types from MemEventBus. No changes needed there — these new
// event types will automatically flow through the existing SSE pipeline.
//
// The frontend useEventBus hook also already receives all event types.
// The useCrawler hook filters for crawl_* events specifically.
