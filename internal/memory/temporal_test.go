package memory

import (
	"testing"
	"time"

	"github.com/ziloss-tech/zbot/internal/agent"
)

func TestCalculateFreshness(t *testing.T) {
	cfg := DefaultFreshnessConfig()

	tests := []struct {
		name    string
		daysAgo int
		wantMin float64
		wantMax float64
	}{
		{"just created", 0, 0.99, 1.01},
		{"1 day old", 1, 0.95, 0.98},
		{"10 days old", 10, 0.72, 0.76},
		{"23 days old (near stale)", 23, 0.49, 0.52},
		{"30 days old", 30, 0.39, 0.42},
		{"60 days old", 60, 0.15, 0.18},
		{"100 days old", 100, 0.04, 0.06},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			created := time.Now().Add(-time.Duration(tt.daysAgo) * 24 * time.Hour)
			score := CalculateFreshness(created, cfg)
			if score < tt.wantMin || score > tt.wantMax {
				t.Errorf("freshness at %d days = %f, want [%f, %f]", tt.daysAgo, score, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestIsStale(t *testing.T) {
	cfg := DefaultFreshnessConfig()

	fresh := time.Now().Add(-5 * 24 * time.Hour) // 5 days ago
	if IsStale(fresh, cfg) {
		t.Error("5-day-old fact should not be stale")
	}

	stale := time.Now().Add(-50 * 24 * time.Hour) // 50 days ago
	if !IsStale(stale, cfg) {
		t.Error("50-day-old fact should be stale")
	}
}

func TestDaysSince(t *testing.T) {
	d := DaysSince(time.Now().Add(-72 * time.Hour))
	if d != 3 {
		t.Errorf("DaysSince 72h ago = %d, want 3", d)
	}
}

func TestAnnotateStale(t *testing.T) {
	cfg := DefaultFreshnessConfig()

	fresh := agent.ThoughtPackage{
		Content:   "ZBOT uses pgvector",
		Freshness: time.Now().Add(-2 * 24 * time.Hour),
	}
	result := AnnotateStale(fresh, cfg)
	if result != "ZBOT uses pgvector" {
		t.Errorf("fresh package should not be annotated, got: %s", result)
	}

	stale := agent.ThoughtPackage{
		Content:   "Old fact about something",
		Freshness: time.Now().Add(-50 * 24 * time.Hour),
	}
	result = AnnotateStale(stale, cfg)
	if len(result) <= len(stale.Content) {
		t.Error("stale package should have annotation prefix")
	}
}
