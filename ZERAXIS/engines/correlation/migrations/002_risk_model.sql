-- Phase 6.3: Risk and Attack Chain Model additions

ALTER TABLE incidents
ADD COLUMN IF NOT EXISTS risk_level TEXT,
ADD COLUMN IF NOT EXISTS calculated_at TIMESTAMPTZ,
ADD COLUMN IF NOT EXISTS attack_chain_state TEXT,
ADD COLUMN IF NOT EXISTS attack_chain_evidence JSONB;

-- Index for querying incidents by risk level and attack chain state
CREATE INDEX IF NOT EXISTS idx_incidents_risk_level ON incidents (risk_level);
CREATE INDEX IF NOT EXISTS idx_incidents_attack_chain ON incidents (attack_chain_state);
