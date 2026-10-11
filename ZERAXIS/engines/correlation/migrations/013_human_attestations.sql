-- Phase 7.8: Human Quarantine Resolution and Technical Debt Fixes

-- 1. Remove the UNIQUE constraint from fingerprint to allow new incidents for RESOLVED fingerprints
-- Justification: The fingerprint represents a deterministic 1-hour time bucket for a given target. 
-- If an incident is manually remediated and closed within 15 minutes, and a recurrence arrives 10 minutes later 
-- (still within the same 1-hour bucket), it logically shares the same deterministic fingerprint but requires a 
-- new incident_id. Therefore, dropping the UNIQUE constraint is data-model mandatory to support intra-bucket 
-- recurrences without corrupting the finding's hash semantics.
ALTER TABLE incidents DROP CONSTRAINT IF EXISTS incidents_fingerprint_key;
CREATE UNIQUE INDEX IF NOT EXISTS idx_incidents_fingerprint_active ON incidents(fingerprint) WHERE status NOT IN ('RESOLVED', 'CLOSED');
CREATE INDEX IF NOT EXISTS idx_incidents_fingerprint_created ON incidents(fingerprint, created_at DESC);

-- 2. Add SUPERSEDED and MANUALLY_REMEDIATED to allowed recommendation statuses
ALTER TABLE incident_recommendations DROP CONSTRAINT IF EXISTS chk_recommendation_status;

ALTER TABLE incident_recommendations
ADD CONSTRAINT chk_recommendation_status 
CHECK (status IN ('PROPOSED', 'ACKNOWLEDGED', 'DISMISSED', 'APPROVED', 'EXECUTED', 'MANUAL_INVESTIGATION_REQUIRED', 'SUPERSEDED', 'MANUALLY_REMEDIATED'));

-- 3. Create the human remediation attestations table
CREATE TABLE IF NOT EXISTS human_remediation_attestations (
    attestation_id       UUID PRIMARY KEY,
    recommendation_id    UUID NOT NULL REFERENCES incident_recommendations(recommendation_id),
    incident_id          TEXT NOT NULL REFERENCES incidents(incident_id),
    operator_id          TEXT NOT NULL,
    outcome              TEXT NOT NULL CHECK (outcome IN ('OUTCOME_MANUALLY_REMEDIATED', 'OUTCOME_SAFE_TO_RETRY')),
    reason               TEXT NOT NULL,
    target_asset_id      TEXT,
    target_container_id  TEXT,
    target_image_digest  TEXT,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
