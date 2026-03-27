// Package memory — Thought Package storage and matching.
// Phase 2 of the Memory Overhaul: schema, CRUD, keyword matching.
// Packages are pre-organized blocks of compressed memories that replace
// raw vector search at runtime. Keyword match is < 1ms, zero LLM cost.
package memory

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ziloss-tech/zbot/internal/agent"
)

// PackageStore implements agent.PackageStore using PostgreSQL + pgvector.
type PackageStore struct {
	db        *pgxpool.Pool
	embedder  Embedder
	logger    *slog.Logger
	namespace string
}

func (ps *PackageStore) tableName() string {
	return ps.namespace + "_thought_packages"
}

// NewPackageStore creates a PackageStore and runs migrations.
func NewPackageStore(ctx context.Context, db *pgxpool.Pool, embedder Embedder, logger *slog.Logger, namespace string) (*PackageStore, error) {
	if namespace == "" {
		namespace = "zbot"
	}
	ps := &PackageStore{db: db, embedder: embedder, logger: logger, namespace: namespace}
	if err := ps.migrate(ctx); err != nil {
		return nil, fmt.Errorf("PackageStore migrate: %w", err)
	}
	return ps, nil
}

// migrate creates the thought_packages table. Idempotent.
func (ps *PackageStore) migrate(ctx context.Context) error {
	tbl := ps.tableName()
	sql := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id          TEXT PRIMARY KEY,
			label       TEXT NOT NULL,
			keywords    TEXT[] NOT NULL DEFAULT '{}',
			embedding   vector(768),
			content     TEXT NOT NULL,
			token_count INT NOT NULL DEFAULT 0,
			memory_ids  TEXT[] NOT NULL DEFAULT '{}',
			priority    INT NOT NULL DEFAULT 1,
			freshness   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			version     INT NOT NULL DEFAULT 1,
			created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE INDEX IF NOT EXISTS %s_priority_idx
			ON %s (priority, freshness DESC);

		CREATE INDEX IF NOT EXISTS %s_keywords_idx
			ON %s USING gin (keywords);

		CREATE INDEX IF NOT EXISTS %s_embedding_idx
			ON %s USING hnsw (embedding vector_cosine_ops)
			WITH (m = 16, ef_construction = 64);
	`, tbl, tbl, tbl, tbl, tbl, tbl, tbl)

	_, err := ps.db.Exec(ctx, sql)
	if err != nil {
		return fmt.Errorf("thought_packages migrate: %w", err)
	}
	ps.logger.Info("thought_packages schema ready", "table", tbl)
	return nil
}

// SavePackage upserts a thought package. Generates embedding from label + keywords.
func (ps *PackageStore) SavePackage(ctx context.Context, pkg agent.ThoughtPackage) error {
	// Generate embedding from label + first 200 chars of content for similarity fallback.
	embedText := pkg.Label + " " + strings.Join(pkg.Keywords, " ")
	if len(pkg.Content) > 200 {
		embedText += " " + pkg.Content[:200]
	} else {
		embedText += " " + pkg.Content
	}

	embedding, err := ps.embedder.Embed(ctx, embedText)
	if err != nil {
		return fmt.Errorf("PackageStore.Save embed: %w", err)
	}

	now := time.Now()
	if pkg.CreatedAt.IsZero() {
		pkg.CreatedAt = now
	}
	pkg.UpdatedAt = now
	if pkg.Freshness.IsZero() {
		pkg.Freshness = now
	}

	sql := fmt.Sprintf(`
		INSERT INTO %s (id, label, keywords, embedding, content, token_count,
			memory_ids, priority, freshness, version, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (id) DO UPDATE SET
			label = EXCLUDED.label,
			keywords = EXCLUDED.keywords,
			embedding = EXCLUDED.embedding,
			content = EXCLUDED.content,
			token_count = EXCLUDED.token_count,
			memory_ids = EXCLUDED.memory_ids,
			priority = EXCLUDED.priority,
			freshness = EXCLUDED.freshness,
			version = EXCLUDED.version,
			updated_at = EXCLUDED.updated_at
	`, ps.tableName())

	_, err = ps.db.Exec(ctx, sql,
		pkg.ID, pkg.Label, pkg.Keywords, fmtVec(embedding), pkg.Content,
		pkg.TokenCount, pkg.MemoryIDs, int(pkg.Priority), pkg.Freshness,
		pkg.Version, pkg.CreatedAt, pkg.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("PackageStore.Save insert: %w", err)
	}
	ps.logger.Debug("package saved", "id", pkg.ID, "label", pkg.Label, "priority", pkg.Priority)
	return nil
}

// GetPackage retrieves a single package by ID.
func (ps *PackageStore) GetPackage(ctx context.Context, id string) (*agent.ThoughtPackage, error) {
	sql := fmt.Sprintf(`
		SELECT id, label, keywords, content, token_count, memory_ids,
			priority, freshness, version, created_at, updated_at
		FROM %s WHERE id = $1
	`, ps.tableName())

	var pkg agent.ThoughtPackage
	var keywords, memIDs []string
	var priority int

	err := ps.db.QueryRow(ctx, sql, id).Scan(
		&pkg.ID, &pkg.Label, &keywords, &pkg.Content, &pkg.TokenCount,
		&memIDs, &priority, &pkg.Freshness, &pkg.Version,
		&pkg.CreatedAt, &pkg.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("PackageStore.Get: %w", err)
	}
	pkg.Keywords = keywords
	pkg.MemoryIDs = memIDs
	pkg.Priority = agent.PackagePriority(priority)
	return &pkg, nil
}

// ListPackages returns all packages ordered by priority ASC, freshness DESC.
func (ps *PackageStore) ListPackages(ctx context.Context) ([]agent.ThoughtPackage, error) {
	sql := fmt.Sprintf(`
		SELECT id, label, keywords, content, token_count, memory_ids,
			priority, freshness, version, created_at, updated_at
		FROM %s
		ORDER BY priority ASC, freshness DESC
	`, ps.tableName())

	rows, err := ps.db.Query(ctx, sql)
	if err != nil {
		return nil, fmt.Errorf("PackageStore.List: %w", err)
	}
	defer rows.Close()

	var pkgs []agent.ThoughtPackage
	for rows.Next() {
		var pkg agent.ThoughtPackage
		var keywords, memIDs []string
		var priority int
		if err := rows.Scan(
			&pkg.ID, &pkg.Label, &keywords, &pkg.Content, &pkg.TokenCount,
			&memIDs, &priority, &pkg.Freshness, &pkg.Version,
			&pkg.CreatedAt, &pkg.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("PackageStore.List scan: %w", err)
		}
		pkg.Keywords = keywords
		pkg.MemoryIDs = memIDs
		pkg.Priority = agent.PackagePriority(priority)
		pkgs = append(pkgs, pkg)
	}
	return pkgs, rows.Err()
}

// DeletePackage removes a package by ID.
func (ps *PackageStore) DeletePackage(ctx context.Context, id string) error {
	sql := fmt.Sprintf(`DELETE FROM %s WHERE id = $1`, ps.tableName())
	_, err := ps.db.Exec(ctx, sql, id)
	return err
}

// AlwaysPackages returns all Priority=0 packages (always injected at runtime).
func (ps *PackageStore) AlwaysPackages(ctx context.Context) ([]agent.ThoughtPackage, error) {
	sql := fmt.Sprintf(`
		SELECT id, label, keywords, content, token_count, memory_ids,
			priority, freshness, version, created_at, updated_at
		FROM %s
		WHERE priority = 0
		ORDER BY freshness DESC
	`, ps.tableName())

	rows, err := ps.db.Query(ctx, sql)
	if err != nil {
		return nil, fmt.Errorf("PackageStore.Always: %w", err)
	}
	defer rows.Close()

	var pkgs []agent.ThoughtPackage
	for rows.Next() {
		var pkg agent.ThoughtPackage
		var keywords, memIDs []string
		var priority int
		if err := rows.Scan(
			&pkg.ID, &pkg.Label, &keywords, &pkg.Content, &pkg.TokenCount,
			&memIDs, &priority, &pkg.Freshness, &pkg.Version,
			&pkg.CreatedAt, &pkg.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("PackageStore.Always scan: %w", err)
		}
		pkg.Keywords = keywords
		pkg.MemoryIDs = memIDs
		pkg.Priority = agent.PackagePriority(priority)
		pkgs = append(pkgs, pkg)
	}
	return pkgs, rows.Err()
}

// MatchPackages returns packages relevant to a query, scored and sorted.
// Two-pass matching:
//   1. Keyword match (< 0.1ms, zero LLM cost) — check if query words overlap package keywords
//   2. Embedding similarity fallback — only if keyword match returns < 3 results
// tokenBudget limits total injected tokens (0 = no limit).
func (ps *PackageStore) MatchPackages(ctx context.Context, query string, tokenBudget int) ([]agent.PackageMatch, error) {
	// Load all packages (typically 50-100, very fast)
	pkgs, err := ps.ListPackages(ctx)
	if err != nil {
		return nil, err
	}

	queryWords := tokenize(query)
	var matches []agent.PackageMatch

	// Pass 1: Keyword matching
	for _, pkg := range pkgs {
		score := keywordScore(queryWords, pkg.Keywords)
		if score > 0 {
			matches = append(matches, agent.PackageMatch{
				Package: pkg,
				Score:   score,
				Method:  "keyword",
			})
		}
	}

	// Pass 2: Embedding similarity fallback (only if keyword match found < 3)
	if len(matches) < 3 && ps.embedder != nil {
		embedding, embErr := ps.embedder.Embed(ctx, query)
		if embErr == nil {
			sql := fmt.Sprintf(`
				SELECT id, label, keywords, content, token_count, memory_ids,
					priority, freshness, version, created_at, updated_at,
					1 - (embedding <=> $1::vector) AS sim
				FROM %s
				WHERE embedding IS NOT NULL
				ORDER BY embedding <=> $1::vector
				LIMIT 5
			`, ps.tableName())

			rows, qErr := ps.db.Query(ctx, sql, fmtVec(embedding))
			if qErr == nil {
				defer rows.Close()
				seen := make(map[string]bool)
				for _, m := range matches {
					seen[m.Package.ID] = true
				}
				for rows.Next() {
					var pkg agent.ThoughtPackage
					var keywords, memIDs []string
					var priority int
					var sim float32
					if err := rows.Scan(
						&pkg.ID, &pkg.Label, &keywords, &pkg.Content, &pkg.TokenCount,
						&memIDs, &priority, &pkg.Freshness, &pkg.Version,
						&pkg.CreatedAt, &pkg.UpdatedAt, &sim,
					); err != nil {
						continue
					}
					pkg.Keywords = keywords
					pkg.MemoryIDs = memIDs
					pkg.Priority = agent.PackagePriority(priority)
					if !seen[pkg.ID] && sim > 0.3 {
						matches = append(matches, agent.PackageMatch{
							Package: pkg,
							Score:   sim * 0.8, // slight discount vs keyword match
							Method:  "embedding",
						})
						seen[pkg.ID] = true
					}
				}
			}
		}
	}

	// Sort by score descending
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Score > matches[j].Score
	})

	// Apply token budget
	if tokenBudget > 0 {
		var budgeted []agent.PackageMatch
		used := 0
		for _, m := range matches {
			if used+m.Package.TokenCount > tokenBudget {
				continue
			}
			budgeted = append(budgeted, m)
			used += m.Package.TokenCount
		}
		matches = budgeted
	}

	ps.logger.Debug("packages matched",
		"query_words", len(queryWords),
		"keyword_hits", len(matches),
		"token_budget", tokenBudget,
	)
	return matches, nil
}

// ─── HELPERS ─────────────────────────────────────────────────────────────────

// tokenize splits a query into lowercase words for keyword matching.
func tokenize(text string) []string {
	words := strings.Fields(strings.ToLower(text))
	// Deduplicate
	seen := make(map[string]bool, len(words))
	unique := make([]string, 0, len(words))
	for _, w := range words {
		if len(w) < 2 {
			continue // skip single chars
		}
		if !seen[w] {
			seen[w] = true
			unique = append(unique, w)
		}
	}
	return unique
}

// keywordScore computes overlap between query words and package keywords.
// Returns 0.0-1.0 where 1.0 means all query words matched.
func keywordScore(queryWords, pkgKeywords []string) float32 {
	if len(queryWords) == 0 || len(pkgKeywords) == 0 {
		return 0
	}
	pkgSet := make(map[string]bool, len(pkgKeywords))
	for _, kw := range pkgKeywords {
		pkgSet[strings.ToLower(kw)] = true
	}
	var hits float32
	for _, qw := range queryWords {
		if pkgSet[qw] {
			hits++
			continue
		}
		// Partial match: query word is substring of a keyword or vice versa
		for _, kw := range pkgKeywords {
			kwLower := strings.ToLower(kw)
			if strings.Contains(kwLower, qw) || strings.Contains(qw, kwLower) {
				hits += 0.5
				break
			}
		}
	}
	return hits / float32(len(queryWords))
}

// Ensure PackageStore implements the interface.
var _ agent.PackageStore = (*PackageStore)(nil)
