package scraper

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"time"
)

// Retry executes fn up to maxAttempts times with exponential backoff.
// Retries on: 429 (rate limit), 503 (unavailable), network errors.
// Does NOT retry on: 404, 403, 401 (permanent failures).
// Backoff: 1s, 2s, 4s, 8s... with ±20% jitter.
func Retry(ctx context.Context, maxAttempts int, fn func() (*http.Response, error)) (*http.Response, error) {
	var lastErr error

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			// Exponential backoff: 1s, 2s, 4s, 8s...
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			// Add ±20% jitter.
			jitter := time.Duration(float64(backoff) * (0.8 + 0.4*rand.Float64()))

			select {
			case <-time.After(jitter):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		resp, err := fn()
		if err != nil {
			// Network error — retryable.
			lastErr = err
			continue
		}

		// Check if we should retry based on status code.
		switch resp.StatusCode {
		case http.StatusTooManyRequests, // 429
			http.StatusServiceUnavailable, // 503
			http.StatusBadGateway,         // 502
			http.StatusGatewayTimeout:     // 504
			// Retryable — close body and try again.
			resp.Body.Close()
			lastErr = fmt.Errorf("HTTP %d (attempt %d/%d)", resp.StatusCode, attempt+1, maxAttempts)
			continue

		default:
			// Not retryable (including success) — return as-is.
			return resp, nil
		}
	}

	return nil, fmt.Errorf("all %d attempts failed: %w", maxAttempts, lastErr)
}
