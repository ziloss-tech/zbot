// Package memory — Sprint D: Markdown daily notes layer.
//
// Writes timestamped entries to memory/YYYY-MM-DD.md files alongside
// pgvector storage. Human-readable, git-trackable, survives DB migrations.
package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DailyNotesWriter writes entries to daily markdown files.
type DailyNotesWriter struct {
	dir string // base directory for notes (e.g., ~/zbot-workspace/memory)
}

// NewDailyNotesWriter creates a writer that stores daily notes as markdown files.
func NewDailyNotesWriter(dir string) (*DailyNotesWriter, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("daily notes dir: %w", err)
	}
	return &DailyNotesWriter{dir: dir}, nil
}

// Write appends a timestamped entry to today's daily notes file.
// Creates the file with a header if it doesn't exist.
func (w *DailyNotesWriter) Write(entry string) error {
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return nil
	}

	now := time.Now()
	filename := now.Format("2006-01-02") + ".md"
	path := filepath.Join(w.dir, filename)

	// Check if file exists to decide whether to write header.
	_, err := os.Stat(path)
	needsHeader := os.IsNotExist(err)

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o640)
	if err != nil {
		return fmt.Errorf("open daily note: %w", err)
	}
	defer f.Close()

	if needsHeader {
		header := fmt.Sprintf("# Daily Notes — %s\n\n", now.Format("Monday, January 2, 2006"))
		if _, err := f.WriteString(header); err != nil {
			return fmt.Errorf("write header: %w", err)
		}
	}

	// Write the timestamped entry.
	timestamp := now.Format("15:04")
	line := fmt.Sprintf("- **%s** — %s\n", timestamp, entry)
	if _, err := f.WriteString(line); err != nil {
		return fmt.Errorf("write entry: %w", err)
	}

	return nil
}

// Read returns the contents of a specific day's notes.
func (w *DailyNotesWriter) Read(date time.Time) (string, error) {
	filename := date.Format("2006-01-02") + ".md"
	path := filepath.Join(w.dir, filename)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil // no notes for this day
		}
		return "", fmt.Errorf("read daily note: %w", err)
	}
	return string(data), nil
}

// ListDays returns dates that have daily notes, most recent first.
func (w *DailyNotesWriter) ListDays(limit int) ([]time.Time, error) {
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return nil, fmt.Errorf("list daily notes: %w", err)
	}

	var dates []time.Time
	for i := len(entries) - 1; i >= 0; i-- { // reverse order (newest first)
		e := entries[i]
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		dateStr := strings.TrimSuffix(e.Name(), ".md")
		date, parseErr := time.Parse("2006-01-02", dateStr)
		if parseErr != nil {
			continue
		}
		dates = append(dates, date)
		if limit > 0 && len(dates) >= limit {
			break
		}
	}

	return dates, nil
}
