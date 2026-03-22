package crawler

import (
	"encoding/base64"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
	"github.com/ysmood/gson"
)

// Crawler is the main engine managing browser lifecycle and navigation.
type Crawler struct {
	browser    *rod.Browser
	page       *rod.Page
	grid       *Grid
	logger     *ActionLogger
	streamer   *ScreenshotStreamer
	sessionID  string
	eventBus   EventBus
	mu         sync.RWMutex
	status     CrawlerStatus
	viewport   ViewportSize
	lastAction time.Time
	rateLimit  time.Duration
}

// NewCrawler initializes a headless Chrome browser and returns a Crawler instance.
func NewCrawler(eventBus EventBus, sessionID string, viewport ViewportSize) (*Crawler, error) {
	// Launch headless Chrome via rod launcher
	u, err := launcher.New().Headless(true).Launch()
	if err != nil {
		return nil, fmt.Errorf("failed to launch Chrome: %w", err)
	}

	// Connect rod browser
	browser := rod.New().ControlURL(u)
	err = browser.Connect()
	if err != nil {
		return nil, fmt.Errorf("failed to connect browser: %w", err)
	}

	// Create a page
	page, err := browser.Page(proto.TargetCreateTarget{
		URL: "about:blank",
	})
	if err != nil {
		browser.MustClose()
		return nil, fmt.Errorf("failed to create page: %w", err)
	}

	// Set viewport size
	err = proto.EmulationSetDeviceMetricsOverride{
		Width:             int(viewport.Width),
		Height:            int(viewport.Height),
		DeviceScaleFactor: 1,
		Mobile:            false,
	}.Call(page)
	if err != nil {
		page.MustClose()
		browser.MustClose()
		return nil, fmt.Errorf("failed to set viewport: %w", err)
	}

	// Initialize Grid
	grid := NewGrid(viewport.Width, viewport.Height, 64)

	// Initialize ActionLogger
	logger := NewActionLogger(sessionID, eventBus)

	// Create Crawler
	c := &Crawler{
		browser:    browser,
		page:       page,
		grid:       grid,
		logger:     logger,
		sessionID:  sessionID,
		eventBus:   eventBus,
		status:     StatusIdle,
		viewport:   viewport,
		lastAction: time.Now(),
		rateLimit:  500 * time.Millisecond,
	}

	// Emit status event
	c.emitEvent(CrawlEvent{
		Type:      EventCrawlStatus,
		SessionID: sessionID,
		Status:    StatusIdle,
		Timestamp: time.Now(),
	})

	return c, nil
}

// Navigate navigates the page to the given URL.
func (c *Crawler) Navigate(url string) error {
	c.mu.Lock()
	c.setStatus(StatusNavigating)
	c.mu.Unlock()

	// Enforce rate limit
	c.enforceRateLimit()

	startTime := time.Now()

	// Navigate to URL
	err := c.page.Navigate(url)
	if err != nil {
		c.mu.Lock()
		c.setStatus(StatusIdle)
		c.mu.Unlock()
		return fmt.Errorf("navigation failed: %w", err)
	}

	// Wait for page load
	err = c.page.WaitLoad()
	if err != nil {
		c.mu.Lock()
		c.setStatus(StatusIdle)
		c.mu.Unlock()
		return fmt.Errorf("wait load failed: %w", err)
	}

	// Capture screenshot
	_, screenshotB64, err := c.captureScreenshot()
	if err != nil {
		c.mu.Lock()
		c.setStatus(StatusIdle)
		c.mu.Unlock()
		return fmt.Errorf("screenshot capture failed: %w", err)
	}

	// Get page title
	title := c.PageTitle()

	duration := time.Since(startTime)

	// Log action
	c.logger.Log(ActionEntry{
		Timestamp:     time.Now(),
		Action:        "navigate",
		URL:           url,
		PageTitle:     title,
		DurationMs:    duration.Milliseconds(),
		ScreenshotB64: screenshotB64,
	})

	c.mu.Lock()
	c.setStatus(StatusIdle)
	c.lastAction = time.Now()
	c.mu.Unlock()

	return nil
}

// Click clicks on the element at the given grid label.
func (c *Crawler) Click(gridLabel string) (*ClickResult, error) {
	c.mu.Lock()
	c.setStatus(StatusActing)
	c.mu.Unlock()

	// Resolve grid cell
	cell, err := c.grid.CellFromLabel(gridLabel)
	if err != nil {
		c.mu.Lock()
		c.setStatus(StatusIdle)
		c.mu.Unlock()
		return nil, fmt.Errorf("grid resolution failed: %w", err)
	}

	// Get pixel center of grid cell
	x, y := cell.CenterX, cell.CenterY

	// Capture BEFORE screenshot
	_, beforeB64, err := c.captureScreenshot()
	if err != nil {
		c.mu.Lock()
		c.setStatus(StatusIdle)
		c.mu.Unlock()
		return nil, fmt.Errorf("before screenshot failed: %w", err)
	}

	// Get element info at grid center
	elemInfo, err := c.ElementAtGrid(gridLabel)
	if err != nil {
		c.mu.Lock()
		c.setStatus(StatusIdle)
		c.mu.Unlock()
		return nil, fmt.Errorf("element info retrieval failed: %w", err)
	}

	// Click at pixel coordinates
	err = c.page.Mouse.MoveTo(proto.Point{X: float64(x), Y: float64(y)})
	if err != nil {
		c.mu.Lock()
		c.setStatus(StatusIdle)
		c.mu.Unlock()
		return nil, fmt.Errorf("mouse move failed: %w", err)
	}

	err = c.page.Mouse.Click(proto.InputMouseButtonLeft, 1)
	if err != nil {
		c.mu.Lock()
		c.setStatus(StatusIdle)
		c.mu.Unlock()
		return nil, fmt.Errorf("mouse click failed: %w", err)
	}

	// Wait briefly for page to settle
	time.Sleep(200 * time.Millisecond)

	// Capture AFTER screenshot
	_, afterB64, err := c.captureScreenshot()
	if err != nil {
		c.mu.Lock()
		c.setStatus(StatusIdle)
		c.mu.Unlock()
		return nil, fmt.Errorf("after screenshot failed: %w", err)
	}

	// Log action
	c.logger.Log(ActionEntry{
		Timestamp:     time.Now(),
		Action:        "click",
		GridCell:      gridLabel,
		ElementTag:    elemInfo.Tag,
		ElementText:   elemInfo.Text,
		PixelX:        x,
		PixelY:        y,
		ScreenshotB64: beforeB64,
		URL:           c.CurrentURL(),
	})

	c.mu.Lock()
	c.setStatus(StatusIdle)
	c.lastAction = time.Now()
	c.mu.Unlock()

	return &ClickResult{
		Element:      elemInfo,
		BeforeShot:   beforeB64,
		AfterShot:    afterB64,
		GridCell:     cell.Label,
		PixelX:       x,
		PixelY:       y,
		Success:      true,
	}, nil
}

// Type types text into the focused element.
func (c *Crawler) Type(text string) error {
	c.mu.Lock()
	c.setStatus(StatusActing)
	c.mu.Unlock()

	// Type text into focused element
	err := c.page.InsertText(text)
	if err != nil {
		c.mu.Lock()
		c.setStatus(StatusIdle)
		c.mu.Unlock()
		return fmt.Errorf("text insertion failed: %w", err)
	}

	// Capture screenshot after typing
	_, screenshotB64, err := c.captureScreenshot()
	if err != nil {
		c.mu.Lock()
		c.setStatus(StatusIdle)
		c.mu.Unlock()
		return fmt.Errorf("screenshot capture failed: %w", err)
	}

	// Log action
	c.logger.Log(ActionEntry{
		Timestamp:     time.Now(),
		Action:        "type",
		Input:         text,
		ScreenshotB64: screenshotB64,
		URL:           c.CurrentURL(),
	})

	c.mu.Lock()
	c.setStatus(StatusIdle)
	c.lastAction = time.Now()
	c.mu.Unlock()

	return nil
}

// Scroll scrolls the page in the given direction.
func (c *Crawler) Scroll(direction string, amount int) error {
	c.mu.Lock()
	c.setStatus(StatusActing)
	c.mu.Unlock()

	var deltaX, deltaY float64

	// Map direction to deltas
	switch strings.ToLower(direction) {
	case "down":
		deltaY = float64(amount * 100)
	case "up":
		deltaY = float64(-amount * 100)
	case "left":
		deltaX = float64(-amount * 100)
	case "right":
		deltaX = float64(amount * 100)
	default:
		c.mu.Lock()
		c.setStatus(StatusIdle)
		c.mu.Unlock()
		return fmt.Errorf("invalid scroll direction: %s", direction)
	}

	// Perform scroll
	err := c.page.Mouse.Scroll(deltaX, deltaY, 5)
	if err != nil {
		c.mu.Lock()
		c.setStatus(StatusIdle)
		c.mu.Unlock()
		return fmt.Errorf("scroll failed: %w", err)
	}

	// Wait briefly for scroll to settle
	time.Sleep(200 * time.Millisecond)

	// Capture screenshot after scroll
	_, screenshotB64, err := c.captureScreenshot()
	if err != nil {
		c.mu.Lock()
		c.setStatus(StatusIdle)
		c.mu.Unlock()
		return fmt.Errorf("screenshot capture failed: %w", err)
	}

	// Log action
	c.logger.Log(ActionEntry{
		Timestamp:     time.Now(),
		Action:        "scroll",
		Input:         fmt.Sprintf("%s:%d", direction, amount),
		ScreenshotB64: screenshotB64,
		URL:           c.CurrentURL(),
	})

	c.mu.Lock()
	c.setStatus(StatusIdle)
	c.lastAction = time.Now()
	c.mu.Unlock()

	return nil
}

// Screenshot captures the current page as a base64-encoded image.
func (c *Crawler) Screenshot() (string, error) {
	buf, err := c.page.Screenshot(false, &proto.PageCaptureScreenshot{
		Format:  proto.PageCaptureScreenshotFormatJpeg,
		Quality: gson.Int(60),
	})
	if err != nil {
		return "", fmt.Errorf("screenshot failed: %w", err)
	}

	b64 := base64.StdEncoding.EncodeToString(buf)
	return b64, nil
}

// ScreenshotWithGrid captures the page and overlays the grid.
func (c *Crawler) ScreenshotWithGrid() (string, error) {
	// Capture screenshot as PNG bytes
	buf, err := c.page.Screenshot(false, &proto.PageCaptureScreenshot{
		Format: proto.PageCaptureScreenshotFormatPng,
	})
	if err != nil {
		return "", fmt.Errorf("screenshot failed: %w", err)
	}

	// Render grid overlay
	overlayBytes, err := RenderGridOverlay(buf, c.grid)
	if err != nil {
		return "", fmt.Errorf("grid overlay failed: %w", err)
	}

	// Encode to base64
	b64 := base64.StdEncoding.EncodeToString(overlayBytes)
	return b64, nil
}

// ElementAtGrid retrieves element information at the grid cell center.
func (c *Crawler) ElementAtGrid(gridLabel string) (*ElementInfo, error) {
	// Resolve grid cell
	cell, err := c.grid.CellFromLabel(gridLabel)
	if err != nil {
		return nil, fmt.Errorf("grid resolution failed: %w", err)
	}

	// Get pixel center
	x, y := cell.CenterX, cell.CenterY

	// Evaluate JavaScript to get element at point
	jsCode := `
	(x, y) => {
		const elem = document.elementFromPoint(x, y);
		if (!elem) return null;

		const rect = elem.getBoundingClientRect();
		const attrs = {};
		for (let attr of elem.attributes) {
			attrs[attr.name] = attr.value;
		}

		return {
			tag: elem.tagName.toLowerCase(),
			text: elem.innerText || elem.textContent || '',
			html: elem.innerHTML,
			attributes: attrs,
			boundingBox: {
				x: Math.round(rect.left),
				y: Math.round(rect.top),
				width: Math.round(rect.width),
				height: Math.round(rect.height)
			}
		};
	}
	`

	result, err := c.page.Eval(jsCode, x, y)
	if err != nil {
		return nil, fmt.Errorf("element evaluation failed: %w", err)
	}

	if result.Value.Nil() {
		return &ElementInfo{
			Tag:  "unknown",
			Text: "",
		}, nil
	}

	// Parse result (result.Value is a gson.JSON object from CDP)
	// We'll extract the basic info from the eval result
	elemInfo := &ElementInfo{
		Tag: "unknown",
	}

	// Note: Detailed parsing of result.Value would require unmarshaling
	// For now, we'll do a simpler extraction using the gson JSON API
	data := result.Value.Map()
	if len(data) > 0 {
		if tagJSON, exists := data["tag"]; exists {
			elemInfo.Tag = tagJSON.String()
		}
		if textJSON, exists := data["text"]; exists {
			elemInfo.Text = textJSON.String()
		}
		if attrsJSON, exists := data["attributes"]; exists {
			attrs := attrsJSON.Map()
			elemInfo.Attrs = make(map[string]string)
			for k, v := range attrs {
				elemInfo.Attrs[k] = v.String()
			}
		}
		if bboxJSON, exists := data["boundingBox"]; exists && !bboxJSON.Nil() {
			bbox := bboxJSON.Map()
			elemInfo.BoundingBox = &Rect{
				X:      bbox["x"].Num(),
				Y:      bbox["y"].Num(),
				Width:  bbox["width"].Num(),
				Height: bbox["height"].Num(),
			}
			// Map bounding box to grid cells
			elemInfo.GridCells = c.grid.CellsFromRect(
				int(elemInfo.BoundingBox.X),
				int(elemInfo.BoundingBox.Y),
				int(elemInfo.BoundingBox.X + elemInfo.BoundingBox.Width),
				int(elemInfo.BoundingBox.Y + elemInfo.BoundingBox.Height),
			)
		}
	}

	return elemInfo, nil
}

// PageText extracts all visible text from the page.
func (c *Crawler) PageText() (string, error) {
	result, err := c.page.Eval("() => document.body.innerText")
	if err != nil {
		return "", fmt.Errorf("page text extraction failed: %w", err)
	}

	if result.Value.Nil() {
		return "", nil
	}

	return result.Value.String(), nil
}

// CurrentURL returns the current page URL.
func (c *Crawler) CurrentURL() string {
	result, err := c.page.Eval("() => window.location.href")
	if err != nil {
		return ""
	}

	if result.Value.Nil() {
		return ""
	}

	return result.Value.String()
}

// PageTitle returns the current page title.
func (c *Crawler) PageTitle() string {
	result, err := c.page.Eval("() => document.title")
	if err != nil {
		return ""
	}

	if result.Value.Nil() {
		return ""
	}

	return result.Value.String()
}

// Close closes the browser and releases resources.
func (c *Crawler) Close() {
	c.mu.Lock()
	c.setStatus(StatusStopped)
	c.mu.Unlock()

	if c.streamer != nil {
		c.streamer.Stop()
	}

	if c.page != nil {
		c.page.MustClose()
	}

	if c.browser != nil {
		c.browser.MustClose()
	}
}

// Status returns the current crawler status.
func (c *Crawler) Status() CrawlerStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.status
}

// Grid returns the grid instance.
func (c *Crawler) Grid() *Grid {
	return c.grid
}

// Logger returns the action logger instance.
func (c *Crawler) Logger() *ActionLogger {
	return c.logger
}

// setStatus sets the crawler status and emits a status event (must be called with lock held).
func (c *Crawler) setStatus(s CrawlerStatus) {
	c.status = s
	c.emitEvent(CrawlEvent{
		Type:      EventCrawlStatus,
		SessionID: c.sessionID,
		Status:    s,
		Timestamp: time.Now(),
	})
}

// enforceRateLimit sleeps if less than rateLimit time has passed since last action.
func (c *Crawler) enforceRateLimit() {
	c.mu.RLock()
	lastAction := c.lastAction
	rateLimit := c.rateLimit
	c.mu.RUnlock()

	elapsed := time.Since(lastAction)
	if elapsed < rateLimit {
		time.Sleep(rateLimit - elapsed)
	}
}

// emitEvent publishes an event through the event bus.
func (c *Crawler) emitEvent(event CrawlEvent) {
	if c.eventBus != nil {
		c.eventBus.Publish(c.sessionID, event)
	}
}

// captureScreenshot captures a screenshot and returns raw PNG bytes and base64-encoded JPEG.
func (c *Crawler) captureScreenshot() ([]byte, string, error) {
	// Capture as PNG for processing
	pngBuf, err := c.page.Screenshot(false, &proto.PageCaptureScreenshot{
		Format: proto.PageCaptureScreenshotFormatPng,
	})
	if err != nil {
		return nil, "", fmt.Errorf("PNG screenshot failed: %w", err)
	}

	// Capture as JPEG for base64 (smaller size)
	jpegBuf, err := c.page.Screenshot(false, &proto.PageCaptureScreenshot{
		Format:  proto.PageCaptureScreenshotFormatJpeg,
		Quality: gson.Int(60),
	})
	if err != nil {
		return nil, "", fmt.Errorf("JPEG screenshot failed: %w", err)
	}

	// Encode JPEG to base64
	b64 := base64.StdEncoding.EncodeToString(jpegBuf)

	return pngBuf, b64, nil
}
