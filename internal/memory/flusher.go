// Package memory — Sprint D: MemoryFlusher implementation.
//
// FlushContext extracts critical facts from a conversation before the
// context window is compacted, preventing information loss.
// WriteDailyNote appends timestamped entries to a running daily notes table.
package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/zbot-ai/zbot/internal/agent"
)

// Flusher implements agent.MemoryFlusher using the memory store + LLM extraction.
// When a context window fills up, FlushContext uses a cheap model (Haiku) to
// identify the most important facts before they're lost to compaction.
type Flusher struct {
	store    *Store
	extractor agent.LLMClient // cheap model (Haiku) for fact extraction
	db       *pgxpool.Pool
	logger   *slog.Logger
}

// NewFlusher creates a MemoryFlusher backed by the pgvector store.
func NewFlusher(store *Store, extractor agent.LLMClient, db *pgxpool.Pool, logger *slog.Logger) (*Flusher, error) {
	f := &Flusher{
		store:     store,
		extractor: extractor,
		db:        db,
		logger:    logger,
	}
	if err := f.migrateDailyNotes(context.Background()); err != nil {
		return nil, fmt.Errorf("flusher migration: %w", err)
	}
	return f, nil
}

// migrateDailyNotes creates the daily_notes table if it doesn't exist.
func (f *Flusher) migrateDailyNotes(ctx context.Context) error {
	_, err := f.db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS daily_notes (
			id         SERIAL PRIMARY KEY,
			date       DATE NOT NULL DEFAULT CURRENT_DATE,
			entry      TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS idx_daily_notes_date ON daily_notes(date);
	`)
	return err
}

// FlushContext extracts critical facts from a conversation before compaction.
// It uses the cheap LLM (Haiku) to identify saveable facts, then persists them
// to the memory store so they survive context window resets.
func (f *Flusher) FlushContext(ctx context.Context, conversation []agent.Message) error {
	if len(conversation) == 0 {
		return nil
	}

	// Build a condensed transcript for the extractor.
	var transcript strings.Builder
	for _, msg := range conversation {
		if msg.Role == agent.RoleSystem {
			continue // skip system prompts
		}
		transcript.WriteString(fmt.Sprintf("[%s]: %s\n", msg.Role, truncate(msg.Content, 500)))
	}

	// Ask the cheap model to extract saveable facts.
	extractPrompt := `You are a memory extraction assistant. Review this conversation transcript and extract the most important facts worth remembering long-term.

Focus on:
- Personal preferences and details about the user
- Important decisions made
- Key findings or research results
- Project milestones or status updates
- Any explicit "remember this" requests

Return JSON: {"facts": [{"content": "fact text", "tags": ["tag1", "tag2"]}]}
Return an empty array if nothing is worth saving.

Transcript:
` + transcript.String()

	result, err := f.extractor.Complete(ctx, []agent.Message{
		{Role: agent.RoleUser, Content: extractPrompt},
	}, nil)
	if err != nil {
		return fmt.Errorf("flush extract: %w", err)
	}

	// Parse extracted facts.
	var extracted struct {
		Facts []struct {
			Content string   `json:"content"`
			Tags    []string `json:"tags"`
		} `json:"facts"`
	}

	// Find JSON in response (model may wrap it in markdown).
	raw := result.Content
	jsonStart := strings.Index(raw, "{")
	jsonEnd := strings.LastIndex(raw, "}")
	if jsonStart >= 0 && jsonEnd > jsonStart {
		raw = raw[jsonStart : jsonEnd+1]
	}

	if err := json.Unmarshal([]byte(raw), &extracted); err != nil {
		f.logger.Warn("flush: couldn't parse extractor output", "err", err)
		return nil // non-fatal — we tried
	}

	saved := 0
	for _, ef := range extracted.Facts {
		if ef.Content == "" {
			continue
		}
		fact := agent.Fact{
			ID:        fmt.Sprintf("flush-%d-%d", time.Now().UnixMilli(), saved),
			Content:   ef.Content,
			Source:    "context_flush",
			Tags:      append(ef.Tags, "flushed"),
			CreatedAt: time.Now(),
		}
		if saveErr := f.store.Save(ctx, fact); saveErr != nil {
			f.logger.Warn("flush: save failed", "fact", ef.Content, "err", saveErr)
			continue
		}
		saved++
	}

	f.logger.Info("context flushed",
		"messages", len(conversation),
		"facts_extracted", len(extracted.Facts),
		"facts_saved", saved,
	)
	return nil
}

// WriteDailyNote appends an entry to today's daily notes.
func (f *Flusher) WriteDailyNote(ctx context.Context, entry string) error {
	if strings.TrimSpace(entry) == "" {
		return nil
	}

	_, err := f.db.Exec(ctx,
		`INSERT INTO daily_notes (date, entry) VALUES (CURRENT_DATE, $1)`,
		entry,
	)
	if err != nil {
		return fmt.Errorf("write daily note: %w", err)
	}

	f.logger.Info("daily note saved", "entry_len", len(entry))
	return nil
}

// ReadDailyNotes returns all entries for a given date.
func (f *Flusher) ReadDailyNotes(ctx context.Context, date time.Time) ([]string, error) {
	rows, err := f.db.Query(ctx,
		`SELECT entry FROM daily_notes WHERE date = $1 ORDER BY created_at ASC`,
		date.Format("2006-01-02"),
	)
	if err != nil {
		return nil, fmt.Errorf("read daily notes: %w", err)
	}
	defer rows.Close()

	var entries []string
	for rows.Next() {
		var entry string
		if err := rows.Scan(&entry); err != nil {
			return nil, fmt.Errorf("scan daily note: %w", err)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// truncate shortens a string for display.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// Ensure Flusher implements the port.
var _ agent.MemoryFlusher = (*Flusher)(nil)
