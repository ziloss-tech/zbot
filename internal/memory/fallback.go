// Package memory — in-memory fallback store.
// Used when PostgreSQL is unavailable so ZBOT still works (just without persistence).
package memory

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/zbot-ai/zbot/internal/agent"
)

// InMemoryStore implements agent.MemoryStore using a sync.Map.
// No persistence — resets on restart. Used as fallback when DB is down.
type InMemoryStore struct {
	mu     sync.RWMutex
	facts  map[string]agent.Fact
	logger *slog.Logger
}

// NewInMemoryStore creates an in-memory fallback store.
func NewInMemoryStore(logger *slog.Logger) *InMemoryStore {
	logger.Warn("using in-memory fallback for memory store — data will not persist across restarts")
	return &InMemoryStore{
		facts:  make(map[string]agent.Fact),
		logger: logger,
	}
}

// Save stores a fact in memory.
func (s *InMemoryStore) Save(_ context.Context, fact agent.Fact) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.facts[fact.ID] = fact
	s.logger.Debug("memory saved (in-memory)", "id", fact.ID)
	return nil
}

// Search performs simple keyword matching over stored facts.
// Returns facts sorted by relevance (keyword match count) with time decay.
func (s *InMemoryStore) Search(_ context.Context, query string, limit int) ([]agent.Fact, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	queryLower := strings.ToLower(query)
	words := strings.Fields(queryLower)
	if len(words) == 0 {
		return nil, nil
	}

	type scored struct {
		fact  agent.Fact
		score float32
	}

	var results []scored
	now := time.Now()

	for _, fact := range s.facts {
		contentLower := strings.ToLower(fact.Content)
		var matchCount float32
		for _, w := range words {
			if strings.Contains(contentLower, w) {
				matchCount++
			}
		}
		if matchCount == 0 {
			continue
		}

		// Normalize by word count and apply time decay.
		score := matchCount / float32(len(words))
		daysSince := now.Sub(fact.CreatedAt).Hours() / 24
		decayFactor := float32(1.0)
		if daysSince > 0 {
			// Approximate exp(-0.01 * days)
			decayFactor = float32(1.0 / (1.0 + 0.01*daysSince))
		}
		score *= decayFactor

		results = append(results, scored{fact: fact, score: score})
	}

	// Sort by score descending.
	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})

	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}

	facts := make([]agent.Fact, len(results))
	for i, r := range results {
		r.fact.Score = r.score
		facts[i] = r.fact
	}
	return facts, nil
}

// Delete removes a fact by ID.
func (s *InMemoryStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.facts, id)
	return nil
}

// Ensure InMemoryStore implements the port.
var _ agent.MemoryStore = (*InMemoryStore)(nil)
