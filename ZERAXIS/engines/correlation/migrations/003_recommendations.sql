-- Phase 6.4: Recommendations Table
CREATE TABLE IF NOT EXISTS incident_recommendations (
    recommendation_id UUID PRIMARY KEY,
    incident_id       TEXT NOT NULL REFERENCES incidents(incident_id) ON DELETE CASCADE,
    fingerprint       TEXT NOT NULL UNIQUE, -- Deterministic generation fingerprint
    action            TEXT NOT NULL,
    target_type       TEXT,
    target_id         TEXT,
    priority          TEXT NOT NULL,
    confidence        INTEGER,
    reason            TEXT NOT NULL,
    evidence          JSONB NOT NULL,
    model_version     TEXT NOT NULL,
    status            TEXT NOT NULL DEFAULT 'PROPOSED',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    CONSTRAINT chk_recommendation_status CHECK (status IN ('PROPOSED', 'ACKNOWLEDGED', 'DISMISSED'))
);

CREATE INDEX IF NOT EXISTS idx_recommendations_incident_id ON incident_recommendations(incident_id);
CREATE INDEX IF NOT EXISTS idx_recommendations_status ON incident_recommendations(status);
CREATE INDEX IF NOT EXISTS idx_recommendations_created_at ON incident_recommendations(created_at);
CREATE INDEX IF NOT EXISTS idx_recommendations_priority ON incident_recommendations(priority);
