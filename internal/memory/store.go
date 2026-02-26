// Package memory implements the hybrid pgvector + BM25 memory store.
// Reuses the existing Vertex AI / GCP Cloud SQL pgvector infrastructure at Ziloss.
// Same database as mem0, separate table namespace: e.g. "zbot" → "zbot_memories".
package memory

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jeremylerwick-max/zbot/internal/agent"
)

// fmtVec formats a []float32 as a pgvector literal: "[0.1,0.2,...]"
func fmtVec(v []float32) string {
	parts := make([]string, len(v))
	for i, f := range v {
		parts[i] = fmt.Sprintf("%g", f)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// Store implements agent.MemoryStore using pgvector + PostgreSQL FTS.
// Architecture:
//   - Vector search: pgvector cosine similarity (semantic matching)
//   - BM25 search:   PostgreSQL tsvector FTS (lexical matching)
//   - Fusion:        70% vector + 30% BM25 scores with time decay
type Store struct {
	db        *pgxpool.Pool
	embedder  Embedder
	logger    *slog.Logger
	namespace string // table prefix, e.g. "zbot" → table "zbot_memories"
}

// Embedder is the interface for generating vector embeddings.
// Adapter: Vertex AI text-embedding-004 (768 dims) — matches existing Ziloss infra.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	Dims() int
}

// tableName returns the fully-qualified memories table name for this namespace.
func (s *Store) tableName() string {
	return s.namespace + "_memories"
}

// New creates a Store. Runs migrations on startup to ensure schema exists.
// namespace controls the table name: e.g. "zbot" → "zbot_memories".
func New(ctx context.Context, db *pgxpool.Pool, embedder Embedder, logger *slog.Logger, namespace string) (*Store, error) {
	if namespace == "" {
		namespace = "zbot"
	}
	s := &Store{
		db:        db,
		embedder:  embedder,
		logger:    logger,
		namespace: namespace,
	}
	if err := s.migrate(ctx); err != nil {
		return nil, fmt.Errorf("memory.New migrate: %w", err)
	}
	return s, nil
}

// Save persists a fact to long-term memory.
// Generates an embedding and inserts into pgvector + FTS tables.
func (s *Store) Save(ctx context.Context, fact agent.Fact) error {
	embedding, err := s.embedder.Embed(ctx, fact.Content)
	if err != nil {
		return fmt.Errorf("memory.Save Embed: %w", err)
	}

	query := fmt.Sprintf(`
		INSERT INTO %s (id, content, source, tags, embedding, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (id) DO UPDATE
		  SET content = EXCLUDED.content,
		      embedding = EXCLUDED.embedding,
		      updated_at = NOW()
	`, s.tableName())

	_, err = s.db.Exec(ctx, query,
		fact.ID, fact.Content, fact.Source, fact.Tags, fmtVec(embedding), fact.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("memory.Save insert: %w", err)
	}

	s.logger.Debug("memory saved", "id", fact.ID, "source", fact.Source)
	return nil
}

// Search retrieves facts using hybrid BM25 + vector scoring with time decay.
// Query flow:
//  1. Generate query embedding
//  2. Run vector similarity search (top 20)
//  3. Run BM25 full-text search (top 20)
//  4. Fuse scores: 0.7*vector + 0.3*bm25
//  5. Apply time decay: score * exp(-0.01 * days_old)
//  6. Return top `limit` results
func (s *Store) Search(ctx context.Context, query string, limit int) ([]agent.Fact, error) {
	embedding, err := s.embedder.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("memory.Search Embed: %w", err)
	}

	tbl := s.tableName()
	sqlQuery := fmt.Sprintf(`
		WITH vector_results AS (
			SELECT id, content, source, tags, created_at,
			       1 - (embedding <=> $1::vector) AS vector_score
			FROM %s
			ORDER BY embedding <=> $1::vector
			LIMIT 20
		),
		bm25_results AS (
			SELECT id, content, source, tags, created_at,
			       ts_rank(to_tsvector('english', content), plainto_tsquery('english', $2)) AS bm25_score
			FROM %s
			WHERE to_tsvector('english', content) @@ plainto_tsquery('english', $2)
			LIMIT 20
		),
		fused AS (
			SELECT
				COALESCE(v.id, b.id) AS id,
				COALESCE(v.content, b.content) AS content,
				COALESCE(v.source, b.source) AS source,
				COALESCE(v.tags, b.tags) AS tags,
				COALESCE(v.created_at, b.created_at) AS created_at,
				(COALESCE(v.vector_score, 0) * 0.7 + COALESCE(b.bm25_score, 0) * 0.3)
				  * EXP(-0.01 * EXTRACT(EPOCH FROM (NOW() - COALESCE(v.created_at, b.created_at))) / 86400)
				AS final_score
			FROM vector_results v
			FULL OUTER JOIN bm25_results b USING (id)
		)
		SELECT id, content, source, tags, created_at, final_score
		FROM fused
		ORDER BY final_score DESC
		LIMIT $3
	`, tbl, tbl)

	rows, err := s.db.Query(ctx, sqlQuery, fmtVec(embedding), query, limit)
	if err != nil {
		return nil, fmt.Errorf("memory.Search query: %w", err)
	}
	defer rows.Close()

	var facts []agent.Fact
	for rows.Next() {
		var f agent.Fact
		var tags []string
		if err := rows.Scan(&f.ID, &f.Content, &f.Source, &tags, &f.CreatedAt, &f.Score); err != nil {
			return nil, fmt.Errorf("memory.Search scan: %w", err)
		}
		f.Tags = tags
		facts = append(facts, f)
	}
	return facts, rows.Err()
}

// Delete removes a memory by ID.
func (s *Store) Delete(ctx context.Context, id string) error {
	query := fmt.Sprintf(`DELETE FROM %s WHERE id = $1`, s.tableName())
	_, err := s.db.Exec(ctx, query, id)
	return err
}

// Count returns the total number of memories in the store.
func (s *Store) Count(ctx context.Context) (int64, error) {
	var count int64
	query := fmt.Sprintf(`SELECT COUNT(*) FROM %s`, s.tableName())
	err := s.db.QueryRow(ctx, query).Scan(&count)
	return count, err
}

// Stats returns aggregate statistics about the memory store.
type MemoryStats struct {
	Total   int64
	Oldest  time.Time
	Newest  time.Time
}

func (s *Store) Stats(ctx context.Context) (*MemoryStats, error) {
	stats := &MemoryStats{}
	query := fmt.Sprintf(`
		SELECT COUNT(*), COALESCE(MIN(created_at), NOW()), COALESCE(MAX(created_at), NOW())
		FROM %s
	`, s.tableName())
	err := s.db.QueryRow(ctx, query).Scan(&stats.Total, &stats.Oldest, &stats.Newest)
	if err != nil {
		return nil, fmt.Errorf("memory.Stats: %w", err)
	}
	return stats, nil
}

// List returns the most recent N memories.
func (s *Store) List(ctx context.Context, limit int) ([]agent.Fact, error) {
	query := fmt.Sprintf(`
		SELECT id, content, source, tags, created_at
		FROM %s
		ORDER BY created_at DESC
		LIMIT $1
	`, s.tableName())

	rows, err := s.db.Query(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("memory.List: %w", err)
	}
	defer rows.Close()

	var facts []agent.Fact
	for rows.Next() {
		var f agent.Fact
		var tags []string
		if err := rows.Scan(&f.ID, &f.Content, &f.Source, &tags, &f.CreatedAt); err != nil {
			return nil, fmt.Errorf("memory.List scan: %w", err)
		}
		f.Tags = tags
		facts = append(facts, f)
	}
	return facts, rows.Err()
}

// migrate ensures the memories table exists with correct schema.
// Idempotent — safe to run on every startup.
func (s *Store) migrate(ctx context.Context) error {
	tbl := s.tableName()
	migration := fmt.Sprintf(`
		CREATE EXTENSION IF NOT EXISTS vector;

		CREATE TABLE IF NOT EXISTS %s (
			id         TEXT PRIMARY KEY,
			content    TEXT NOT NULL,
			source     TEXT NOT NULL DEFAULT 'conversation',
			tags       TEXT[] NOT NULL DEFAULT '{}',
			embedding  vector(768),
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE INDEX IF NOT EXISTS %s_embedding_idx
			ON %s USING hnsw (embedding vector_cosine_ops)
			WITH (m = 16, ef_construction = 64);

		CREATE INDEX IF NOT EXISTS %s_fts_idx
			ON %s USING gin (to_tsvector('english', content));

		CREATE INDEX IF NOT EXISTS %s_created_idx
			ON %s (created_at DESC);
	`, tbl, tbl, tbl, tbl, tbl, tbl, tbl)

	_, err := s.db.Exec(ctx, migration)
	if err != nil {
		return fmt.Errorf("memory migrate: %w", err)
	}
	s.logger.Info("memory schema ready", "table", tbl)
	return nil
}

// ─── AUTO-SAVE HELPER ────────────────────────────────────────────────────────

// AutoSave checks if a turn output contains facts worth saving,
// and saves them. Called after every agent turn.
func (s *Store) AutoSave(ctx context.Context, sessionID, content string) {
	// Simple heuristic: save if the assistant produced substantial output (> 200 chars)
	// and it's not a one-liner reply. Phase 2 will use LLM classification here.
	if len(content) < 200 {
		return
	}

	fact := agent.Fact{
		ID:        fmt.Sprintf("%s-%d", sessionID, time.Now().UnixMilli()),
		Content:   content,
		Source:    "conversation",
		CreatedAt: time.Now(),
	}

	if err := s.Save(ctx, fact); err != nil {
		s.logger.Warn("AutoSave failed", "session", sessionID, "err", err)
	}
}
