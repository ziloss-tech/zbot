package scraper

import (
	"context"
	"fmt"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

// BrowserFetcher fetches JavaScript-heavy pages using headless Chromium.
// Falls back gracefully if Chromium is not installed.
type BrowserFetcher struct {
	available bool // false if Chromium not found at init
}

// NewBrowserFetcher creates a browser fetcher.
// If Chromium is not installed, it still returns a fetcher but Fetch() will error.
func NewBrowserFetcher() *BrowserFetcher {
	// Check if a browser is available.
	path, _ := launcher.LookPath()
	return &BrowserFetcher{
		available: path != "",
	}
}

// Available returns true if headless Chromium is available.
func (f *BrowserFetcher) Available() bool {
	return f.available
}

// Fetch loads the URL in a headless browser and returns the rendered HTML.
// Waits for network idle (no requests for 500ms) before extracting content.
// Timeout: 30 seconds.
func (f *BrowserFetcher) Fetch(ctx context.Context, rawURL string) (string, error) {
	if !f.available {
		return "", fmt.Errorf("headless browser unavailable — install Chromium or Chrome")
	}

	// Create a timeout context for the browser operation.
	fetchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Launch a fresh browser instance.
	l := launcher.New().Headless(true).Leakless(true)
	controlURL, err := l.Launch()
	if err != nil {
		return "", fmt.Errorf("launch browser: %w", err)
	}

	browser := rod.New().ControlURL(controlURL)
	if err := browser.Connect(); err != nil {
		return "", fmt.Errorf("connect browser: %w", err)
	}
	defer browser.Close()

	// Navigate to the URL and wait for network idle.
	page, err := browser.Context(fetchCtx).Page(proto.TargetCreateTarget{})
	if err != nil {
		return "", fmt.Errorf("create page: %w", err)
	}

	// Set a realistic user agent.
	_ = page.SetUserAgent(&proto.NetworkSetUserAgentOverride{
		UserAgent: RandomUserAgent(),
	})

	if err := page.Navigate(rawURL); err != nil {
		return "", fmt.Errorf("navigate: %w", err)
	}

	// Wait for network to be idle (no requests for 500ms).
	if err := page.WaitStable(500 * time.Millisecond); err != nil {
		// Timeout is ok — page might have persistent connections.
		// We still try to get whatever has loaded.
	}

	// Extract the rendered HTML.
	html, err := page.HTML()
	if err != nil {
		return "", fmt.Errorf("get HTML: %w", err)
	}

	return html, nil
}
