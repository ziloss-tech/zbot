package research

// claimmemory.go — Cross-session claim memory for the research pipeline.
//
// Architecture:
//   After each completed session → SaveClaims() stores every verified claim
//   with a timestamp, topic category, and staleness tier.
//
//   Before planning → SearchPriorKnowledge() finds semantically related claims
//   from past sessions and returns them as formatted prior context. Old claims
//   in fast-changing categories are flagged for re-verification.
//
//   Instead of deleting outdated claims, we mark them superseded_by a newer
//   claim ID — preserving the full timeline of how knowledge evolved.

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ziloss-tech/zbot/internal/memory"
)

// stalenessAge defines how long a claim is considered fresh per tier.
var stalenessAge = map[string]time.Duration{
	"fast":   14 * 24 * time.Hour,  // 2 weeks
	"medium": 90 * 24 * time.Hour,  // 3 months
	"slow":   365 * 24 * time.Hour, // 1 year
}

// stalenessEmoji provides a visual signal in the prior context injected into the planner.
var stalenessEmoji = map[string]string{
	"fast":   "⚡",
	"medium": "🔄",
	"slow":   "🐢",
}

// fastKeywords — if any of these appear in the research goal or claim,
// the claim is assigned the "fast" staleness tier.
var fastKeywords = []string{
	"pric", "cost", "plan", "subscription", "fee",
	"releas", "launch", "announc", "update", "version",
	"funding", "acqui", "merger", "ipo",
	"ai model", "llm", "gpt", "claude", "gemini",
}

// slowKeywords — assign "slow" tier.
var slowKeywords = []string{
	"architect", "fundamentals", "how it works",
	"protocol", "standard", "infrastructure",
}

// StoredClaim is the DB row for a persisted research claim.
type StoredClaim struct {
	ID            string
	SessionID     string
	Goal          string
	Statement     string
	Confidence    float64
	TopicCategory string
	StalenessTier string
	CapturedAt    time.Time
	SupersededBy  *string
	EvidenceIDs   []string
}

// ClaimMemory manages cross-session research memory.
type ClaimMemory struct {
	db       *pgxpool.Pool
	embedder memory.Embedder
	logger   *slog.Logger
}

// NewClaimMemory creates a ClaimMemory. Runs the migration on startup.
func NewClaimMemory(ctx context.Context, db *pgxpool.Pool, embedder memory.Embedder, logger *slog.Logger) (*ClaimMemory, error) {
	cm := &ClaimMemory{db: db, embedder: embedder, logger: logger}
	if err := cm.migrate(ctx); err != nil {
		return nil, fmt.Errorf("claimmemory.New migrate: %w", err)
	}
	return cm, nil
}

// SaveClaims persists all verified claims from a completed research session.
// Each claim gets a timestamp, topic category inferred from the goal, and
// a staleness tier based on keyword heuristics.
// Existing claims on the same topic from older sessions are marked superseded.
func (cm *ClaimMemory) SaveClaims(ctx context.Context, sessionID, goal string, claims []Claim) error {
	if len(claims) == 0 {
		return nil
	}

	topic := inferTopicCategory(goal)
	tier := inferStalenessTier(goal)
	now := time.Now()

	for _, c := range claims {
		// Only persist high-confidence claims.
		if c.Confidence < 0.5 {
			continue
		}

		claimID := fmt.Sprintf("claim_%s_%s", sessionID, c.ID)

		embedding, err := cm.embedder.Embed(ctx, c.Statement)
		if err != nil {
			cm.logger.Warn("claimmemory: embed failed", "claim", c.ID, "err", err)
			continue
		}

		// Check if a very similar claim already exists — if so, mark it superseded.
		oldID, err := cm.findSimilarClaim(ctx, embedding, topic, 0.95)
		if err != nil {
			cm.logger.Warn("claimmemory: findSimilar failed", "err", err)
		} else if oldID != "" && oldID != claimID {
			_ = cm.markSuperseded(ctx, oldID, claimID)
		}

		_, err = cm.db.Exec(ctx, `
			INSERT INTO research_claims
				(id, session_id, goal, statement, confidence, topic_category,
				 staleness_tier, captured_at, evidence_ids, embedding)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			ON CONFLICT (id) DO UPDATE
				SET statement = EXCLUDED.statement,
				    confidence = EXCLUDED.confidence,
				    captured_at = EXCLUDED.captured_at
		`,
			claimID, sessionID, goal, c.Statement, c.Confidence,
			topic, tier, now, c.EvidenceIDs, fmtVec768(embedding),
		)
		if err != nil {
			cm.logger.Warn("claimmemory: insert failed", "claim", c.ID, "err", err)
		}
	}

	cm.logger.Info("claimmemory: claims saved",
		"session", sessionID, "count", len(claims),
		"topic", topic, "tier", tier,
	)
	return nil
}

// SearchPriorKnowledge finds semantically related claims from past sessions.
// Returns a formatted string ready to inject into the planner prompt.
// Claims are annotated with age and staleness signals so the planner can
// decide which facts to trust vs re-verify.
func (cm *ClaimMemory) SearchPriorKnowledge(ctx context.Context, goal string) (string, error) {
	embedding, err := cm.embedder.Embed(ctx, goal)
	if err != nil {
		return "", fmt.Errorf("claimmemory.Search embed: %w", err)
	}

	rows, err := cm.db.Query(ctx, `
		SELECT id, session_id, goal, statement, confidence,
		       topic_category, staleness_tier, captured_at, superseded_by
		FROM research_claims
		WHERE superseded_by IS NULL
		ORDER BY (1 - (embedding <=> $1::vector)) * 
		         EXP(-0.005 * EXTRACT(EPOCH FROM (NOW() - captured_at)) / 86400) DESC
		LIMIT 20
	`, fmtVec768(embedding))
	if err != nil {
		return "", fmt.Errorf("claimmemory.Search query: %w", err)
	}
	defer rows.Close()

	var claims []StoredClaim
	for rows.Next() {
		var sc StoredClaim
		if err := rows.Scan(
			&sc.ID, &sc.SessionID, &sc.Goal, &sc.Statement, &sc.Confidence,
			&sc.TopicCategory, &sc.StalenessTier, &sc.CapturedAt, &sc.SupersededBy,
		); err != nil {
			continue
		}
		claims = append(claims, sc)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}

	if len(claims) == 0 {
		return "", nil
	}

	return cm.formatPriorKnowledge(claims), nil
}

// formatPriorKnowledge builds the prior context string injected into the planner.
// Format is designed for the planner to reason about: what do I already know,
// how old is it, and is it likely still accurate?
func (cm *ClaimMemory) formatPriorKnowledge(claims []StoredClaim) string {
	var sb strings.Builder
	sb.WriteString("## Prior Knowledge (from previous research sessions)\n")
	sb.WriteString("Use this to avoid re-discovering known facts. Re-verify STALE items marked ⚡.\n\n")

	now := time.Now()
	for _, c := range claims {
		age := now.Sub(c.CapturedAt)
		ageDays := int(age.Hours() / 24)
		fresh := age < stalenessAge[c.StalenessTier]

		ageStr := formatAge(ageDays)
		emoji := stalenessEmoji[c.StalenessTier]
		tierUpper := strings.ToUpper(c.StalenessTier)

		freshTag := "✅ CURRENT"
		if !fresh {
			freshTag = "⚠️  STALE — re-verify"
		}

		sb.WriteString(fmt.Sprintf("[%s — %s — %s %s — %s]\n%s\n\n",
			ageStr, c.TopicCategory, emoji, tierUpper, freshTag, c.Statement,
		))
	}

	return sb.String()
}

// ─── HELPERS ────────────────────────────────────────────────────────────────

// inferTopicCategory derives a topic label from the research goal.
// We use a short cleaned version of the goal as the category —
// semantic search handles clustering naturally without brittle enumerations.
func inferTopicCategory(goal string) string {
	goal = strings.ToLower(goal)
	goal = strings.TrimSpace(goal)
	// Truncate to first 60 chars as the category label.
	if len(goal) > 60 {
		goal = goal[:60]
	}
	return goal
}

// inferStalenessTier assigns a tier based on keyword heuristics.
func inferStalenessTier(text string) string {
	lower := strings.ToLower(text)
	for _, kw := range fastKeywords {
		if strings.Contains(lower, kw) {
			return "fast"
		}
	}
	for _, kw := range slowKeywords {
		if strings.Contains(lower, kw) {
			return "slow"
		}
	}
	return "medium"
}

// findSimilarClaim returns the ID of an existing claim with cosine similarity
// above the threshold, or empty string if none found.
func (cm *ClaimMemory) findSimilarClaim(ctx context.Context, embedding []float32, topic string, threshold float64) (string, error) {
	var id string
	err := cm.db.QueryRow(ctx, `
		SELECT id
		FROM research_claims
		WHERE topic_category = $1
		  AND superseded_by IS NULL
		  AND 1 - (embedding <=> $2::vector) >= $3
		ORDER BY embedding <=> $2::vector
		LIMIT 1
	`, topic, fmtVec768(embedding), threshold).Scan(&id)
	if err != nil {
		return "", nil // not found is fine
	}
	return id, nil
}

// markSuperseded links an old claim to the newer one that replaced it.
// The old claim is preserved for timeline queries — never deleted.
func (cm *ClaimMemory) markSuperseded(ctx context.Context, oldID, newID string) error {
	_, err := cm.db.Exec(ctx,
		`UPDATE research_claims SET superseded_by = $1 WHERE id = $2`,
		newID, oldID,
	)
	return err
}

// fmtVec768 formats a []float32 as a pgvector literal.
func fmtVec768(v []float32) string {
	parts := make([]string, len(v))
	for i, f := range v {
		parts[i] = fmt.Sprintf("%g", f)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// formatAge returns a human-readable age string.
func formatAge(days int) string {
	switch {
	case days == 0:
		return "today"
	case days == 1:
		return "1 day ago"
	case days < 7:
		return fmt.Sprintf("%d days ago", days)
	case days < 30:
		weeks := days / 7
		if weeks == 1 {
			return "1 week ago"
		}
		return fmt.Sprintf("%d weeks ago", weeks)
	case days < 365:
		months := days / 30
		if months == 1 {
			return "1 month ago"
		}
		return fmt.Sprintf("%d months ago", months)
	default:
		return fmt.Sprintf("%d months ago", days/30)
	}
}

// migrate creates the research_claims table if it doesn't exist.
func (cm *ClaimMemory) migrate(ctx context.Context) error {
	_, err := cm.db.Exec(ctx, `
		CREATE EXTENSION IF NOT EXISTS vector;

		CREATE TABLE IF NOT EXISTS research_claims (
			id             TEXT PRIMARY KEY,
			session_id     TEXT NOT NULL,
			goal           TEXT NOT NULL,
			statement      TEXT NOT NULL,
			confidence     FLOAT NOT NULL DEFAULT 0.7,
			topic_category TEXT NOT NULL DEFAULT 'general',
			staleness_tier TEXT NOT NULL DEFAULT 'medium',
			captured_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			superseded_by  TEXT DEFAULT NULL,
			evidence_ids   TEXT[] NOT NULL DEFAULT '{}',
			embedding      vector(768)
		);

		CREATE INDEX IF NOT EXISTS research_claims_embedding_idx
			ON research_claims USING hnsw (embedding vector_cosine_ops)
			WITH (m = 16, ef_construction = 64);

		CREATE INDEX IF NOT EXISTS research_claims_fts_idx
			ON research_claims USING gin (to_tsvector('english', statement || ' ' || goal));

		CREATE INDEX IF NOT EXISTS research_claims_captured_idx
			ON research_claims (captured_at DESC);

		CREATE INDEX IF NOT EXISTS research_claims_topic_time_idx
			ON research_claims (topic_category, captured_at DESC);
	`)
	if err != nil {
		return fmt.Errorf("claimmemory migrate: %w", err)
	}
	cm.logger.Info("claimmemory schema ready")
	return nil
}
