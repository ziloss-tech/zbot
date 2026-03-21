package healthcheck

import (
	"testing"
	"time"
)

func TestHealthCheckerRegisterAndRunAll(t *testing.T) {
	hc := NewHealthChecker()

	// Register two checks.
	hc.Register("always_ok", func() Check {
		return Check{Name: "always_ok", Status: "ok", Message: "all good"}
	})
	hc.Register("always_down", func() Check {
		return Check{Name: "always_down", Status: "down", Message: "broken"}
	})

	results := hc.RunAll()
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// Results should be sorted by name.
	if results[0].Name != "always_down" {
		t.Errorf("expected first result 'always_down', got %q", results[0].Name)
	}
	if results[1].Name != "always_ok" {
		t.Errorf("expected second result 'always_ok', got %q", results[1].Name)
	}

	if results[0].Status != "down" {
		t.Errorf("expected 'always_down' status=down, got %q", results[0].Status)
	}
	if results[1].Status != "ok" {
		t.Errorf("expected 'always_ok' status=ok, got %q", results[1].Status)
	}
}

func TestHealthCheckerTimeout(t *testing.T) {
	hc := NewHealthChecker()
	hc.timeout = 100 * time.Millisecond

	// Register a check that hangs forever.
	hc.Register("slow_check", func() Check {
		time.Sleep(5 * time.Second)
		return Check{Name: "slow_check", Status: "ok"}
	})

	start := time.Now()
	results := hc.RunAll()
	elapsed := time.Since(start)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Status != "down" {
		t.Errorf("expected timeout to produce 'down' status, got %q", results[0].Status)
	}
	if results[0].Message != "timeout" {
		t.Errorf("expected timeout message, got %q", results[0].Message)
	}
	if elapsed > 1*time.Second {
		t.Errorf("timeout should have fired within ~100ms, took %v", elapsed)
	}
}

func TestHealthCheckerNoChecks(t *testing.T) {
	hc := NewHealthChecker()
	results := hc.RunAll()
	if len(results) != 0 {
		t.Errorf("expected 0 results for empty checker, got %d", len(results))
	}
}

func TestDiskCheckReturnsValidResult(t *testing.T) {
	// DiskCheck on the current directory should always work.
	check := DiskCheck(".", 0.001) // 0.001 GB = trivially low threshold
	result := check()

	if result.Name != "disk" {
		t.Errorf("expected name 'disk', got %q", result.Name)
	}
	if result.Status != "ok" && result.Status != "degraded" {
		t.Errorf("expected status 'ok' or 'degraded', got %q", result.Status)
	}
	if result.Message == "" {
		t.Error("expected non-empty message with free space info")
	}
}
