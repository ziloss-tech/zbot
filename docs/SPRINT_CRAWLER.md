# ZBOT Sprint — Visual Crawler with Grid Navigation + Action Logging
## Codename: Hawkeye

**Date:** 2026-03-22
**Assignee:** Coworker (Jean Claude)
**Repo:** ~/Desktop/Projects/zbot
**Branch:** feat/visual-crawler
**Base:** public-release

---

## Vision

ZBOT gets a contained, visible web crawler that runs INSIDE the ZBOT window as a pane.
The user can see exactly what the crawler is doing at all times. A grid overlay ensures
precise click targeting — Cortex specifies grid coordinates, not fragile CSS selectors.
Every single action is logged with timestamps, screenshots, and element metadata.

This is NOT a headless scraper. This is a visual browser automation pane that the user
can watch, verify, and control — like having a second pair of hands browsing the web.

---

## Architecture Overview

### New Module: `internal/crawler/`

The crawler wraps go-rod (already a dependency) in HEADED mode with CDP screenshot
streaming. It does NOT open a separate Chrome window — it runs headless but streams
screenshots to the ZBOT UI at ~2fps, giving the appearance of a live embedded browser.

```
┌─────────────────────────────────────────────────┐
│  ZBOT Window (PaneManager)                      │
│  ┌──────────┬───────────────────┬─────────────┐ │
│  │ Chat     │  BrowserPane      │ CrawlLog    │ │
│  │ (40%)    │  (40%)            │ (20%)       │ │
│  │          │  ┌─────────────┐  │ [14:23:01]  │ │
│  │          │  │ Screenshot  │  │  navigate → │ │
│  │          │  │ + Grid      │  │  google.com │ │
│  │          │  │ Overlay     │  │ [14:23:03]  │ │
│  │          │  │             │  │  click B4   │ │
│  │          │  │ [A1][A2]... │  │  "Search"   │ │
│  │          │  │ [B1][B2]... │  │ [14:23:04]  │ │
│  │          │  └─────────────┘  │  type "..."  │ │
│  │          │  URL: google.com  │             │ │
│  └──────────┴───────────────────┴─────────────┘ │
└─────────────────────────────────────────────────┘
```

### Data Flow

```
Cortex says "click grid B4"
  → API: POST /api/crawler/action {type: "click", grid: "B4"}
    → crawler.go maps B4 → pixel (x=180, y=120)
    → go-rod clicks at (180, 120)
    → screenshot captured BEFORE + AFTER
    → CrawlEvent emitted to EventBus {action, grid, pixel, element_tag, element_text, screenshot_url, timestamp}
    → SSE streams event to frontend
      → BrowserPane updates screenshot
      → CrawlLogPane appends action entry
```

---

## File Structure

```
internal/crawler/
├── crawler.go        # Main Crawler struct — manages go-rod browser lifecycle
├── grid.go           # Grid coordinate system (viewport → NxN grid → pixel mapping)
├── actions.go        # Action executors: click, type, scroll, navigate, hover, wait
├── logger.go         # Persistent action log — every action with full metadata
├── session.go        # Session management: start, pause, resume, stop, list
├── screenshot.go     # Screenshot capture + optional grid overlay rendering
└── events.go         # CrawlEvent types + EventBus integration

internal/webui/
├── crawler_handler.go  # HTTP API for crawler control

internal/webui/frontend/src/
├── components/
│   ├── BrowserPane.tsx    # Live screenshot viewer + grid overlay + URL bar
│   └── CrawlLogPane.tsx   # Scrolling action log with timestamps
├── hooks/
│   └── useCrawler.ts      # React hook for crawler state + SSE events
```

---

## TASK 1: Grid Coordinate System (`internal/crawler/grid.go`)

The grid is the killer feature. It divides the browser viewport into a labeled grid
so that Cortex (or any tool) can say "click C7" instead of trying to find elements
by CSS selector (which breaks constantly on dynamic sites).

### Grid Design

```go
// Grid divides the viewport into rows (labeled A-Z, AA-AZ for >26) and columns (1-N).
// Default: 20 columns × 15 rows = 300 cells covering a 1280×960 viewport.
// Each cell is 64px × 64px at default resolution.
//
// Example: "C7" = Row C (3rd row), Column 7
//   → pixel center: x = (7-1)*64 + 32 = 416, y = (3-1)*64 + 32 = 160
//
// The grid adapts to viewport size. If viewport is 1920×1080:
//   cols = 1920/64 = 30, rows = 1080/64 ≈ 17

type Grid struct {
    CellWidth  int // pixels per cell width (default 64)
    CellHeight int // pixels per cell height (default 64)
    Cols       int // computed from viewport width
    Rows       int // computed from viewport height
    ViewportW  int
    ViewportH  int
}

type GridCell struct {
    Label  string // e.g. "C7"
    Row    int
    Col    int
    CenterX int   // pixel center X
    CenterY int   // pixel center Y
    TopLeft  Point
    BotRight Point
}

// CellFromLabel("C7") → GridCell with pixel coordinates
// CellsFromElement(rod.Element) → []GridCell that the element overlaps
// OverlayJSON() → JSON grid data for frontend rendering
```

### Why Grid > CSS Selectors
- CSS selectors break on SPAs, shadow DOM, dynamic IDs, iframes
- Grid coordinates are ABSOLUTE — they work on any page, any framework
- Cortex can look at the screenshot + grid and say "the button is at D5"
- Before/after screenshots prove exactly what was clicked
- Grid labels are compact: "click C7" vs `document.querySelector('.btn-primary.mt-4.hidden-xs')`

---

## TASK 2: Crawler Core (`internal/crawler/crawler.go`)

```go
type Crawler struct {
    browser    *rod.Browser
    page       *rod.Page
    grid       *Grid
    logger     *ActionLogger
    sessionID  string
    eventBus   EventBus      // ZBOT's existing MemEventBus
    mu         sync.RWMutex
    status     CrawlerStatus // idle | navigating | acting | waiting
    viewport   ViewportSize  // {width: 1280, height: 960}
}

type CrawlerStatus string
const (
    StatusIdle       CrawlerStatus = "idle"
    StatusNavigating CrawlerStatus = "navigating"
    StatusActing     CrawlerStatus = "acting"
    StatusWaiting    CrawlerStatus = "waiting"
)

// NewCrawler creates a crawler with a HEADLESS browser but with screenshot streaming.
// NOT headed — we don't want a Chrome window popping up on the user's machine.
// Instead, we capture screenshots at ~2fps and stream them to the UI via SSE.
func NewCrawler(eventBus EventBus, sessionID string) (*Crawler, error)

// Navigate goes to a URL, waits for load, captures screenshot, emits event.
func (c *Crawler) Navigate(url string) error

// Click clicks the center of a grid cell, captures before/after screenshots, emits event.
// Returns info about what element was at that grid cell (tag, text, href, etc.)
func (c *Crawler) Click(gridLabel string) (*ClickResult, error)

// Type types text into the currently focused element.
func (c *Crawler) Type(text string) error

// Scroll scrolls the page. direction: "up" | "down" | "left" | "right"
func (c *Crawler) Scroll(direction string, amount int) error

// Screenshot captures current page state and returns base64 PNG.
func (c *Crawler) Screenshot() (string, error)

// ScreenshotWithGrid captures screenshot with grid overlay baked in.
func (c *Crawler) ScreenshotWithGrid() (string, error)

// ElementAtGrid returns metadata about the DOM element at a grid cell center.
func (c *Crawler) ElementAtGrid(gridLabel string) (*ElementInfo, error)

// Close tears down the browser.
func (c *Crawler) Close()
```

### Key Design Decisions
1. **Headless + Screenshot Stream** — NOT headed. No Chrome window cluttering the desktop.
   go-rod runs headless. We capture screenshots via CDP `Page.captureScreenshot` at ~2fps
   and push them to the frontend via SSE as base64 PNGs.

2. **Single Tab** — One page per crawler session. User can see exactly which page ZBOT is on.
   No background tabs, no hidden windows.

3. **Rate Limited** — Built-in 500ms minimum between actions to avoid looking like a bot
   and to give the UI time to update. Configurable per-session.

4. **Event Bus Integration** — Every action emits a structured CrawlEvent through ZBOT's
   existing MemEventBus. The frontend picks these up via the existing SSE endpoint.

---

## TASK 3: Action Logger (`internal/crawler/logger.go`)

Every single thing the crawler does is logged. This is non-negotiable.

```go
type ActionLog struct {
    SessionID    string
    Entries      []ActionEntry
    mu           sync.RWMutex
}

type ActionEntry struct {
    ID             string        `json:"id"`
    Timestamp      time.Time     `json:"timestamp"`
    Action         string        `json:"action"`        // "navigate", "click", "type", "scroll", "wait"
    GridCell       string        `json:"grid_cell"`     // "C7" (empty for non-click actions)
    PixelX         int           `json:"pixel_x"`
    PixelY         int           `json:"pixel_y"`
    URL            string        `json:"url"`           // current page URL
    Input          string        `json:"input"`         // typed text, URL navigated to, etc.
    ElementTag     string        `json:"element_tag"`   // "button", "a", "input", etc.
    ElementText    string        `json:"element_text"`  // visible text of clicked element
    ElementAttrs   map[string]string `json:"element_attrs"` // href, class, id, name, etc.
    ScreenshotB64  string        `json:"screenshot_b64,omitempty"` // before-action screenshot
    Success        bool          `json:"success"`
    Error          string        `json:"error,omitempty"`
    DurationMs     int64         `json:"duration_ms"`   // how long the action took
    PageTitle      string        `json:"page_title"`
}

// Log appends an entry and emits a CrawlEvent to the event bus.
func (l *ActionLog) Log(entry ActionEntry)

// Export returns the full log as JSON (for download/review).
func (l *ActionLog) Export() []byte

// Tail returns the last N entries.
func (l *ActionLog) Tail(n int) []ActionEntry
```

### What Gets Logged
- **navigate**: URL, page title after load, load time
- **click**: grid cell, pixel coords, element tag/text/attrs, before screenshot
- **type**: text typed, which element received it
- **scroll**: direction, amount, new scroll position
- **wait**: what we waited for, how long
- **screenshot**: every screenshot capture (periodic + on-action)
- **error**: any action that failed, with full error context

---

## TASK 4: Screenshot Streaming (`internal/crawler/screenshot.go`)

```go
// ScreenshotStreamer runs in a goroutine, capturing screenshots at targetFPS
// and emitting them as events for the frontend.
type ScreenshotStreamer struct {
    crawler   *Crawler
    targetFPS int  // default 2 (one screenshot every 500ms)
    running   bool
    stopCh    chan struct{}
}

// Start begins periodic screenshot capture.
func (s *ScreenshotStreamer) Start()

// Stop halts the screenshot stream.
func (s *ScreenshotStreamer) Stop()

// RenderGridOverlay takes a screenshot and composites the grid lines + labels on it.
// Uses Go's image/draw package — no external deps.
// Grid lines: semi-transparent cyan (#00d4ff at 30% opacity)
// Labels: white text on dark background at each cell corner
func RenderGridOverlay(screenshot []byte, grid *Grid) ([]byte, error)
```

---

## TASK 5: HTTP API (`internal/webui/crawler_handler.go`)

```go
// Mount these routes in server.go alongside existing handlers.

POST /api/crawler/start
  Body: { "viewport_width": 1280, "viewport_height": 960, "grid_cell_size": 64 }
  Response: { "session_id": "...", "grid": { "rows": 15, "cols": 20 } }

POST /api/crawler/navigate
  Body: { "session_id": "...", "url": "https://example.com" }
  Response: { "success": true, "page_title": "...", "screenshot": "base64..." }

POST /api/crawler/click
  Body: { "session_id": "...", "grid_cell": "C7" }
  Response: { "success": true, "element": { "tag": "button", "text": "Submit" }, "screenshot": "base64..." }

POST /api/crawler/type
  Body: { "session_id": "...", "text": "hello world" }
  Response: { "success": true }

POST /api/crawler/scroll
  Body: { "session_id": "...", "direction": "down", "amount": 3 }
  Response: { "success": true, "screenshot": "base64..." }

GET /api/crawler/screenshot?session_id=...&grid=true
  Response: base64 PNG (with or without grid overlay)

GET /api/crawler/log?session_id=...&tail=50
  Response: [ ActionEntry, ActionEntry, ... ]

POST /api/crawler/stop
  Body: { "session_id": "..." }

// SSE: Crawl events flow through the EXISTING /api/events/:sessionID endpoint.
// New event types:
//   "crawl_screenshot" — periodic screenshot update (base64 PNG, ~2fps)
//   "crawl_action"     — action completed (ActionEntry JSON)
//   "crawl_status"     — status change (idle/navigating/acting/waiting)
//   "crawl_error"      — action failed
```

---

## TASK 6: CrawlEvent Types (`internal/crawler/events.go`)


Integrate with ZBOT's existing `AgentEvent` types in `internal/agent/ports.go`.

```go
// Add to existing event type constants in ports.go:
const (
    EventCrawlScreenshot EventType = "crawl_screenshot"
    EventCrawlAction     EventType = "crawl_action"
    EventCrawlStatus     EventType = "crawl_status"
    EventCrawlError      EventType = "crawl_error"
    EventCrawlGridUpdate EventType = "crawl_grid_update"
)

// CrawlEvent is the payload for crawl-related events.
type CrawlEvent struct {
    SessionID   string            `json:"session_id"`
    Action      string            `json:"action"`        // navigate, click, type, scroll, screenshot
    GridCell    string            `json:"grid_cell"`
    URL         string            `json:"url"`
    ElementInfo *ElementInfo      `json:"element_info,omitempty"`
    Screenshot  string            `json:"screenshot,omitempty"` // base64 PNG
    Status      CrawlerStatus     `json:"status"`
    Error       string            `json:"error,omitempty"`
    Timestamp   time.Time         `json:"timestamp"`
}

type ElementInfo struct {
    Tag      string            `json:"tag"`
    Text     string            `json:"text"`
    Attrs    map[string]string `json:"attrs"`
    BoundingBox *Rect          `json:"bounding_box"`
    GridCells   []string       `json:"grid_cells"` // which grid cells this element covers
}
```

---

## TASK 7: Frontend — BrowserPane (`components/BrowserPane.tsx`)

The BrowserPane is a new pane type in PaneManager. It shows:
1. **URL bar** at the top (shows current page URL, editable for manual navigation)
2. **Screenshot viewer** in the center (live-updating from SSE screenshots)
3. **Grid overlay** on top of the screenshot (CSS grid, toggleable)
4. **Mini toolbar**: back, forward, refresh, stop, toggle grid, screenshot button

```tsx
// Add to PaneManager's PANE_TEMPLATES:
browser: { type: 'browser', label: 'Browser', icon: '🌐' }

// BrowserPane.tsx structure:
export function BrowserPane({ sessionID, events }: BrowserPaneProps) {
  // State
  const [screenshot, setScreenshot] = useState<string>('')  // base64 PNG
  const [gridVisible, setGridVisible] = useState(true)
  const [gridConfig, setGridConfig] = useState({ rows: 15, cols: 20, cellW: 64, cellH: 64 })
  const [currentURL, setCurrentURL] = useState('')
  const [status, setStatus] = useState<CrawlerStatus>('idle')

  // Listen for crawl events via existing useEventBus hook
  // Update screenshot on crawl_screenshot events
  // Update status on crawl_status events

  return (
    <div className="browser-pane">
      {/* URL Bar */}
      <div className="url-bar">
        <button onClick={goBack}>←</button>
        <button onClick={goForward}>→</button>
        <button onClick={refresh}>↻</button>
        <input value={currentURL} onChange={...} onKeyDown={handleEnter} />
        <StatusIndicator status={status} />
      </div>

      {/* Screenshot + Grid Overlay */}
      <div className="viewport" style={{ position: 'relative' }}>
        <img src={`data:image/png;base64,${screenshot}`} />
        {gridVisible && <GridOverlay config={gridConfig} onClick={handleGridClick} />}
      </div>

      {/* Mini Toolbar */}
      <div className="toolbar">
        <button onClick={toggleGrid}>Grid: {gridVisible ? 'ON' : 'OFF'}</button>
        <span className="status-text">{status}</span>
      </div>
    </div>
  )
}

// GridOverlay: CSS grid of transparent cells with subtle borders.
// Hovering a cell highlights it cyan. Clicking emits the cell label.
// Each cell shows its label (A1, B3, etc.) on hover.
function GridOverlay({ config, onClick }) {
  // Renders a CSS grid with `config.rows * config.cols` cells
  // Each cell has onClick → onClick("C7")
  // Cells have 1px semi-transparent cyan borders
  // Hover: cell fills with rgba(0, 212, 255, 0.15)
}
```

### Styling
- Dark theme matching ZBOT (#0a0a1a bg, #00d4ff cyan accents)
- Grid lines: 1px rgba(0, 212, 255, 0.3)
- Grid hover: rgba(0, 212, 255, 0.15) fill
- Grid labels: 8px monospace, white on rgba(0,0,0,0.7) on hover
- URL bar: dark input, monospace, #00d4ff focus ring
- Status indicator: pulsing cyan dot when navigating, green when idle, amber when acting

---

## TASK 8: Frontend — CrawlLogPane (`components/CrawlLogPane.tsx`)

A scrolling log of every crawler action. Auto-scrolls to bottom. Each entry shows:

```
14:23:01  NAVIGATE  → google.com                    200ms
14:23:03  CLICK C7  → <button> "Google Search"       45ms
14:23:04  TYPE      → "zbot github"                   2ms
14:23:04  CLICK D4  → <button> "Google Search"       38ms
14:23:06  NAVIGATE  → google.com/search?q=...       890ms
14:23:07  CLICK B8  → <a> "ziloss-tech/zbot"         52ms
```

```tsx
function CrawlLogPane({ entries }: { entries: ActionEntry[] }) {
  // Auto-scrolling log with:
  // - Timestamp (HH:MM:SS)
  // - Action type (color-coded: navigate=cyan, click=amber, type=green, error=red)
  // - Grid cell (if click)
  // - Element tag + text (truncated)
  // - Duration
  // - Click on entry → show full details (element attrs, screenshot, pixel coords)
}
```

---

## TASK 9: Cortex Integration — Crawler as a Tool

Register the crawler as a tool that Cortex can use in the agent loop.
This is how Cortex autonomously controls the browser.

```go
// internal/tools/crawler_tool.go

// Tool: "web_crawl"
// Description: Browse the web visually. Navigate to URLs, click elements by grid
// coordinate, type text, and scroll. Every action is logged with screenshots.
//
// Actions:
//   navigate: Go to a URL
//   screenshot: Get current page screenshot with grid overlay
//   click: Click a grid cell (e.g. "C7")
//   type: Type text into the focused element
//   scroll: Scroll up/down/left/right
//   read: Extract all visible text from the current page
//   elements: List all interactive elements with their grid positions
//
// Example tool calls from Cortex:
//   {"action": "navigate", "url": "https://developers.facebook.com/apps/"}
//   {"action": "screenshot"}  // returns screenshot with grid labels
//   {"action": "click", "grid": "D5"}
//   {"action": "type", "text": "hello@example.com"}
//   {"action": "elements"} // returns [{tag: "button", text: "Submit", grid: ["C7","C8"]}]

type CrawlerTool struct {
    crawler *crawler.Crawler
}

func (t *CrawlerTool) Name() string { return "web_crawl" }

func (t *CrawlerTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
    // Parse action from input
    // Dispatch to crawler methods
    // Return result as formatted string for Cortex to process
    //
    // For "screenshot" action: return the grid-labeled screenshot as a description
    //   "Current page: Google Search Results. Grid positions of interactive elements:
    //    B3: <input> search box (focused)
    //    C7: <button> 'Google Search'
    //    D4: <a> 'Images'
    //    D5: <a> 'Videos'
    //    ..."
    //
    // For "elements" action: return structured list of clickable elements + grid positions
    // This lets Cortex SEE the page through the grid without needing vision models.
}
```

### The `elements` Action Is Key
When Cortex calls `{"action": "elements"}`, the crawler:
1. Finds all interactive elements (a, button, input, select, [onclick], [role=button])
2. Gets their bounding boxes
3. Maps each to grid cells
4. Returns a compact list: `B3: <input> placeholder="Search" | C7: <button> "Submit"`

This means Cortex can "see" the page as a text grid without needing OCR or vision models.
It's like giving the AI a screen reader with coordinate awareness.

---

## TASK 10: Auto-Split for Crawl Mode in PaneManager

When Cortex starts using the `web_crawl` tool, PaneManager auto-splits into:
- Chat (40%) + BrowserPane (40%) + CrawlLogPane (20%)

```tsx
// In PaneManager.tsx, add to the event handling:
// When event type is "crawl_screenshot" or "crawl_action":
//   If BrowserPane not open → auto-split to show it
//   If CrawlLogPane not open → add it as a side panel

// Add 'browser' and 'crawl_log' to PaneType union
// Add to PANE_TEMPLATES
```

---

## Definition of Done

1. `go build ./...` passes clean
2. `npx vite build` passes clean (in internal/webui/frontend/)
3. `go test ./...` passes
4. Manual test: Start ZBOT, type "browse to google.com and search for ZBOT"
5. BrowserPane auto-opens showing live screenshots of the browser
6. Grid overlay visible with labeled cells
7. CrawlLogPane shows every action with timestamps
8. Cortex successfully navigates, clicks by grid cell, and types text
9. Full action log exportable via GET /api/crawler/log
10. No Chrome windows pop up on the desktop (headless + screenshot stream)

---

## Architecture Reference

- **Existing scraper:** internal/scraper/browser.go (go-rod, headless — KEEP as-is for simple HTML fetching)
- **Event bus:** internal/agent/eventbus.go (MemEventBus — ADD new event types)
- **Event types:** internal/agent/ports.go (ADD crawl event constants)
- **SSE endpoint:** internal/webui/events_handler.go (already streams all event types)
- **PaneManager:** internal/webui/frontend/src/components/PaneManager.tsx (ADD browser + crawl_log pane types)
- **useEventBus hook:** internal/webui/frontend/src/hooks/useEventBus.ts (already works for new event types)
- **Types:** internal/webui/frontend/src/lib/types.ts (ADD CrawlEvent, ElementInfo types)

## DO NOT

- Modify the existing scraper/browser.go (that's for simple headless fetching)
- Open a visible Chrome window on the desktop (use headless + screenshot streaming)
- Store screenshots in memory longer than needed (base64 → emit → discard)
- Make more than 1 request per 500ms to any single domain (built-in rate limit)
- Break existing chat, streaming, or event bus functionality
- Push to main without testing build + tests

## IMPORTANT ORDER OF OPERATIONS

Build backend first, frontend second. Recommended order:
1. grid.go (pure logic, easy to test)
2. events.go (event types, no deps)
3. crawler.go (core, depends on grid + events)
4. actions.go (depends on crawler)
5. screenshot.go (depends on crawler)
6. logger.go (depends on events)
7. session.go (ties it together)
8. crawler_handler.go (HTTP API)
9. crawler_tool.go (Cortex integration)
10. BrowserPane.tsx + CrawlLogPane.tsx + useCrawler.ts (frontend)
11. PaneManager.tsx updates (auto-split)
12. Integration test: full crawl flow through chat

---

## Cost & Performance Notes

- go-rod headless Chromium: ~100MB RAM for one tab
- Screenshots at 2fps: ~200KB per JPEG (quality 60), ~400KB/sec bandwidth
  - Use JPEG not PNG for streaming (5x smaller)
  - PNG only for action log archives
- Grid overlay rendering: <1ms in Go image/draw
- Total additional memory: ~150MB per active crawl session
- CPU: negligible (go-rod does the heavy lifting via CDP)
