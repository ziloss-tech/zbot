-- Migration 001: Initial ZBOT schema
-- Run via golang-migrate on startup

-- Enable pgvector extension (already installed on existing GCP Cloud SQL instance)
CREATE EXTENSION IF NOT EXISTS vector;

-- ── ZBOT MEMORIES ───────────────────────────────────────────────────────────
-- Separate namespace from existing mem0_memories_vertex table.
-- Uses same Vertex AI text-embedding-004 (768 dims) for consistency.

CREATE TABLE IF NOT EXISTS zbot_memories (
    id          TEXT PRIMARY KEY,
    namespace   TEXT NOT NULL DEFAULT 'zbot',
    content     TEXT NOT NULL,
    source      TEXT NOT NULL DEFAULT 'agent',
    tags        JSONB NOT NULL DEFAULT '[]',
    embedding   vector(768) NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- HNSW index for fast approximate nearest-neighbor search.
-- m=16, ef_construction=64 — good balance of speed/recall for personal scale.
CREATE INDEX IF NOT EXISTS zbot_memories_embedding_idx
    ON zbot_memories USING hnsw (embedding vector_cosine_ops)
    WITH (m = 16, ef_construction = 64);

-- BM25 full-text index for the hybrid search.
CREATE INDEX IF NOT EXISTS zbot_memories_fts_idx
    ON zbot_memories USING GIN (to_tsvector('english', content));

-- Namespace filter (most queries filter by namespace first).
CREATE INDEX IF NOT EXISTS zbot_memories_namespace_idx ON zbot_memories (namespace);

-- ── WORKFLOW TASK GRAPH ──────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS zbot_workflows (
    id          TEXT PRIMARY KEY,
    session_id  TEXT NOT NULL,
    request     TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'running',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS zbot_tasks (
    id               TEXT PRIMARY KEY,
    workflow_id      TEXT NOT NULL REFERENCES zbot_workflows(id) ON DELETE CASCADE,
    step             INTEGER NOT NULL,
    name             TEXT NOT NULL,
    instruction      TEXT NOT NULL,
    status           TEXT NOT NULL DEFAULT 'pending',
    depends_on       TEXT[] NOT NULL DEFAULT '{}',
    input_ref        TEXT,          -- reference to datastore entry
    output_ref       TEXT,          -- reference to datastore entry
    claimed_by       TEXT,          -- worker ID
    claimed_at       TIMESTAMPTZ,
    error_msg        TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Index for the worker poll query (pending tasks with satisfied dependencies).
CREATE INDEX IF NOT EXISTS zbot_tasks_status_idx ON zbot_tasks (status, workflow_id);

-- ── EPHEMERAL DATA STORE ─────────────────────────────────────────────────────
-- Stores structured outputs between workflow steps.
-- TTL: auto-deleted after 7 days (cronjob in Sprint 4).

CREATE TABLE IF NOT EXISTS zbot_data_refs (
    ref         TEXT PRIMARY KEY,
    data        JSONB NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at  TIMESTAMPTZ NOT NULL DEFAULT NOW() + INTERVAL '7 days'
);

CREATE INDEX IF NOT EXISTS zbot_data_refs_expires_idx ON zbot_data_refs (expires_at);

-- ── AUDIT LOG ────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS zbot_audit_log (
    id           BIGSERIAL PRIMARY KEY,
    event_type   TEXT NOT NULL,   -- 'tool_call' | 'model_call' | 'workflow_event'
    session_id   TEXT,
    workflow_id  TEXT,
    task_id      TEXT,
    detail       JSONB NOT NULL DEFAULT '{}',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS zbot_audit_log_session_idx ON zbot_audit_log (session_id, created_at DESC);
CREATE INDEX IF NOT EXISTS zbot_audit_log_workflow_idx ON zbot_audit_log (workflow_id, created_at DESC);
