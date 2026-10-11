CREATE TABLE IF NOT EXISTS events (
    event_id            TEXT PRIMARY KEY,
    event_type          TEXT NOT NULL,
    event_version       INTEGER NOT NULL,
    timestamp           TIMESTAMPTZ NOT NULL,
    ingestion_timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    source              TEXT NOT NULL,
    asset_id            TEXT,
    image_digest        TEXT,
    severity            TEXT,
    correlation_id      TEXT,
    trace_id            TEXT,
    metadata            JSONB NOT NULL,
    subject             TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_events_timestamp ON events(timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_events_asset_id ON events(asset_id);
CREATE INDEX IF NOT EXISTS idx_events_image_digest ON events(image_digest);
CREATE INDEX IF NOT EXISTS idx_events_type ON events(event_type);
