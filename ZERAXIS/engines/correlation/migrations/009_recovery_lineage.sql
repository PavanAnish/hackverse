-- Phase 7.4: Recovery Lineage
-- Extend incident_recommendations to track the failed execution it recovers.

ALTER TABLE incident_recommendations
ADD COLUMN IF NOT EXISTS recovery_for_execution_id UUID
REFERENCES execution_results(execution_id)
ON DELETE SET NULL;
