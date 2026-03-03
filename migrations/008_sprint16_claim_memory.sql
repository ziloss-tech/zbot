-- Sprint 16: Research Claim Memory
-- Stores individual verified claims from completed research sessions.
-- Enables cross-session prior knowledge injection and temporal drift detection.

-- Staleness tiers determine how aggressively old data is flagged:
--   fast   = changes in days/weeks (pricing, AI model releases, news)
--   medium = changes in months (product features, company strategy)
--   slow   = changes in years (fundamentals, tech architecture)

CREATE TABLE IF NOT EXISTS research_claims (
    id             TEXT PRIMARY KEY,                    -- "claim_{sessionID}_{clmID}"
    session_id     TEXT NOT NULL,                       -- source research session
    goal           TEXT NOT NULL,                       -- research goal that produced this
    statement      TEXT NOT NULL,                       -- the atomic claim
    confidence     FLOAT NOT NULL DEFAULT 0.7,
    topic_category TEXT NOT NULL DEFAULT 'general',     -- semantic cluster (e.g. "crm pricing")
    staleness_tier TEXT NOT NULL DEFAULT 'medium',      -- fast | medium | slow
    captured_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    superseded_by  TEXT DEFAULT NULL,                   -- claim ID that replaced this (history kept)
    evidence_ids   TEXT[] NOT NULL DEFAULT '{}',
    embedding      vector(768)
);

-- HNSW index for fast semantic search across millions of claims.
CREATE INDEX IF NOT EXISTS research_claims_embedding_idx
    ON research_claims USING hnsw (embedding vector_cosine_ops)
    WITH (m = 16, ef_construction = 64);

-- FTS index for keyword search fallback.
CREATE INDEX IF NOT EXISTS research_claims_fts_idx
    ON research_claims USING gin (to_tsvector('english', statement || ' ' || goal));

-- Time-based index for staleness queries.
CREATE INDEX IF NOT EXISTS research_claims_captured_idx
    ON research_claims (captured_at DESC);

-- Topic + time for cluster queries.
CREATE INDEX IF NOT EXISTS research_claims_topic_time_idx
    ON research_claims (topic_category, captured_at DESC);
