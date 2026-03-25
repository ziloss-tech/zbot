package scraper

import (
	"context"
	"testing"
	"time"
)

func TestNewDomainRateLimiter(t *testing.T) {
	limiter := NewDomainRateLimiter(2 * time.Second)

	if limiter == nil {
		t.Fatal("expected non-nil limiter")
	}
	if limiter.defaultDelay != 2*time.Second {
		t.Errorf("expected delay 2s, got %v", limiter.defaultDelay)
	}
	if len(limiter.buckets) != 0 {
		t.Errorf("expected empty buckets, got %d", len(limiter.buckets))
	}
}

func TestRateLimiterFirstRequest(t *testing.T) {
	limiter := NewDomainRateLimiter(1 * time.Second)
	ctx := context.Background()

	// First request should never wait
	start := time.Now()
	err := limiter.Wait(ctx, "example.com")
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if elapsed > 100*time.Millisecond {
		t.Errorf("first request took too long: %v", elapsed)
	}
}

func TestRateLimiterSecondRequestWaits(t *testing.T) {
	delay := 100 * time.Millisecond
	limiter := NewDomainRateLimiter(delay)
	ctx := context.Background()

	// First request
	limiter.Wait(ctx, "example.com")

	// Second request should wait approximately delay duration
	start := time.Now()
	err := limiter.Wait(ctx, "example.com")
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	// Allow for some timing variance
	if elapsed < delay*9/10 {
		t.Errorf("request waited too short: %v (expected ~%v)", elapsed, delay)
	}
	if elapsed > delay*11/10 {
		t.Errorf("request waited too long: %v (expected ~%v)", elapsed, delay)
	}
}

func TestRateLimiterMultipleDomains(t *testing.T) {
	limiter := NewDomainRateLimiter(100 * time.Millisecond)
	ctx := context.Background()

	// Request from two different domains should not interfere
	start := time.Now()
	limiter.Wait(ctx, "example.com")
	limiter.Wait(ctx, "example.org") // Different domain
	elapsed := time.Since(start)

	// Both should be fast since they're different domains
	if elapsed > 50*time.Millisecond {
		t.Errorf("different domains interfered, elapsed: %v", elapsed)
	}
}

func TestRateLimiterContextCancellation(t *testing.T) {
	limiter := NewDomainRateLimiter(1 * time.Second)

	// Make first request to populate bucket
	ctx := context.Background()
	limiter.Wait(ctx, "example.com")

	// Cancel context before second request needs to wait
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := limiter.Wait(ctx, "example.com")

	if err == nil {
		t.Error("expected context cancellation error")
	}
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestRateLimiterCleanup(t *testing.T) {
	limiter := NewDomainRateLimiter(100 * time.Millisecond)
	ctx := context.Background()

	// Add some entries
	limiter.Wait(ctx, "example.com")
	limiter.Wait(ctx, "example.org")

	// Verify entries exist
	if len(limiter.buckets) != 2 {
		t.Fatalf("expected 2 buckets, got %d", len(limiter.buckets))
	}

	// Force cleanup by moving time forward (manually update times)
	limiter.mu.Lock()
	limiter.buckets["example.com"] = time.Now().Add(-2 * time.Hour)
	limiter.buckets["example.org"] = time.Now().Add(-2 * time.Hour)
	limiter.mu.Unlock()

	limiter.Cleanup()

	limiter.mu.Lock()
	if len(limiter.buckets) != 0 {
		t.Errorf("expected 0 buckets after cleanup, got %d", len(limiter.buckets))
	}
	limiter.mu.Unlock()
}

func TestRateLimiterCleanupKeepsRecent(t *testing.T) {
	limiter := NewDomainRateLimiter(100 * time.Millisecond)
	ctx := context.Background()

	// Add some entries
	limiter.Wait(ctx, "example.com")
	limiter.Wait(ctx, "example.org")

	// Make one entry old, keep the other recent
	limiter.mu.Lock()
	limiter.buckets["example.com"] = time.Now().Add(-2 * time.Hour) // Old
	// example.org stays recent
	limiter.mu.Unlock()

	limiter.Cleanup()

	limiter.mu.Lock()
	if len(limiter.buckets) != 1 {
		t.Errorf("expected 1 bucket after cleanup, got %d", len(limiter.buckets))
	}
	if _, exists := limiter.buckets["example.org"]; !exists {
		t.Error("expected example.org to remain after cleanup")
	}
	limiter.mu.Unlock()
}

func TestRateLimiterSequentialRequests(t *testing.T) {
	limiter := NewDomainRateLimiter(50 * time.Millisecond)
	ctx := context.Background()

	start := time.Now()
	for i := 0; i < 3; i++ {
		limiter.Wait(ctx, "example.com")
	}
	elapsed := time.Since(start)

	// 3 requests with 50ms delay between each: ~100ms total
	// Allow some variance
	expectedMin := 90 * time.Millisecond
	expectedMax := 150 * time.Millisecond

	if elapsed < expectedMin || elapsed > expectedMax {
		t.Errorf("sequential requests took %v, expected %v-%v", elapsed, expectedMin, expectedMax)
	}
}

func TestRateLimiterZeroDelay(t *testing.T) {
	// Zero delay should still work (no artificial waiting)
	limiter := NewDomainRateLimiter(0)
	ctx := context.Background()

	start := time.Now()
	for i := 0; i < 5; i++ {
		limiter.Wait(ctx, "example.com")
	}
	elapsed := time.Since(start)

	// Should be very fast with no delay
	if elapsed > 50*time.Millisecond {
		t.Errorf("zero-delay requests took too long: %v", elapsed)
	}
}

func TestRateLimiterConcurrency(t *testing.T) {
	limiter := NewDomainRateLimiter(10 * time.Millisecond)
	ctx := context.Background()

	// Simulate concurrent requests (this is a basic test)
	done := make(chan error, 10)

	for i := 0; i < 10; i++ {
		go func() {
			err := limiter.Wait(ctx, "example.com")
			done <- err
		}()
	}

	// Collect results
	for i := 0; i < 10; i++ {
		err := <-done
		if err != nil {
			t.Errorf("concurrent request failed: %v", err)
		}
	}
}

func TestRateLimiterBucketTracking(t *testing.T) {
	limiter := NewDomainRateLimiter(100 * time.Millisecond)
	ctx := context.Background()

	// Make a request to track the domain
	limiter.Wait(ctx, "example.com")

	limiter.mu.Lock()
	if len(limiter.buckets) != 1 {
		t.Errorf("expected 1 bucket, got %d", len(limiter.buckets))
	}

	nextTime, exists := limiter.buckets["example.com"]
	if !exists {
		t.Fatal("expected example.com in buckets")
	}

	// Next allowed time should be in the future
	if !nextTime.After(time.Now()) {
		t.Errorf("next allowed time should be in future, got %v", nextTime)
	}
	limiter.mu.Unlock()
}
