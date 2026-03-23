package crawler

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"sync"
	"time"

	"github.com/ziloss-tech/zbot/internal/agent"
)

// ScreenshotStreamer handles periodic screenshot capture at 2fps
// and emits events with base64-encoded JPEG images.
type ScreenshotStreamer struct {
	crawler   *Crawler
	targetFPS int
	running   bool
	stopCh    chan struct{}
	mu        sync.Mutex
}

// NewScreenshotStreamer creates a new screenshot streamer.
// fps defaults to 2 if not specified.
func NewScreenshotStreamer(crawler *Crawler, fps int) *ScreenshotStreamer {
	if fps <= 0 {
		fps = 2
	}
	return &ScreenshotStreamer{
		crawler:   crawler,
		targetFPS: fps,
		running:   false,
		stopCh:    make(chan struct{}),
	}
}

// Start launches a goroutine that captures screenshots at the target FPS.
// Each screenshot is processed with grid overlay, converted to JPEG,
// encoded to base64, and emitted as an event.
func (s *ScreenshotStreamer) Start() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.stopCh = make(chan struct{})
	s.mu.Unlock()

	interval := time.Duration(1000/s.targetFPS) * time.Millisecond

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-s.stopCh:
				return
			case <-ticker.C:
				// Capture screenshot from crawler
				screenshotB64, err := s.crawler.Screenshot()
				if err != nil {
					// Log error but continue streaming
					continue
				}

				// Decode base64 to get PNG bytes for overlay
				screenshotPNG, err := base64.StdEncoding.DecodeString(screenshotB64)
				if err != nil {
					// Log error but continue streaming
					continue
				}

				// Render grid overlay onto the screenshot
				overlayPNG, err := RenderGridOverlay(screenshotPNG, s.crawler.Grid())
				if err != nil {
					// Log error but continue streaming
					continue
				}

				// Convert PNG to JPEG with quality 60
				jpegBytes, err := PNGtoJPEG(overlayPNG, 60)
				if err != nil {
					// Log error but continue streaming
					continue
				}

				// Encode JPEG to base64
				b64 := EncodeBase64(jpegBytes)

				// Emit event through crawler's event bus
				event := NewCrawlEvent(s.crawler.sessionID, agent.EventType("crawl_screenshot"), "screenshot captured", map[string]any{
					"screenshot": b64,
				})
				s.crawler.eventBus.Emit(context.Background(), event)
			}
		}
	}()
}

// Stop closes the streamer and stops capturing screenshots.
func (s *ScreenshotStreamer) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		s.running = false
		close(s.stopCh)
	}
}

// IsRunning returns whether the streamer is currently active.
func (s *ScreenshotStreamer) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// RenderGridOverlay composites grid lines onto a screenshot PNG.
// The grid is drawn with semi-transparent cyan lines at cell boundaries.
// Returns the modified PNG bytes.
func RenderGridOverlay(screenshotPNG []byte, grid *Grid) ([]byte, error) {
	if grid == nil || grid.CellWidth <= 0 || grid.CellHeight <= 0 {
		// No valid grid, return original screenshot
		return screenshotPNG, nil
	}

	// Decode PNG screenshot into an image.Image
	img, err := png.Decode(bytes.NewReader(screenshotPNG))
	if err != nil {
		return nil, fmt.Errorf("failed to decode PNG: %w", err)
	}

	// Get image bounds
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	// Create a new RGBA image with the same dimensions
	rgba := image.NewRGBA(bounds)

	// Draw the original screenshot onto the RGBA image
	draw.Draw(rgba, bounds, img, bounds.Min, draw.Src)

	// Grid line color: rgba(0, 212, 255, 77) — cyan at ~30% opacity
	gridColor := color.RGBA{R: 0, G: 212, B: 255, A: 77}

	// Draw vertical lines at every cellWidth interval
	for x := grid.CellWidth; x < width; x += grid.CellWidth {
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			rgba.SetRGBA(x, y, gridColor)
		}
	}

	// Draw horizontal lines at every cellHeight interval
	for y := grid.CellHeight; y < height; y += grid.CellHeight {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			rgba.SetRGBA(x, y, gridColor)
		}
	}

	// Encode the modified image back to PNG bytes
	var buf bytes.Buffer
	err = png.Encode(&buf, rgba)
	if err != nil {
		return nil, fmt.Errorf("failed to encode PNG: %w", err)
	}

	return buf.Bytes(), nil
}

// PNGtoJPEG decodes a PNG image and re-encodes it as JPEG
// with the specified quality (0-100).
func PNGtoJPEG(pngBytes []byte, quality int) ([]byte, error) {
	// Clamp quality to valid range
	if quality < 0 {
		quality = 0
	}
	if quality > 100 {
		quality = 100
	}

	// Decode PNG
	img, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to decode PNG: %w", err)
	}

	// Encode as JPEG
	var buf bytes.Buffer
	err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality})
	if err != nil {
		return nil, fmt.Errorf("failed to encode JPEG: %w", err)
	}

	return buf.Bytes(), nil
}

// EncodeBase64 encodes raw bytes to base64 string.
func EncodeBase64(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}