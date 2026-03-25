package scheduler

import (
	"testing"
	"time"
)

func TestParseCronAllWildcards(t *testing.T) {
	expr, err := ParseCron("* * * * *")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if expr == nil {
		t.Fatal("expected non-nil CronExpr")
	}
	if len(expr.Minutes) != 60 {
		t.Errorf("expected 60 minutes, got %d", len(expr.Minutes))
	}
	if len(expr.Hours) != 24 {
		t.Errorf("expected 24 hours, got %d", len(expr.Hours))
	}
}

func TestParseCronStep(t *testing.T) {
	expr, err := ParseCron("*/15 * * * *")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	expected := []int{0, 15, 30, 45}
	if len(expr.Minutes) != len(expected) {
		t.Errorf("expected %d minutes, got %d", len(expected), len(expr.Minutes))
	}
	for i, exp := range expected {
		if expr.Minutes[i] != exp {
			t.Errorf("minute[%d]: expected %d, got %d", i, exp, expr.Minutes[i])
		}
	}
}

func TestParseCronRange(t *testing.T) {
	expr, err := ParseCron("0 9-17 * * *")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(expr.Hours) != 9 {
		t.Errorf("expected 9 hours (9-17 inclusive), got %d", len(expr.Hours))
	}
}

func TestParseCronList(t *testing.T) {
	expr, err := ParseCron("0 0 1,15 * *")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(expr.Days) != 2 {
		t.Errorf("expected 2 days, got %d", len(expr.Days))
	}
	if expr.Days[0] != 1 || expr.Days[1] != 15 {
		t.Errorf("expected days [1, 15], got %v", expr.Days)
	}
}

func TestParseCronInvalidFields(t *testing.T) {
	tests := []string{
		"* * * *",         // 4 fields
		"* * * * * *",     // 6 fields
		"60 * * * *",      // minute out of range
		"0 24 * * *",      // hour out of range
		"0 0 32 * *",      // day out of range
		"0 0 0 13 *",      // month out of range
		"0 0 1 1 7",       // weekday out of range
		"*/0 * * * *",     // invalid step
		"65-70 * * * *",   // range entirely out of bounds for minutes
	}

	for _, expr := range tests {
		_, err := ParseCron(expr)
		if err == nil {
			t.Errorf("expected error for %q, got nil", expr)
		}
	}
}

func TestValidateCron(t *testing.T) {
	tests := []struct {
		expr  string
		valid bool
	}{
		{"* * * * *", true},
		{"0 9 * * 1-5", true},
		{"0 0 1 1 0", true},
		{"*/5 * * * *", true},
		{"0 0 0 13 *", false},
		{"not a cron", false},
	}

	for _, tt := range tests {
		err := ValidateCron(tt.expr)
		if tt.valid && err != nil {
			t.Errorf("ValidateCron(%q) expected valid, got error: %v", tt.expr, err)
		}
		if !tt.valid && err == nil {
			t.Errorf("ValidateCron(%q) expected invalid, got nil", tt.expr)
		}
	}
}

func TestNextCronTime(t *testing.T) {
	// Parse "0 9 * * *" (9 AM every day)
	expr, _ := ParseCron("0 9 * * *")

	// Start from 8 AM, should get 9 AM same day
	start := time.Date(2026, 3, 25, 8, 0, 0, 0, time.Local)
	next := NextCronTime(expr, start)

	expected := time.Date(2026, 3, 25, 9, 0, 0, 0, time.Local)
	if !next.Equal(expected) {
		t.Errorf("NextCronTime: expected %v, got %v", expected, next)
	}
}

func TestNextCronTimeNextDay(t *testing.T) {
	// Parse "0 9 * * *" (9 AM every day)
	expr, _ := ParseCron("0 9 * * *")

	// Start from 10 AM, should get 9 AM next day
	start := time.Date(2026, 3, 25, 10, 0, 0, 0, time.Local)
	next := NextCronTime(expr, start)

	expected := time.Date(2026, 3, 26, 9, 0, 0, 0, time.Local)
	if !next.Equal(expected) {
		t.Errorf("NextCronTime: expected %v, got %v", expected, next)
	}
}

func TestNextCronTimeWeekdays(t *testing.T) {
	// Parse "0 9 * * 1-5" (9 AM Mon-Fri)
	expr, _ := ParseCron("0 9 * * 1-5")

	// Friday 9 AM - should get Monday 9 AM next week
	start := time.Date(2026, 3, 27, 9, 0, 0, 0, time.Local) // Friday
	next := NextCronTime(expr, start)

	// Monday of next week
	expected := time.Date(2026, 3, 30, 9, 0, 0, 0, time.Local)
	if !next.Equal(expected) {
		t.Errorf("NextCronTime weekday: expected %v, got %v", expected, next)
	}
}

func TestNextCronTimeFromExpr(t *testing.T) {
	start := time.Date(2026, 3, 25, 8, 0, 0, 0, time.Local)
	next, err := NextCronTimeFromExpr("0 9 * * *", start)

	if err != nil {
		t.Fatalf("NextCronTimeFromExpr: expected no error, got %v", err)
	}

	expected := time.Date(2026, 3, 25, 9, 0, 0, 0, time.Local)
	if !next.Equal(expected) {
		t.Errorf("NextCronTimeFromExpr: expected %v, got %v", expected, next)
	}
}

func TestNextCronTimeFromExprInvalid(t *testing.T) {
	start := time.Date(2026, 3, 25, 8, 0, 0, 0, time.Local)
	_, err := NextCronTimeFromExpr("invalid cron", start)

	if err == nil {
		t.Error("NextCronTimeFromExpr: expected error for invalid cron")
	}
}

func TestContains(t *testing.T) {
	tests := []struct {
		vals     []int
		val      int
		expected bool
	}{
		{[]int{1, 2, 3}, 2, true},
		{[]int{1, 2, 3}, 4, false},
		{[]int{}, 1, false},
		{[]int{5}, 5, true},
	}

	for _, tt := range tests {
		result := contains(tt.vals, tt.val)
		if result != tt.expected {
			t.Errorf("contains(%v, %d) = %v, expected %v", tt.vals, tt.val, result, tt.expected)
		}
	}
}

func TestParseCronMonthBoundary(t *testing.T) {
	// Parse month 1-12
	expr, err := ParseCron("0 0 1 1-3 *")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(expr.Months) != 3 {
		t.Errorf("expected 3 months, got %d", len(expr.Months))
	}
}

func TestParseCronWeekdayBoundary(t *testing.T) {
	// Parse weekday 0-6 (Sunday=0)
	expr, err := ParseCron("0 0 1 1 0-6")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(expr.Weekdays) != 7 {
		t.Errorf("expected 7 weekdays, got %d", len(expr.Weekdays))
	}
}

func TestNextCronTimeMinuteGranularity(t *testing.T) {
	// Every 5 minutes
	expr, _ := ParseCron("*/5 * * * *")

	start := time.Date(2026, 3, 25, 9, 3, 0, 0, time.Local)
	next := NextCronTime(expr, start)

	// Should snap to next 5-minute boundary (9:05)
	expected := time.Date(2026, 3, 25, 9, 5, 0, 0, time.Local)
	if !next.Equal(expected) {
		t.Errorf("NextCronTime minute: expected %v, got %v", expected, next)
	}
}
