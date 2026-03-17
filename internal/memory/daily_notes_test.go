package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDailyNotesWriter_Write(t *testing.T) {
	dir := t.TempDir()
	w, err := NewDailyNotesWriter(dir)
	if err != nil {
		t.Fatalf("NewDailyNotesWriter: %v", err)
	}

	// Write two entries.
	if err := w.Write("First entry of the day"); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Write("Second entry"); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Read back today's notes.
	content, err := w.Read(time.Now())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if !strings.Contains(content, "Daily Notes") {
		t.Error("expected header in daily notes")
	}
	if !strings.Contains(content, "First entry of the day") {
		t.Error("expected first entry")
	}
	if !strings.Contains(content, "Second entry") {
		t.Error("expected second entry")
	}
}

func TestDailyNotesWriter_ReadMissing(t *testing.T) {
	dir := t.TempDir()
	w, _ := NewDailyNotesWriter(dir)

	content, err := w.Read(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content != "" {
		t.Errorf("expected empty content for missing date, got: %q", content)
	}
}

func TestDailyNotesWriter_WriteEmpty(t *testing.T) {
	dir := t.TempDir()
	w, _ := NewDailyNotesWriter(dir)

	if err := w.Write(""); err != nil {
		t.Fatalf("Write empty: %v", err)
	}
	if err := w.Write("   "); err != nil {
		t.Fatalf("Write whitespace: %v", err)
	}

	// No file should have been created.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("expected no files, got %d", len(entries))
	}
}

func TestDailyNotesWriter_ListDays(t *testing.T) {
	dir := t.TempDir()

	// Create a few fake daily note files.
	for _, name := range []string{"2026-03-14.md", "2026-03-15.md", "2026-03-16.md", "not-a-date.txt"} {
		os.WriteFile(filepath.Join(dir, name), []byte("test"), 0o640)
	}

	w, _ := NewDailyNotesWriter(dir)
	dates, err := w.ListDays(10)
	if err != nil {
		t.Fatalf("ListDays: %v", err)
	}
	if len(dates) != 3 {
		t.Fatalf("expected 3 dates, got %d", len(dates))
	}
	// Should be newest first.
	if dates[0].Day() != 16 {
		t.Errorf("expected newest first (16), got %d", dates[0].Day())
	}
}

func TestDailyNotesWriter_ListDaysLimit(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"2026-03-14.md", "2026-03-15.md", "2026-03-16.md"} {
		os.WriteFile(filepath.Join(dir, name), []byte("test"), 0o640)
	}

	w, _ := NewDailyNotesWriter(dir)
	dates, err := w.ListDays(2)
	if err != nil {
		t.Fatalf("ListDays: %v", err)
	}
	if len(dates) != 2 {
		t.Errorf("expected 2 dates with limit, got %d", len(dates))
	}
}
