-- Sprint 8: Audit logging tables

CREATE TABLE IF NOT EXISTS zbot_audit_tool_calls (
    id           TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    session_id   TEXT NOT NULL,
    tool_name    TEXT NOT NULL,
    input        JSONB NOT NULL,
    output       TEXT,
    is_error     BOOLEAN NOT NULL DEFAULT FALSE,
    duration_ms  BIGINT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS zbot_audit_model_calls (
    id            TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    session_id    TEXT NOT NULL,
    model         TEXT NOT NULL,
    input_tokens  INTEGER NOT NULL,
    output_tokens INTEGER NOT NULL,
    duration_ms   BIGINT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS zbot_audit_workflow_events (
    id           TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    workflow_id  TEXT NOT NULL,
    task_id      TEXT,
    event        TEXT NOT NULL,
    detail       TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS audit_tool_session_idx ON zbot_audit_tool_calls(session_id, created_at DESC);
CREATE INDEX IF NOT EXISTS audit_model_session_idx ON zbot_audit_model_calls(session_id, created_at DESC);
CREATE INDEX IF NOT EXISTS audit_workflow_idx ON zbot_audit_workflow_events(workflow_id, created_at DESC);
