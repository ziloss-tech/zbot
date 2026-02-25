package scraper

import (
	"context"
	"sync"
	"time"
)

// DomainRateLimiter enforces per-domain request rate limits.
// Default: 1 request per 2 seconds per domain.
// Prevents hammering any single site.
type DomainRateLimiter struct {
	mu           sync.Mutex
	buckets      map[string]time.Time // domain → next allowed request time
	defaultDelay time.Duration
}

// NewDomainRateLimiter creates a rate limiter with the given per-domain delay.
func NewDomainRateLimiter(defaultDelay time.Duration) *DomainRateLimiter {
	return &DomainRateLimiter{
		buckets:      make(map[string]time.Time),
		defaultDelay: defaultDelay,
	}
}

// Wait blocks until it's safe to make a request to the given domain.
// Returns an error if the context is cancelled while waiting.
func (r *DomainRateLimiter) Wait(ctx context.Context, domain string) error {
	r.mu.Lock()
	nextAllowed, exists := r.buckets[domain]
	now := time.Now()

	if !exists || now.After(nextAllowed) {
		// No wait needed — mark next allowed time and proceed.
		r.buckets[domain] = now.Add(r.defaultDelay)
		r.mu.Unlock()
		return nil
	}

	// Need to wait.
	waitDuration := nextAllowed.Sub(now)
	r.buckets[domain] = nextAllowed.Add(r.defaultDelay)
	r.mu.Unlock()

	select {
	case <-time.After(waitDuration):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Cleanup removes entries older than 1 hour to prevent memory leak.
func (r *DomainRateLimiter) Cleanup() {
	r.mu.Lock()
	defer r.mu.Unlock()

	cutoff := time.Now().Add(-1 * time.Hour)
	for domain, nextAllowed := range r.buckets {
		if nextAllowed.Before(cutoff) {
			delete(r.buckets, domain)
		}
	}
}
