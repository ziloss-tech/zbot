package healthcheck

import "time"

// Check represents a single health check result.
type Check struct {
	Name    string        `json:"name"`
	Status  string        `json:"status"` // "ok", "degraded", "down"
	Latency time.Duration `json:"latency"`
	Message string        `json:"message,omitempty"`
}

// Checker runs all health checks and returns results.
type Checker interface {
	RunAll() []Check
	Register(name string, fn CheckFunc)
}

// CheckFunc is a function that performs a single health check.
type CheckFunc func() Check
