# COWORKER PROMPT — Hawkeye Sprint (Visual Crawler)

You are working on ZBOT, an AI agent platform written in Go (backend) and React/TypeScript (frontend).

## Your Mission

Build a **Visual Web Crawler** system that runs INSIDE the ZBOT UI as a pane. The crawler uses go-rod (already a dependency) in headless mode but streams screenshots to the frontend at ~2fps, so the user can watch exactly what the crawler is doing. A **grid overlay** divides the viewport into labeled cells (A1, B3, C7, etc.) so that click targets are specified by grid coordinate — never by CSS selector. **Every single action is logged** with timestamps, element metadata, and screenshots.

## Read This First

Before writing any code, read the full sprint brief:
```
cat ~/Desktop/Projects/zbot/docs/SPRINT_CRAWLER.md
```

Also read these files for architecture context:
```
cat ~/Desktop/Projects/zbot/docs/ARCHITECTURE.md
cat ~/Desktop/Projects/zbot/internal/agent/eventbus.go
cat ~/Desktop/Projects/zbot/internal/agent/ports.go
cat ~/Desktop/Projects/zbot/internal/scraper/browser.go
cat ~/Desktop/Projects/zbot/internal/webui/frontend/src/components/PaneManager.tsx
cat ~/Desktop/Projects/zbot/internal/webui/frontend/src/lib/types.ts
cat ~/Desktop/Projects/zbot/internal/webui/frontend/src/hooks/useEventBus.ts
cat ~/Desktop/Projects/zbot/internal/webui/server.go
```

## Branch

```
cd ~/Desktop/Projects/zbot
git checkout public-release
git checkout -b feat/visual-crawler
```

## Build Order (DO THIS IN ORDER)

### Phase 1: Core Grid + Events (pure logic, testable in isolation)

**1. `internal/crawler/grid.go`**
- Grid struct: configurable cell size (default 64px), auto-computes rows/cols from viewport
- `CellFromLabel(label string) → GridCell` — parses "C7" into row=2, col=6, centerX, centerY
- `LabelFromPixel(x, y int) → string` — reverse: pixel coords to grid label
- `AllCells() → []GridCell` — enumerate all cells for overlay rendering
- `OverlayData() → GridOverlayJSON` — JSON for frontend grid rendering
- Write tests: `internal/crawler/grid_test.go` — test parsing, edge cases, round-trips

**2. `internal/crawler/events.go`**
- Define CrawlEvent struct (sessionID, action, gridCell, URL, elementInfo, screenshot, status, timestamp)
- Define ElementInfo struct (tag, text, attrs, boundingBox, gridCells)
- Define CrawlerStatus constants (idle, navigating, acting, waiting)
- Define event type constants: EventCrawlScreenshot, EventCrawlAction, EventCrawlStatus, EventCrawlError
- These must integrate with the existing AgentEvent type in `internal/agent/ports.go`

### Phase 2: Crawler Engine (go-rod integration)

**3. `internal/crawler/crawler.go`**
- Crawler struct wrapping go-rod browser in HEADLESS mode
- NewCrawler(eventBus, sessionID, viewportW, viewportH) — launches headless Chrome via go-rod
- Navigate(url) — go to URL, wait for stable, capture screenshot, emit event
- Click(gridLabel) — map grid cell to pixels, click via go-rod, capture before/after screenshots, emit event with element info
- Type(text) — type into focused element, emit event
- Scroll(direction, amount) — scroll, emit event
- Screenshot() — capture current state as JPEG base64
- ElementAtGrid(gridLabel) — use go-rod to get DOM element at pixel, return ElementInfo
- Close() — tear down browser
- IMPORTANT: all actions must emit events through the eventBus parameter

**4. `internal/crawler/actions.go`**
- InteractiveElements(page) — find all a, button, input, select, [onclick], [role=button] elements
- MapElementsToGrid(elements, grid) — get bounding boxes, map each to grid cells
- FormatElementList(mappedElements) — text format for Cortex: "B3: <input> placeholder='Search' | C7: <button> 'Submit'"

**5. `internal/crawler/screenshot.go`**
- ScreenshotStreamer: goroutine that captures JPEG screenshots at targetFPS (default 2)
- Emits EventCrawlScreenshot events with base64 JPEG
- RenderGridOverlay(jpegBytes, grid) — composites grid lines + labels onto screenshot using Go image/draw
- Grid lines: semi-transparent cyan (#00d4ff at 30% opacity)
- Cell labels: white 8px text on dark bg at cell corners
- Use JPEG quality 60 for streaming (small), PNG for log archives

**6. `internal/crawler/logger.go`**
- ActionLogger struct: stores ActionEntry slice, thread-safe
- ActionEntry: id, timestamp, action, gridCell, pixelX/Y, URL, input, elementTag/Text/Attrs, screenshotB64, success, error, durationMs, pageTitle
- Log(entry) — append + emit CrawlAction event
- Export() → JSON of full log
- Tail(n) → last N entries

**7. `internal/crawler/session.go`**
- SessionManager: manages multiple concurrent crawl sessions (map[string]*Crawler)
- StartSession(eventBus, viewport) → sessionID
- GetSession(sessionID) → *Crawler
- StopSession(sessionID) — calls Close(), removes from map
- ListSessions() → []SessionInfo

### Phase 3: HTTP API + Tool Integration

**8. `internal/webui/crawler_handler.go`**
- POST /api/crawler/start → creates session, returns sessionID + grid config
- POST /api/crawler/navigate → navigate URL
- POST /api/crawler/click → click grid cell
- POST /api/crawler/type → type text
- POST /api/crawler/scroll → scroll
- GET /api/crawler/screenshot?session_id=...&grid=true → current screenshot
- GET /api/crawler/log?session_id=...&tail=50 → action log
- POST /api/crawler/stop → stop session
- Mount these in server.go

**9. `internal/tools/crawler_tool.go`**
- Register "web_crawl" as a tool in the agent's tool registry
- Actions: navigate, screenshot, click, type, scroll, read (extract page text), elements (list interactive elements with grid positions)
- The "elements" action is critical — it lets Cortex "see" the page as text grid positions
- The "screenshot" action should return a text description of the page + element positions (not raw image)
- Wire into the existing tool registration in wire.go or wherever tools are registered

### Phase 4: Frontend

**10. `internal/webui/frontend/src/hooks/useCrawler.ts`**
- Hook that listens for crawl events via the existing useEventBus hook
- Maintains state: screenshot, currentURL, status, gridConfig, actionLog
- Provides functions: startCrawl, navigate, click, type, scroll, stopCrawl

**11. `internal/webui/frontend/src/components/BrowserPane.tsx`**
- URL bar at top (editable, Enter to navigate)
- Screenshot viewer (img element, updates from useCrawler hook)
- Grid overlay (CSS grid positioned absolutely over screenshot, clickable cells)
- Grid cells: transparent with 1px cyan border, highlight on hover, show label on hover
- Mini toolbar: back, forward, refresh, toggle grid, status indicator
- Styling: dark theme (#0a0a1a bg, #00d4ff accents) matching ZBOT design system

**12. `internal/webui/frontend/src/components/CrawlLogPane.tsx`**
- Auto-scrolling list of ActionEntry items
- Each entry: timestamp (HH:MM:SS) + action type (color coded) + details + duration
- Color coding: navigate=cyan, click=amber, type=green, scroll=blue, error=red
- Click entry to expand full details (element attrs, grid cell, pixel coords)

**13. Update `PaneManager.tsx`**
- Add 'browser' and 'crawl_log' to PaneType union
- Add to PANE_TEMPLATES: browser: { label: 'Browser', icon: '🌐' }, crawl_log: { label: 'Crawl Log', icon: '📋' }
- Auto-split when crawl events are detected: Chat (40%) + BrowserPane (40%) + CrawlLogPane (20%)
- Import BrowserPane and CrawlLogPane, add cases in pane rendering switch

**14. Update `types.ts`**
- Add CrawlEvent, ElementInfo, ActionEntry, GridConfig, CrawlerStatus types

## Definition of Done

1. `go build ./...` passes
2. `cd internal/webui/frontend && npx vite build` passes
3. `go test ./...` passes (including new crawler tests)
4. Start ZBOT, send chat: "browse to https://example.com"
5. BrowserPane auto-opens showing live screenshots
6. Grid overlay visible and clickable
7. CrawlLogPane shows actions in real time
8. Send: "click the link that says 'More information...'" → Cortex uses web_crawl tool with correct grid cell
9. Full action log available at GET /api/crawler/log
10. No Chrome windows pop up on the desktop
11. Commit all changes to feat/visual-crawler branch

## DO NOT
- Modify internal/scraper/browser.go (keep existing headless fetcher)
- Open a visible Chrome window (headless + screenshot streaming only)
- Break existing chat/streaming/event bus functionality
- Add npm deps without checking bundle size first (exception: if you need a syntax highlight lib)
- Store full screenshots in memory — emit via SSE then discard
- Push directly to main

## Repo Location
~/Desktop/Projects/zbot
