// Package memory — Sprint D: Memory curator.
//
// Periodic job that reviews daily notes and promotes important facts
// to the permanent pgvector memory store. Runs on a configurable
// schedule (default: weekly).
//
// Promotion criteria (evaluated by a cheap LLM):
// - User preferences and personal details
// - Project milestones and decisions
// - Important research findings
// - Recurring themes across multiple days
package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/zbot-ai/zbot/internal/agent"
)

// Curator reviews daily notes and promotes important facts to permanent memory.
type Curator struct {
	store       *Store
	notesWriter *DailyNotesWriter
	extractor   agent.LLMClient // cheap model for fact evaluation
	logger      *slog.Logger
}

// NewCurator creates a memory curator.
func NewCurator(store *Store, notes *DailyNotesWriter, extractor agent.LLMClient, logger *slog.Logger) *Curator {
	return &Curator{
		store:       store,
		notesWriter: notes,
		extractor:   extractor,
		logger:      logger,
	}
}

// CurateRecent reviews the last N days of daily notes and promotes
// important facts to permanent memory.
func (c *Curator) CurateRecent(ctx context.Context, days int) (int, error) {
	if days <= 0 {
		days = 7
	}

	// Collect recent daily notes.
	var allNotes strings.Builder
	notesFound := 0
	for i := 0; i < days; i++ {
		date := time.Now().AddDate(0, 0, -i)
		content, err := c.notesWriter.Read(date)
		if err != nil || content == "" {
			continue
		}
		allNotes.WriteString(content)
		allNotes.WriteString("\n---\n")
		notesFound++
	}

	if notesFound == 0 {
		c.logger.Info("curator: no daily notes found", "days_checked", days)
		return 0, nil
	}

	// Ask the cheap model to identify promotable facts.
	prompt := `You are a memory curator for a personal AI agent. Review these daily notes and identify facts worth promoting to permanent long-term memory.

Promote facts that are:
- Personal preferences or details about the user
- Project milestones, decisions, or deadlines
- Important research findings or conclusions
- Recurring patterns or themes
- Contacts, relationships, or organizations mentioned multiple times

Do NOT promote:
- Transient status updates ("started working on X")
- Routine task completions
- Temporary information (weather, current prices)

Return JSON: {"promotions": [{"content": "fact text", "tags": ["tag1"], "reason": "why this is worth remembering"}]}
Return empty array if nothing is worth promoting.

Daily Notes:
` + allNotes.String()

	result, err := c.extractor.Complete(ctx, []agent.Message{
		{Role: agent.RoleUser, Content: prompt},
	}, nil)
	if err != nil {
		return 0, fmt.Errorf("curator extract: %w", err)
	}

	// Parse promotions.
	var extracted struct {
		Promotions []struct {
			Content string   `json:"content"`
			Tags    []string `json:"tags"`
			Reason  string   `json:"reason"`
		} `json:"promotions"`
	}

	raw := result.Content
	jsonStart := strings.Index(raw, "{")
	jsonEnd := strings.LastIndex(raw, "}")
	if jsonStart >= 0 && jsonEnd > jsonStart {
		raw = raw[jsonStart : jsonEnd+1]
	}

	if err := json.Unmarshal([]byte(raw), &extracted); err != nil {
		c.logger.Warn("curator: couldn't parse extractor output", "err", err)
		return 0, nil
	}

	// Save promoted facts.
	saved := 0
	for _, p := range extracted.Promotions {
		if p.Content == "" {
			continue
		}

		// Check if this fact already exists (avoid duplicates).
		existing, _ := c.store.Search(ctx, p.Content, 1)
		if len(existing) > 0 && existing[0].Score > 0.9 {
			c.logger.Debug("curator: skipping duplicate", "fact", p.Content[:min(50, len(p.Content))])
			continue
		}

		fact := agent.Fact{
			ID:        fmt.Sprintf("curated-%d-%d", time.Now().UnixMilli(), saved),
			Content:   p.Content,
			Source:    "curator",
			Tags:      append(p.Tags, "promoted"),
			CreatedAt: time.Now(),
		}
		if saveErr := c.store.Save(ctx, fact); saveErr != nil {
			c.logger.Warn("curator: save failed", "fact", p.Content[:min(50, len(p.Content))], "err", saveErr)
			continue
		}
		saved++
		c.logger.Info("curator: promoted fact", "fact", p.Content[:min(80, len(p.Content))], "reason", p.Reason)
	}

	c.logger.Info("curator complete",
		"days_reviewed", notesFound,
		"candidates", len(extracted.Promotions),
		"promoted", saved,
	)
	return saved, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
