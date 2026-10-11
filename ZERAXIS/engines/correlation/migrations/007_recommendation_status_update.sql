-- Phase 7.2: Extend recommendation status to include all lifecycle states
-- The original constraint only listed PROPOSED/ACKNOWLEDGED/DISMISSED/APPROVED,
-- causing a startup crash after any successful demo run that wrote EXECUTED,
-- MANUAL_INVESTIGATION_REQUIRED, MANUALLY_REMEDIATED, SUPERSEDED, or REVOKED rows.
ALTER TABLE incident_recommendations
DROP CONSTRAINT IF EXISTS chk_recommendation_status;

ALTER TABLE incident_recommendations
ADD CONSTRAINT chk_recommendation_status
CHECK (status IN (
    'PROPOSED',
    'ACKNOWLEDGED',
    'DISMISSED',
    'APPROVED',
    'EXECUTED',
    'MANUAL_INVESTIGATION_REQUIRED',
    'MANUALLY_REMEDIATED',
    'SUPERSEDED',
    'REVOKED'
));

-- Ensure incidents table has risk_level as required by Executor Phase 7.2
ALTER TABLE incidents
ADD COLUMN IF NOT EXISTS risk_level TEXT NOT NULL DEFAULT 'UNKNOWN';
