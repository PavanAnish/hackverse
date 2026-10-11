-- Phase 6.2: Incident Correlation Tables
-- DO NOT duplicate: events, findings, finding_events, supply_chain_*, discovery_*

-- ============================================================
-- incidents
-- One row per logical security scenario.
-- Fingerprint is deterministic: SHA256(asset_id || ':' || coalesce(image_digest,'') || ':' || hour_bucket).
-- Risk columns are NULL until Phase 6.3 fills them in.
-- ============================================================
CREATE TABLE IF NOT EXISTS incidents (
    incident_id        TEXT        PRIMARY KEY,
    fingerprint        TEXT        NOT NULL UNIQUE,
    status             TEXT        NOT NULL DEFAULT 'OPEN',   -- OPEN | ACTIVE | RESOLVED | CLOSED
    asset_id           TEXT,                                   -- Phase 2 asset identity (nullable for future cross-asset)
    container_id       TEXT,                                   -- Phase 5 runtime identity at creation time
    image_digest       TEXT,                                   -- Phase 3 software artifact identity
    severity           TEXT        NOT NULL DEFAULT 'INFO',   -- max severity among member findings
    finding_count      INTEGER     NOT NULL DEFAULT 1,
    -- Phase 6.3 placeholders (never computed here)
    static_risk        INTEGER,
    dynamic_risk       INTEGER,
    combined_risk      INTEGER,
    confidence         INTEGER,
    risk_model_version TEXT,
    -- Timestamps
    first_seen         TIMESTAMPTZ NOT NULL,
    last_seen          TIMESTAMPTZ NOT NULL,
    resolved_at        TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- CONSTRAINT: status must be a known value
    CONSTRAINT incidents_status_check
        CHECK (status IN ('OPEN', 'ACTIVE', 'RESOLVED', 'CLOSED'))
);

CREATE INDEX IF NOT EXISTS idx_incidents_fingerprint   ON incidents (fingerprint);
CREATE INDEX IF NOT EXISTS idx_incidents_asset_status  ON incidents (asset_id, status);
CREATE INDEX IF NOT EXISTS idx_incidents_image_digest  ON incidents (image_digest);
CREATE INDEX IF NOT EXISTS idx_incidents_last_seen     ON incidents (last_seen DESC);
CREATE INDEX IF NOT EXISTS idx_incidents_status        ON incidents (status);

-- ============================================================
-- incident_findings
-- Many-to-many mapping: which findings belong to which incident.
-- Finding payloads are NOT duplicated here; only the reference.
-- ============================================================
CREATE TABLE IF NOT EXISTS incident_findings (
    incident_id  TEXT        NOT NULL REFERENCES incidents (incident_id) ON DELETE CASCADE,
    finding_id   TEXT        NOT NULL,   -- FK intent: references findings(finding_id) in detection schema
    linked_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (incident_id, finding_id)
);

CREATE INDEX IF NOT EXISTS idx_incident_findings_finding_id  ON incident_findings (finding_id);
CREATE INDEX IF NOT EXISTS idx_incident_findings_incident_id ON incident_findings (incident_id);
