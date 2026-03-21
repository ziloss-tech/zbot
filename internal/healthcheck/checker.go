package healthcheck

import (
	"context"
	"sort"
	"sync"
	"time"
)

type HealthChecker struct {
	mu       sync.Mutex
	checks   map[string]CheckFunc
	timeout  time.Duration
}

func NewHealthChecker() *HealthChecker {
	return &HealthChecker{
		checks:   make(map[string]CheckFunc),
		timeout:  5 * time.Second,
	}
}

func (h *HealthChecker) Register(name string, fn CheckFunc) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.checks[name] = fn
}

func (h *HealthChecker) RunAll() []Check {
	var wg sync.WaitGroup
	results := make(chan Check, len(h.checks))
	h.mu.Lock()
	defer h.mu.Unlock()

	for name, checkFunc := range h.checks {
		wg.Add(1)
		go func(name string, checkFunc CheckFunc) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), h.timeout)
			defer cancel()
			resultChan := make(chan Check, 1)
			go func() {
				resultChan <- checkFunc()
			}()
			select {
			case result := <-resultChan:
				results <- result
			case <-ctx.Done():
				results <- Check{
					Name:    name,
					Status:  "down",
					Message: "timeout",
				}
			}
		}(name, checkFunc)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var checks []Check
	for result := range results {
		checks = append(checks, result)
	}

	sort.Slice(checks, func(i, j int) bool {
		return checks[i].Name < checks[j].Name
	})

	return checks
}