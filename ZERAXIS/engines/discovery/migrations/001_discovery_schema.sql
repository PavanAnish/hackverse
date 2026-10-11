-- Discovery engine schema
-- Does NOT modify Phase 1 tables (products, payments).
-- All tables prefixed with discovery_ to avoid namespace conflicts.

CREATE TABLE IF NOT EXISTS discovery_assets (
    asset_id       TEXT PRIMARY KEY,
    asset_type     TEXT NOT NULL DEFAULT 'container',
    name           TEXT NOT NULL UNIQUE,
    container_id   TEXT,
    image          TEXT,
    image_id       TEXT,
    status         TEXT NOT NULL DEFAULT 'unknown',
    created_at     TIMESTAMPTZ,
    started_at     TIMESTAMPTZ,
    labels         JSONB DEFAULT '{}',
    discovered_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS discovery_asset_ports (
    id             SERIAL PRIMARY KEY,
    asset_id       TEXT NOT NULL REFERENCES discovery_assets(asset_id) ON DELETE CASCADE,
    container_port INTEGER NOT NULL,
    host_port      INTEGER DEFAULT 0,
    protocol       TEXT DEFAULT 'tcp',
    host_ip        TEXT DEFAULT ''
);

CREATE TABLE IF NOT EXISTS discovery_asset_networks (
    id             SERIAL PRIMARY KEY,
    asset_id       TEXT NOT NULL REFERENCES discovery_assets(asset_id) ON DELETE CASCADE,
    network        TEXT NOT NULL,
    ip_address     TEXT DEFAULT '',
    gateway        TEXT DEFAULT ''
);

-- NETWORK_MEMBERSHIP relationships indicate that two assets share the same
-- Docker network and therefore have potential connectivity. This does NOT
-- indicate that actual application-level communication has been observed.
-- Observed communication relationships belong to the runtime monitoring phase.
CREATE TABLE IF NOT EXISTS discovery_relationships (
    id                TEXT PRIMARY KEY,
    source_asset_id   TEXT NOT NULL REFERENCES discovery_assets(asset_id) ON DELETE CASCADE,
    dest_asset_id     TEXT NOT NULL REFERENCES discovery_assets(asset_id) ON DELETE CASCADE,
    network           TEXT NOT NULL,
    relationship_type TEXT NOT NULL DEFAULT 'NETWORK_MEMBERSHIP',
    discovered_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(source_asset_id, dest_asset_id, network, relationship_type)
);

CREATE TABLE IF NOT EXISTS discovery_events (
    id         TEXT PRIMARY KEY,
    event_type TEXT NOT NULL,
    asset_id   TEXT,
    asset_name TEXT,
    details    JSONB DEFAULT '{}',
    scan_id    TEXT,
    timestamp  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_discovery_events_timestamp ON discovery_events(timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_discovery_events_asset ON discovery_events(asset_id);
CREATE INDEX IF NOT EXISTS idx_discovery_assets_status ON discovery_assets(status);
CREATE INDEX IF NOT EXISTS idx_discovery_asset_ports_asset ON discovery_asset_ports(asset_id);
CREATE INDEX IF NOT EXISTS idx_discovery_asset_networks_asset ON discovery_asset_networks(asset_id);
