-- Migration 001: Initial ZBOT schema
-- Run via: golang-migrate or manually
-- Idempotent — safe to run multiple times.

-- Enable pgvector extension (required for embeddings).
CREATE EXTENSION IF NOT EXISTS vector;

-- ─── MEMORY ───────────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS zbot_memories (
    id         TEXT PRIMARY KEY,
    content    TEXT NOT NULL,
    source     TEXT NOT NULL DEFAULT 'conversation',
    tags       TEXT[] NOT NULL DEFAULT '{}',
    embedding  vector(768),          -- Vertex AI text-embedding-004 dimensions
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- HNSW index for fast approximate nearest-neighbor search.
-- m=16, ef_construction=64 matches the existing mem0 Ziloss config.
CREATE INDEX IF NOT EXISTS zbot_memories_embedding_idx
    ON zbot_memories USING hnsw (embedding vector_cosine_ops)
    WITH (m = 16, ef_construction = 64);

-- GIN index for full-text search (BM25 via tsvector).
CREATE INDEX IF NOT EXISTS zbot_memories_fts_idx
    ON zbot_memories USING gin (to_tsvector('english', content));

CREATE INDEX IF NOT EXISTS zbot_memories_created_idx
    ON zbot_memories (created_at DESC);

-- ─── WORKFLOWS ────────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS zbot_workflows (
    id         TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    status     TEXT NOT NULL DEFAULT 'running',  -- running | done | canceled | failed
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS zbot_tasks (
    id           TEXT PRIMARY KEY,
    workflow_id  TEXT NOT NULL REFERENCES zbot_workflows(id) ON DELETE CASCADE,
    step         INTEGER NOT NULL,
    name         TEXT NOT NULL,
    instruction  TEXT NOT NULL,
    status       TEXT NOT NULL DEFAULT 'pending',  -- pending | running | done | failed | canceled
    depends_on   TEXT[] NOT NULL DEFAULT '{}',      -- array of task IDs
    input_ref    TEXT,                              -- reference key in zbot_data
    output_ref   TEXT,                              -- reference key in zbot_data
    worker_id    TEXT,                              -- which worker claimed this task
    error_msg    TEXT,
    claimed_at   TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Index for the SELECT FOR UPDATE SKIP LOCKED task queue pattern.
CREATE INDEX IF NOT EXISTS zbot_tasks_queue_idx
    ON zbot_tasks (status, workflow_id)
    WHERE status = 'pending';

CREATE INDEX IF NOT EXISTS zbot_tasks_workflow_idx
    ON zbot_tasks (workflow_id);

-- ─── DATA STORE ───────────────────────────────────────────────────────────────
-- Stores task inputs and outputs externally (not in context windows).
CREATE TABLE IF NOT EXISTS zbot_data (
    ref        TEXT PRIMARY KEY,     -- UUID reference key
    content    TEXT NOT NULL,        -- JSON or text content
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ           -- NULL = keep forever, set for ephemeral data
);

CREATE INDEX IF NOT EXISTS zbot_data_expires_idx
    ON zbot_data (expires_at)
    WHERE expires_at IS NOT NULL;

-- ─── AUDIT LOG ────────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS zbot_audit (
    id          BIGSERIAL PRIMARY KEY,
    event_type  TEXT NOT NULL,       -- 'tool_call' | 'model_call' | 'workflow_event'
    session_id  TEXT,
    workflow_id TEXT,
    task_id     TEXT,
    details     JSONB NOT NULL DEFAULT '{}',
    duration_ms BIGINT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS zbot_audit_session_idx ON zbot_audit (session_id, created_at DESC);
CREATE INDEX IF NOT EXISTS zbot_audit_workflow_idx ON zbot_audit (workflow_id, created_at DESC);
CREATE INDEX IF NOT EXISTS zbot_audit_created_idx ON zbot_audit (created_at DESC);

-- ─── SESSION HISTORY ──────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS zbot_sessions (
    id         TEXT PRIMARY KEY,
    messages   JSONB NOT NULL DEFAULT '[]',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Auto-expire sessions after 90 days (run via pg_cron or external job).
CREATE INDEX IF NOT EXISTS zbot_sessions_updated_idx ON zbot_sessions (updated_at DESC);
