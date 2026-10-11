CREATE TABLE IF NOT EXISTS findings (
    finding_id      TEXT PRIMARY KEY,
    fingerprint     TEXT NOT NULL UNIQUE,
    rule_id         TEXT NOT NULL,
    rule_version    INTEGER NOT NULL,
    severity        TEXT NOT NULL,
    confidence      TEXT NOT NULL,
    status          TEXT NOT NULL,
    first_seen      TIMESTAMPTZ NOT NULL,
    last_seen       TIMESTAMPTZ NOT NULL,
    asset_id        TEXT,
    image_digest    TEXT,
    evidence        TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_findings_fingerprint ON findings(fingerprint);
CREATE INDEX IF NOT EXISTS idx_findings_asset_status ON findings(asset_id, status);
CREATE INDEX IF NOT EXISTS idx_findings_rule_status ON findings(rule_id, status);
CREATE INDEX IF NOT EXISTS idx_findings_updated_at ON findings(updated_at DESC);

CREATE TABLE IF NOT EXISTS finding_events (
    finding_id      TEXT NOT NULL REFERENCES findings(finding_id) ON DELETE CASCADE,
    event_id        TEXT NOT NULL,
    PRIMARY KEY (finding_id, event_id)
);

CREATE INDEX IF NOT EXISTS idx_finding_events_event_id ON finding_events(event_id);
