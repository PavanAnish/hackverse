-- Phase 7.6: Add EXECUTED to allowed recommendation statuses
ALTER TABLE incident_recommendations
DROP CONSTRAINT IF EXISTS chk_recommendation_status;

ALTER TABLE incident_recommendations
ADD CONSTRAINT chk_recommendation_status 
CHECK (status IN ('PROPOSED', 'ACKNOWLEDGED', 'DISMISSED', 'APPROVED', 'EXECUTED'));
