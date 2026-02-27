-- Sprint 11: Dual Brain Command Center
-- Adds task output/timing columns and SSE event log for streaming replay.

-- Store Claude's output per task
ALTER TABLE zbot_tasks ADD COLUMN IF NOT EXISTS output TEXT NOT NULL DEFAULT '';
ALTER TABLE zbot_tasks ADD COLUMN IF NOT EXISTS started_at TIMESTAMPTZ;
ALTER TABLE zbot_tasks ADD COLUMN IF NOT EXISTS finished_at TIMESTAMPTZ;
ALTER TABLE zbot_tasks ADD COLUMN IF NOT EXISTS error TEXT NOT NULL DEFAULT '';

-- Store the goal text on workflows for display
ALTER TABLE zbot_workflows ADD COLUMN IF NOT EXISTS goal TEXT NOT NULL DEFAULT '';

-- SSE event log for replay on reconnect
CREATE TABLE IF NOT EXISTS zbot_stream_events (
    id          BIGSERIAL PRIMARY KEY,
    workflow_id TEXT NOT NULL,
    task_id     TEXT,
    source      TEXT NOT NULL,      -- 'planner' | 'executor'
    event_type  TEXT NOT NULL,      -- 'token' | 'status' | 'handoff' | 'complete' | 'error'
    payload     TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_stream_events_workflow ON zbot_stream_events(workflow_id, id);
