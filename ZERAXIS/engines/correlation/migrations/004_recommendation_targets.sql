-- Phase 7.1: Explicit Execution Targets
ALTER TABLE incident_recommendations 
ADD COLUMN IF NOT EXISTS target_asset_id TEXT,
ADD COLUMN IF NOT EXISTS target_container_id TEXT,
ADD COLUMN IF NOT EXISTS target_image_digest TEXT;
