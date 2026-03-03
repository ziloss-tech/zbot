-- Sprint Deep Research: multi-agent iterative research pipeline.
-- Tables for research sessions and model spend tracking.

-- Research sessions — tracks each deep research run.
CREATE TABLE IF NOT EXISTS zbot_research_sessions (
    id TEXT PRIMARY KEY,
    workflow_id TEXT,
    goal TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'running',  -- running | complete | failed
    iterations INT DEFAULT 0,
    confidence_score FLOAT DEFAULT 0,
    final_report TEXT,
    state_json JSONB,  -- full ResearchState for inspection
    cost_usd FLOAT DEFAULT 0,
    error TEXT DEFAULT '',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_research_sessions_status ON zbot_research_sessions(status);

-- Model spend — per-call cost tracking for budget enforcement ($6.67/day cap).
CREATE TABLE IF NOT EXISTS zbot_model_spend (
    id SERIAL PRIMARY KEY,
    session_id TEXT,
    model_id TEXT,
    prompt_tokens INT,
    completion_tokens INT,
    cost_usd FLOAT,
    recorded_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_spend_date ON zbot_model_spend(recorded_at);
CREATE INDEX IF NOT EXISTS idx_spend_session ON zbot_model_spend(session_id);
