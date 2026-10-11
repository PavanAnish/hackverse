-- Phase 7.2: Execution Results
CREATE TABLE IF NOT EXISTS execution_results (
    execution_id UUID PRIMARY KEY REFERENCES execution_authorizations(execution_id) ON DELETE CASCADE,
    recommendation_id UUID NOT NULL REFERENCES incident_recommendations(recommendation_id) ON DELETE CASCADE,
    status TEXT NOT NULL,
    executor_version TEXT DEFAULT '1.0',
    error_log TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_exec_res_status ON execution_results(status);
