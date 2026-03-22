-- 009_factory_sessions.sql
-- Factory planning session persistence.
-- Stores the full plan state as JSONB so sessions survive ZBOT restarts.

CREATE TABLE IF NOT EXISTS factory_sessions (
    id TEXT PRIMARY KEY,
    idea TEXT NOT NULL,
    phase TEXT NOT NULL DEFAULT 'interview',
    state JSONB NOT NULL,
    decisions JSONB DEFAULT '[]'::jsonb,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_factory_sessions_phase ON factory_sessions (phase);
CREATE INDEX IF NOT EXISTS idx_factory_sessions_updated ON factory_sessions (updated_at DESC);
