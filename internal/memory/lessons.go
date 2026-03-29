// Package memory — Experiential Learning (Lessons from mistakes).
// Phase 5 of the Memory Overhaul.
//
// When Thalamus rejects a response and Cortex successfully revises it,
// the mistake→correction pattern is saved as a Lesson. Next time a similar
// query comes in, ZBOT has the lesson in context and avoids the same error.
//
// Lessons are stored in Postgres alongside ThoughtPackages.
// The nightly batch builder (Phase 3) can optionally compress lessons
// into a "lessons/recent" ThoughtPackage.
package memory

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ziloss-tech/zbot/internal/agent"
)

// LessonStore persists and retrieves lessons from Postgres.
type LessonStore struct {
	db        *pgxpool.Pool
	embedder  Embedder
	logger    *slog.Logger
	namespace string
}

func (ls *LessonStore) tableName() string {
	return ls.namespace + "_lessons"
}

// NewLessonStore creates a lesson store and runs migrations.
func NewLessonStore(ctx context.Context, db *pgxpool.Pool, embedder Embedder, logger *slog.Logger, namespace string) (*LessonStore, error) {
	if namespace == "" {
		namespace = "zbot"
	}
	ls := &LessonStore{db: db, embedder: embedder, logger: logger, namespace: namespace}
	if err := ls.migrate(ctx); err != nil {
		return nil, fmt.Errorf("LessonStore migrate: %w", err)
	}
	return ls, nil
}

func (ls *LessonStore) migrate(ctx context.Context) error {
	tbl := ls.tableName()
	sql := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id            TEXT PRIMARY KEY,
			mistake       TEXT NOT NULL,
			correction    TEXT NOT NULL,
			context       TEXT NOT NULL DEFAULT '',
			session_id    TEXT NOT NULL DEFAULT '',
			embedding     vector(768),
			trigger_count INT NOT NULL DEFAULT 0,
			created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE INDEX IF NOT EXISTS %s_embedding_idx
			ON %s USING hnsw (embedding vector_cosine_ops)
			WITH (m = 16, ef_construction = 64);

		CREATE INDEX IF NOT EXISTS %s_trigger_idx
			ON %s (trigger_count DESC, updated_at DESC);
	`, tbl, tbl, tbl, tbl, tbl)

	_, err := ls.db.Exec(ctx, sql)
	if err != nil {
		return fmt.Errorf("lessons migrate: %w", err)
	}
	ls.logger.Info("lessons schema ready", "table", tbl)
	return nil
}

// SaveLesson persists a lesson. Generates embedding from mistake+correction.
func (ls *LessonStore) SaveLesson(ctx context.Context, lesson agent.Lesson) error {
	embedText := lesson.Mistake + " " + lesson.Correction + " " + lesson.Context
	embedding, err := ls.embedder.Embed(ctx, embedText)
	if err != nil {
		return fmt.Errorf("LessonStore.Save embed: %w", err)
	}

	now := time.Now()
	if lesson.CreatedAt.IsZero() {
		lesson.CreatedAt = now
	}
	lesson.UpdatedAt = now

	sql := fmt.Sprintf(`
		INSERT INTO %s (id, mistake, correction, context, session_id, embedding, trigger_count, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (id) DO UPDATE SET
			mistake = EXCLUDED.mistake,
			correction = EXCLUDED.correction,
			embedding = EXCLUDED.embedding,
			trigger_count = EXCLUDED.trigger_count,
			updated_at = EXCLUDED.updated_at
	`, ls.tableName())

	_, err = ls.db.Exec(ctx, sql,
		lesson.ID, lesson.Mistake, lesson.Correction, lesson.Context,
		lesson.SessionID, fmtVec(embedding), lesson.TriggerCount,
		lesson.CreatedAt, lesson.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("LessonStore.Save: %w", err)
	}
	ls.logger.Debug("lesson saved", "id", lesson.ID)
	return nil
}

// SearchLessons finds lessons relevant to a query using vector similarity.
// Returns the top `limit` lessons sorted by similarity.
func (ls *LessonStore) SearchLessons(ctx context.Context, query string, limit int) ([]agent.Lesson, error) {
	embedding, err := ls.embedder.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("LessonStore.Search embed: %w", err)
	}

	sql := fmt.Sprintf(`
		SELECT id, mistake, correction, context, session_id, trigger_count, created_at, updated_at,
			1 - (embedding <=> $1::vector) AS sim
		FROM %s
		WHERE embedding IS NOT NULL
		ORDER BY embedding <=> $1::vector
		LIMIT $2
	`, ls.tableName())

	rows, err := ls.db.Query(ctx, sql, fmtVec(embedding), limit)
	if err != nil {
		return nil, fmt.Errorf("LessonStore.Search: %w", err)
	}
	defer rows.Close()

	var lessons []agent.Lesson
	for rows.Next() {
		var l agent.Lesson
		var sim float32
		if err := rows.Scan(&l.ID, &l.Mistake, &l.Correction, &l.Context,
			&l.SessionID, &l.TriggerCount, &l.CreatedAt, &l.UpdatedAt, &sim); err != nil {
			return nil, fmt.Errorf("LessonStore.Search scan: %w", err)
		}
		if sim > 0.3 { // minimum similarity threshold
			lessons = append(lessons, l)
		}
	}
	return lessons, rows.Err()
}

// IncrementTrigger bumps a lesson's trigger count (called when a lesson matches at runtime).
func (ls *LessonStore) IncrementTrigger(ctx context.Context, id string) error {
	sql := fmt.Sprintf(`
		UPDATE %s SET trigger_count = trigger_count + 1, updated_at = NOW()
		WHERE id = $1
	`, ls.tableName())
	_, err := ls.db.Exec(ctx, sql, id)
	return err
}

// ListLessons returns all lessons ordered by trigger count (most useful first).
func (ls *LessonStore) ListLessons(ctx context.Context, limit int) ([]agent.Lesson, error) {
	sql := fmt.Sprintf(`
		SELECT id, mistake, correction, context, session_id, trigger_count, created_at, updated_at
		FROM %s
		ORDER BY trigger_count DESC, updated_at DESC
		LIMIT $1
	`, ls.tableName())

	rows, err := ls.db.Query(ctx, sql, limit)
	if err != nil {
		return nil, fmt.Errorf("LessonStore.List: %w", err)
	}
	defer rows.Close()

	var lessons []agent.Lesson
	for rows.Next() {
		var l agent.Lesson
		if err := rows.Scan(&l.ID, &l.Mistake, &l.Correction, &l.Context,
			&l.SessionID, &l.TriggerCount, &l.CreatedAt, &l.UpdatedAt); err != nil {
			return nil, fmt.Errorf("LessonStore.List scan: %w", err)
		}
		lessons = append(lessons, l)
	}
	return lessons, rows.Err()
}

// Count returns total lesson count.
func (ls *LessonStore) Count(ctx context.Context) (int64, error) {
	var count int64
	sql := fmt.Sprintf(`SELECT COUNT(*) FROM %s`, ls.tableName())
	err := ls.db.QueryRow(ctx, sql).Scan(&count)
	return count, err
}


// Ensure LessonStore implements the interface.
var _ agent.LessonStore = (*LessonStore)(nil)
