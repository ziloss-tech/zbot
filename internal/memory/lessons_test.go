package memory

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ziloss-tech/zbot/internal/agent"
)

// inMemoryLessonStore implements agent.LessonStore for tests.
type inMemoryLessonStore struct {
	mu      sync.RWMutex
	lessons map[string]agent.Lesson
}

func newInMemoryLessonStore() *inMemoryLessonStore {
	return &inMemoryLessonStore{lessons: make(map[string]agent.Lesson)}
}

func (s *inMemoryLessonStore) SaveLesson(_ context.Context, l agent.Lesson) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if l.CreatedAt.IsZero() {
		l.CreatedAt = now
	}
	l.UpdatedAt = now
	s.lessons[l.ID] = l
	return nil
}

func (s *inMemoryLessonStore) SearchLessons(_ context.Context, query string, limit int) ([]agent.Lesson, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var results []agent.Lesson
	queryLower := toLower(query)
	for _, l := range s.lessons {
		if containsStr(toLower(l.Mistake), queryLower) ||
			containsStr(toLower(l.Correction), queryLower) ||
			containsStr(toLower(l.Context), queryLower) {
			results = append(results, l)
		}
		if len(results) >= limit {
			break
		}
	}
	return results, nil
}

func (s *inMemoryLessonStore) IncrementTrigger(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if l, ok := s.lessons[id]; ok {
		l.TriggerCount++
		l.UpdatedAt = time.Now()
		s.lessons[id] = l
	}
	return nil
}

func toLower(s string) string {
	b := make([]byte, len(s))
	for i := range s {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		} else {
			b[i] = c
		}
	}
	return string(b)
}

func containsStr(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

var _ agent.LessonStore = (*inMemoryLessonStore)(nil)


func TestLessonStore_SaveAndSearch(t *testing.T) {
	ctx := context.Background()
	store := newInMemoryLessonStore()

	lesson := agent.Lesson{
		ID:         "lesson-1",
		Mistake:    "Cited specific financial figures not in evidence",
		Correction: "Only include figures directly from search results",
		Context:    "Research query about xAI financials",
		SessionID:  "sess-123",
	}
	if err := store.SaveLesson(ctx, lesson); err != nil {
		t.Fatalf("SaveLesson: %v", err)
	}

	// Search by related query
	results, err := store.SearchLessons(ctx, "financial figures", 5)
	if err != nil {
		t.Fatalf("SearchLessons: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ID != "lesson-1" {
		t.Errorf("ID = %q, want lesson-1", results[0].ID)
	}
}

func TestLessonStore_IncrementTrigger(t *testing.T) {
	ctx := context.Background()
	store := newInMemoryLessonStore()

	store.SaveLesson(ctx, agent.Lesson{
		ID: "lesson-1", Mistake: "bad", Correction: "good", Context: "test",
	})

	// Increment twice
	store.IncrementTrigger(ctx, "lesson-1")
	store.IncrementTrigger(ctx, "lesson-1")

	results, _ := store.SearchLessons(ctx, "bad", 5)
	if len(results) == 0 {
		t.Fatal("expected results")
	}
	if results[0].TriggerCount != 2 {
		t.Errorf("TriggerCount = %d, want 2", results[0].TriggerCount)
	}
}

func TestLessonStore_NoMatch(t *testing.T) {
	ctx := context.Background()
	store := newInMemoryLessonStore()

	store.SaveLesson(ctx, agent.Lesson{
		ID: "lesson-1", Mistake: "hallucinated stock price",
		Correction: "verify before citing", Context: "stock research",
	})

	results, _ := store.SearchLessons(ctx, "cooking recipe pasta", 5)
	if len(results) != 0 {
		t.Errorf("expected 0 results for unrelated query, got %d", len(results))
	}
}

func TestLessonStore_MultipleMatch(t *testing.T) {
	ctx := context.Background()
	store := newInMemoryLessonStore()

	store.SaveLesson(ctx, agent.Lesson{
		ID: "lesson-1", Mistake: "wrong API endpoint",
		Correction: "use v2 endpoint", Context: "GHL API",
	})
	store.SaveLesson(ctx, agent.Lesson{
		ID: "lesson-2", Mistake: "forgot API auth header",
		Correction: "always include token-id", Context: "GHL workflow API",
	})
	store.SaveLesson(ctx, agent.Lesson{
		ID: "lesson-3", Mistake: "unrelated cooking error",
		Correction: "add salt", Context: "recipe",
	})

	results, _ := store.SearchLessons(ctx, "API", 5)
	if len(results) != 2 {
		t.Errorf("expected 2 API lessons, got %d", len(results))
	}
}
