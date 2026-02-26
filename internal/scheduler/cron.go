// Package scheduler implements a pure-Go cron scheduler for ZBOT.
// No external cron library — 5-field standard cron parsing from scratch.
package scheduler

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// CronExpr represents a parsed 5-field cron expression.
// Fields: minute (0-59), hour (0-23), dom (1-31), month (1-12), dow (0-6, 0=Sun).
type CronExpr struct {
	Minutes  []int
	Hours    []int
	Days     []int
	Months   []int
	Weekdays []int
}

// ParseCron parses a standard 5-field cron expression.
// Supports: *, */N, N, N-M, N,M,O
func ParseCron(expr string) (*CronExpr, error) {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return nil, fmt.Errorf("cron: expected 5 fields, got %d in %q", len(fields), expr)
	}

	minutes, err := parseField(fields[0], 0, 59)
	if err != nil {
		return nil, fmt.Errorf("cron minute: %w", err)
	}
	hours, err := parseField(fields[1], 0, 23)
	if err != nil {
		return nil, fmt.Errorf("cron hour: %w", err)
	}
	days, err := parseField(fields[2], 1, 31)
	if err != nil {
		return nil, fmt.Errorf("cron day: %w", err)
	}
	months, err := parseField(fields[3], 1, 12)
	if err != nil {
		return nil, fmt.Errorf("cron month: %w", err)
	}
	weekdays, err := parseField(fields[4], 0, 6)
	if err != nil {
		return nil, fmt.Errorf("cron weekday: %w", err)
	}

	return &CronExpr{
		Minutes:  minutes,
		Hours:    hours,
		Days:     days,
		Months:   months,
		Weekdays: weekdays,
	}, nil
}

// parseField parses a single cron field into a slice of allowed values.
func parseField(field string, min, max int) ([]int, error) {
	var result []int

	for _, part := range strings.Split(field, ",") {
		part = strings.TrimSpace(part)

		// Handle */N (step)
		if strings.HasPrefix(part, "*/") {
			step, err := strconv.Atoi(part[2:])
			if err != nil || step <= 0 {
				return nil, fmt.Errorf("invalid step %q", part)
			}
			for i := min; i <= max; i += step {
				result = append(result, i)
			}
			continue
		}

		// Handle * (all values)
		if part == "*" {
			for i := min; i <= max; i++ {
				result = append(result, i)
			}
			continue
		}

		// Handle N-M (range)
		if strings.Contains(part, "-") {
			rangeParts := strings.SplitN(part, "-", 2)
			lo, err := strconv.Atoi(rangeParts[0])
			if err != nil {
				return nil, fmt.Errorf("invalid range start %q", part)
			}
			hi, err := strconv.Atoi(rangeParts[1])
			if err != nil {
				return nil, fmt.Errorf("invalid range end %q", part)
			}
			if lo < min || hi > max || lo > hi {
				return nil, fmt.Errorf("range out of bounds: %d-%d (allowed %d-%d)", lo, hi, min, max)
			}
			for i := lo; i <= hi; i++ {
				result = append(result, i)
			}
			continue
		}

		// Handle single value
		val, err := strconv.Atoi(part)
		if err != nil {
			return nil, fmt.Errorf("invalid value %q", part)
		}
		if val < min || val > max {
			return nil, fmt.Errorf("value %d out of range %d-%d", val, min, max)
		}
		result = append(result, val)
	}

	if len(result) == 0 {
		return nil, fmt.Errorf("empty field")
	}
	return result, nil
}

// contains checks if val is in the slice.
func contains(vals []int, val int) bool {
	for _, v := range vals {
		if v == val {
			return true
		}
	}
	return false
}

// NextCronTime calculates the next time a cron expression should fire after 'after'.
// Uses local time for DST awareness.
func NextCronTime(expr *CronExpr, after time.Time) time.Time {
	t := after.In(time.Local).Truncate(time.Minute).Add(time.Minute)

	// Search forward up to 2 years (prevents infinite loops on impossible expressions).
	limit := t.Add(2 * 365 * 24 * time.Hour)

	for t.Before(limit) {
		if !contains(expr.Months, int(t.Month())) {
			// Skip to next month.
			t = time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, t.Location())
			continue
		}
		if !contains(expr.Days, t.Day()) {
			t = t.AddDate(0, 0, 1)
			t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
			continue
		}
		if !contains(expr.Weekdays, int(t.Weekday())) {
			t = t.AddDate(0, 0, 1)
			t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
			continue
		}
		if !contains(expr.Hours, t.Hour()) {
			t = t.Add(time.Hour)
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, t.Location())
			continue
		}
		if !contains(expr.Minutes, t.Minute()) {
			t = t.Add(time.Minute)
			continue
		}

		// All fields match!
		return t
	}

	// Fallback: if no match found in 2 years, return far future.
	return limit
}

// NextCronTimeFromExpr is a convenience that parses and computes in one call.
func NextCronTimeFromExpr(cronExpr string, after time.Time) (time.Time, error) {
	parsed, err := ParseCron(cronExpr)
	if err != nil {
		return time.Time{}, err
	}
	return NextCronTime(parsed, after), nil
}

// ValidateCron checks if a cron expression is valid.
func ValidateCron(expr string) error {
	_, err := ParseCron(expr)
	return err
}
