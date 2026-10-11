CREATE TABLE IF NOT EXISTS execution_authorizations (
    execution_id UUID PRIMARY KEY,
    recommendation_id UUID NOT NULL REFERENCES incident_recommendations(recommendation_id) ON DELETE CASCADE,
    approver_id TEXT NOT NULL,
    status TEXT NOT NULL,
    approved_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_exec_auth_status ON execution_authorizations(status);
