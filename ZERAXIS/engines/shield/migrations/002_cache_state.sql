CREATE TABLE shield_cache_state (
    fingerprint TEXT PRIMARY KEY,
    block_reason TEXT, -- NULL if allowed
    cached_at TIMESTAMPTZ DEFAULT NOW(),
    ttl_expires_at TIMESTAMPTZ,
    incident_id UUID,
    last_event_id TEXT -- For dedup
);

CREATE INDEX idx_shield_cache_ttl ON shield_cache_state(ttl_expires_at);
