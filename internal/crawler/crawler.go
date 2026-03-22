package crawler

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

// Crawler wraps a headless go-rod browser with grid navigation and action logging.
// Screenshots are streamed via an EventEmitter callback — no Chrome window opens.
type Crawler struct {
	browser   *rod.Browser
	page      *rod.Page
	grid      *Grid
	logger    *ActionLogger
	sessionID string
	status    CrawlerStatus
	mu        sync.RWMutex
	onEvent   func(CrawlEvent) // callback for event bus integration
	createdAt time.Time
}

// EventEmitter is a function that receives crawl events (for wiring to ZBOT's event bus).
type EventEmitter func(CrawlEvent)

// NewCrawler launches a headless browser and returns a Crawler.
func NewCrawler(viewportW, viewportH, cellSize int, emitter EventEmitter) (*Crawler, error) {
	path, _ := launcher.LookPath()
	if path == "" {
		return nil, fmt.Errorf("headless browser unavailable — install Chromium or Chrome")
	}

	l := launcher.New().Headless(true).Leakless(true)
	controlURL, err := l.Launch()
	if err != nil {
		return nil, fmt.Errorf("launch browser: %w", err)
	}

	browser := rod.New().ControlURL(controlURL)
	if err := browser.Connect(); err != nil {
		return nil, fmt.Errorf("connect browser: %w", err)
	}

	page, err := browser.Page(proto.TargetCreateTarget{})
	if err != nil {
		browser.Close()
		return nil, fmt.Errorf("create page: %w", err)
	}

	_ = page.SetViewport(&proto.EmulationSetDeviceMetricsOverride{
		Width: viewportW, Height: viewportH, DeviceScaleFactor: 1,
	})

	sid := make([]byte, 8)
	_, _ = rand.Read(sid)
	sessionID := fmt.Sprintf("crawl-%x", sid)

	c := &Crawler{
		browser:   browser,
		page:      page,
		grid:      NewGrid(viewportW, viewportH, cellSize),
		logger:    NewActionLogger(),
		sessionID: sessionID,
		status:    StatusIdle,
		onEvent:   emitter,
		createdAt: time.Now(),
	}

	c.emit(CrawlEvent{
		SessionID: sessionID,
		Type:      EventCrawlStatus,
		Status:    StatusIdle,
		Timestamp: time.Now(),
	})

	return c, nil
}

func (c *Crawler) emit(ev CrawlEvent) {
	if c.onEvent != nil {
		c.onEvent(ev)
	}
}

// SessionID returns the crawler's unique session identifier.
func (c *Crawler) SessionID() string { return c.sessionID }

// Grid returns the current grid configuration.
func (c *Crawler) Grid() *Grid { return c.grid }

// Status returns the current crawler status.
func (c *Crawler) Status() CrawlerStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.status
}

func (c *Crawler) setStatus(s CrawlerStatus) {
	c.mu.Lock()
	c.status = s
	c.mu.Unlock()
	c.emit(CrawlEvent{
		SessionID: c.sessionID,
		Type:      EventCrawlStatus,
		Status:    s,
		Timestamp: time.Now(),
	})
}

// Logger returns the action logger.
func (c *Crawler) Logger() *ActionLogger { return c.logger }

// CreatedAt returns when this session was created.
func (c *Crawler) CreatedAt() time.Time { return c.createdAt }

// Navigate goes to a URL, waits for stability, captures a screenshot, and emits an event.
func (c *Crawler) Navigate(rawURL string) error {
	c.setStatus(StatusNavigating)
	start := time.Now()

	if err := c.page.Navigate(rawURL); err != nil {
		c.logAction("navigate", "", rawURL, false, err.Error(), time.Since(start))
		c.setStatus(StatusIdle)
		return fmt.Errorf("navigate: %w", err)
	}

	_ = c.page.WaitStable(800 * time.Millisecond)
	dur := time.Since(start)

	title := c.pageTitle()
	url := c.currentURL()
	c.logAction("navigate", "", url, true, "", dur)

	shot, _ := c.ScreenshotJPEG()
	c.emit(CrawlEvent{
		SessionID:  c.sessionID,
		Type:       EventCrawlAction,
		Action:     "navigate",
		URL:        url,
		PageTitle:  title,
		Screenshot: shot,
		DurationMs: dur.Milliseconds(),
		Timestamp:  time.Now(),
	})

	c.setStatus(StatusIdle)
	return nil
}

// Click clicks the center of a grid cell, logs the action with element info.
func (c *Crawler) Click(gridLabel string) (*ElementInfo, error) {
	cell, err := c.grid.CellFromLabel(gridLabel)
	if err != nil {
		return nil, err
	}

	c.setStatus(StatusActing)
	start := time.Now()

	// Get element info at click point before clicking.
	info := c.elementAtPixel(cell.CenterX, cell.CenterY)

	// Click at the pixel coordinates using go-rod's mouse.
	err = c.page.Mouse.MoveTo(proto.NewPoint(float64(cell.CenterX), float64(cell.CenterY)))
	if err == nil {
		err = c.page.Mouse.Click(proto.InputMouseButtonLeft, 1)
	}

	// Brief wait for any navigation or DOM update.
	_ = c.page.WaitStable(400 * time.Millisecond)
	dur := time.Since(start)

	success := err == nil
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}

	c.logActionFull("click", gridLabel, cell.CenterX, cell.CenterY, c.currentURL(), "", info, success, errMsg, dur)

	shot, _ := c.ScreenshotJPEG()
	c.emit(CrawlEvent{
		SessionID:   c.sessionID,
		Type:        EventCrawlAction,
		Action:      "click",
		GridCell:    gridLabel,
		URL:         c.currentURL(),
		ElementInfo: info,
		Screenshot:  shot,
		DurationMs:  dur.Milliseconds(),
		PageTitle:   c.pageTitle(),
		Timestamp:   time.Now(),
	})

	c.setStatus(StatusIdle)
	if err != nil {
		return info, fmt.Errorf("click %s: %w", gridLabel, err)
	}
	return info, nil
}

// Type types text into the currently focused element.
func (c *Crawler) Type(text string) error {
	c.setStatus(StatusActing)
	start := time.Now()

	err := c.page.InsertText(text)
	dur := time.Since(start)

	success := err == nil
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}

	c.logAction("type", "", text, success, errMsg, dur)
	c.emit(CrawlEvent{
		SessionID:  c.sessionID,
		Type:       EventCrawlAction,
		Action:     "type",
		URL:        c.currentURL(),
		DurationMs: dur.Milliseconds(),
		Timestamp:  time.Now(),
	})

	c.setStatus(StatusIdle)
	return err
}

// Scroll scrolls the page in the given direction.
func (c *Crawler) Scroll(direction string, amount int) error {
	c.setStatus(StatusActing)
	start := time.Now()

	var dx, dy float64
	switch strings.ToLower(direction) {
	case "down":
		dy = float64(amount * 100)
	case "up":
		dy = -float64(amount * 100)
	case "right":
		dx = float64(amount * 100)
	case "left":
		dx = -float64(amount * 100)
	default:
		c.setStatus(StatusIdle)
		return fmt.Errorf("invalid scroll direction: %s", direction)
	}

	err := c.page.Mouse.Scroll(dx, dy, 1)
	_ = c.page.WaitStable(300 * time.Millisecond)
	dur := time.Since(start)

	success := err == nil
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}

	c.logAction("scroll", "", fmt.Sprintf("%s x%d", direction, amount), success, errMsg, dur)
	c.setStatus(StatusIdle)
	return err
}

// ScreenshotJPEG captures the current page as a base64-encoded JPEG.
func (c *Crawler) ScreenshotJPEG() (string, error) {
	quality := 60
	data, err := c.page.Screenshot(true, &proto.PageCaptureScreenshot{
		Format:  proto.PageCaptureScreenshotFormatJpeg,
		Quality: &quality,
	})
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

// ReadPageText extracts all visible text from the current page.
func (c *Crawler) ReadPageText() (string, error) {
	el, err := c.page.Element("body")
	if err != nil {
		return "", err
	}
	text, err := el.Text()
	if err != nil {
		return "", err
	}
	// Truncate to prevent enormous payloads.
	if len(text) > 8000 {
		text = text[:8000] + "\n... [truncated]"
	}
	return text, nil
}

// InteractiveElements finds all clickable elements and maps them to grid cells.
func (c *Crawler) InteractiveElements() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = ctx

	js := `(() => {
		const selectors = 'a, button, input, select, textarea, [onclick], [role="button"], [tabindex]';
		const els = document.querySelectorAll(selectors);
		const results = [];
		for (const el of els) {
			const rect = el.getBoundingClientRect();
			if (rect.width === 0 || rect.height === 0) continue;
			const text = (el.innerText || el.value || el.placeholder || el.title || '').trim().substring(0, 60);
			if (!text && el.tagName !== 'INPUT') continue;
			results.push({
				tag: el.tagName.toLowerCase(),
				text: text,
				type: el.type || '',
				href: el.href || '',
				cx: Math.round(rect.left + rect.width / 2),
				cy: Math.round(rect.top + rect.height / 2),
			});
		}
		return JSON.stringify(results.slice(0, 80));
	})()`

	res, err := c.page.Eval(js)
	if err != nil {
		return "", fmt.Errorf("eval interactive elements: %w", err)
	}

	// Parse and map to grid labels.
	var elements []struct {
		Tag  string `json:"tag"`
		Text string `json:"text"`
		Type string `json:"type"`
		Href string `json:"href"`
		Cx   int    `json:"cx"`
		Cy   int    `json:"cy"`
	}

	if err := res.Value.Unmarshal(&elements); err != nil {
		return res.Value.String(), nil // fallback: return raw
	}

	var sb strings.Builder
	for _, el := range elements {
		label := c.grid.LabelFromPixel(el.Cx, el.Cy)
		desc := fmt.Sprintf("%s: <%s>", label, el.Tag)
		if el.Type != "" {
			desc += fmt.Sprintf(" type=%s", el.Type)
		}
		if el.Text != "" {
			desc += fmt.Sprintf(" %q", el.Text)
		}
		if el.Href != "" && len(el.Href) < 80 {
			desc += fmt.Sprintf(" href=%s", el.Href)
		}
		sb.WriteString(desc)
		sb.WriteByte('\n')
	}
	return sb.String(), nil
}

// CurrentURL returns the current page URL.
func (c *Crawler) CurrentURL() string { return c.currentURL() }

func (c *Crawler) currentURL() string {
	info, err := c.page.Info()
	if err != nil {
		return ""
	}
	return info.URL
}

func (c *Crawler) pageTitle() string {
	info, err := c.page.Info()
	if err != nil {
		return ""
	}
	return info.Title
}

// elementAtPixel gets DOM element info at the given pixel via JavaScript.
func (c *Crawler) elementAtPixel(x, y int) *ElementInfo {
	js := fmt.Sprintf(`(() => {
		const el = document.elementFromPoint(%d, %d);
		if (!el) return JSON.stringify(null);
		const attrs = {};
		for (const a of el.attributes || []) {
			if (['href','class','id','name','type','value','placeholder','role','aria-label'].includes(a.name)) {
				attrs[a.name] = a.value.substring(0, 120);
			}
		}
		return JSON.stringify({
			tag: el.tagName.toLowerCase(),
			text: (el.innerText || el.value || '').trim().substring(0, 100),
			attrs: attrs,
		});
	})()`, x, y)

	res, err := c.page.Eval(js)
	if err != nil {
		return nil
	}

	var info struct {
		Tag   string            `json:"tag"`
		Text  string            `json:"text"`
		Attrs map[string]string `json:"attrs"`
	}
	if err := res.Value.Unmarshal(&info); err != nil {
		return nil
	}
	if info.Tag == "" {
		return nil
	}
	return &ElementInfo{Tag: info.Tag, Text: info.Text, Attrs: info.Attrs}
}

// logAction is a convenience for simple actions.
func (c *Crawler) logAction(action, gridCell, input string, success bool, errMsg string, dur time.Duration) {
	c.logActionFull(action, gridCell, 0, 0, c.currentURL(), input, nil, success, errMsg, dur)
}

// logActionFull logs an action with full metadata.
func (c *Crawler) logActionFull(action, gridCell string, px, py int, url, input string, info *ElementInfo, success bool, errMsg string, dur time.Duration) {
	entry := ActionEntry{
		ID:         fmt.Sprintf("act-%d", time.Now().UnixNano()),
		Timestamp:  time.Now(),
		Action:     action,
		GridCell:   gridCell,
		PixelX:     px,
		PixelY:     py,
		URL:        url,
		Input:      input,
		Success:    success,
		Error:      errMsg,
		DurationMs: dur.Milliseconds(),
		PageTitle:  c.pageTitle(),
	}
	if info != nil {
		entry.ElementTag = info.Tag
		entry.ElementText = info.Text
		entry.ElementAttrs = info.Attrs
	}
	c.logger.Log(entry)
}

// Close tears down the browser and marks the session stopped.
func (c *Crawler) Close() {
	c.setStatus(StatusStopped)
	if c.page != nil {
		_ = c.page.Close()
	}
	if c.browser != nil {
		_ = c.browser.Close()
	}
}
