-- Supply Chain Engine Schema

CREATE TABLE IF NOT EXISTS supply_chain_images (
    image_id        TEXT PRIMARY KEY,
    repository      TEXT,
    tag             TEXT,
    digest          TEXT,
    created_at      TIMESTAMPTZ,
    size_bytes      BIGINT,
    discovered_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_scanned_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS supply_chain_asset_images (
    asset_id        TEXT NOT NULL,
    image_id        TEXT NOT NULL REFERENCES supply_chain_images(image_id) ON DELETE CASCADE,
    discovered_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (asset_id, image_id)
);

CREATE TABLE IF NOT EXISTS supply_chain_scans (
    scan_id         TEXT PRIMARY KEY,
    image_id        TEXT NOT NULL REFERENCES supply_chain_images(image_id) ON DELETE CASCADE,
    scan_type       TEXT NOT NULL,
    status          TEXT NOT NULL,
    started_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at    TIMESTAMPTZ,
    error           TEXT,
    syft_version    TEXT,
    trivy_version   TEXT
);

CREATE TABLE IF NOT EXISTS supply_chain_sboms (
    sbom_id         TEXT PRIMARY KEY,
    image_id        TEXT NOT NULL REFERENCES supply_chain_images(image_id) ON DELETE CASCADE,
    scan_id         TEXT NOT NULL REFERENCES supply_chain_scans(scan_id) ON DELETE CASCADE,
    format          TEXT NOT NULL DEFAULT 'cyclonedx',
    spec_version    TEXT,
    generated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    tool            TEXT NOT NULL DEFAULT 'syft',
    tool_version    TEXT,
    document_digest TEXT,
    package_count   INTEGER DEFAULT 0,
    raw_sbom        JSONB
);

CREATE TABLE IF NOT EXISTS supply_chain_packages (
    package_id      TEXT PRIMARY KEY,
    name            TEXT NOT NULL,
    version         TEXT NOT NULL,
    package_type    TEXT,
    purl            TEXT,
    licenses        TEXT[],
    UNIQUE (name, version, package_type)
);

CREATE TABLE IF NOT EXISTS supply_chain_sbom_packages (
    sbom_id         TEXT NOT NULL REFERENCES supply_chain_sboms(sbom_id) ON DELETE CASCADE,
    package_id      TEXT NOT NULL REFERENCES supply_chain_packages(package_id) ON DELETE CASCADE,
    PRIMARY KEY (sbom_id, package_id)
);

CREATE TABLE IF NOT EXISTS supply_chain_vulnerabilities (
    vuln_id             TEXT PRIMARY KEY,
    vulnerability_identifier TEXT NOT NULL,
    package_id          TEXT NOT NULL REFERENCES supply_chain_packages(package_id) ON DELETE CASCADE,
    image_id            TEXT NOT NULL REFERENCES supply_chain_images(image_id) ON DELETE CASCADE,
    scan_id             TEXT NOT NULL REFERENCES supply_chain_scans(scan_id) ON DELETE CASCADE,
    severity            TEXT NOT NULL,
    installed_version   TEXT,
    fixed_version       TEXT,
    title               TEXT,
    description         TEXT,
    source              TEXT NOT NULL DEFAULT 'trivy',
    detected_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    status              TEXT NOT NULL DEFAULT 'active',
    UNIQUE (vulnerability_identifier, package_id, image_id)
);

CREATE TABLE IF NOT EXISTS supply_chain_policy_decisions (
    decision_id         TEXT PRIMARY KEY,
    image_id            TEXT NOT NULL REFERENCES supply_chain_images(image_id) ON DELETE CASCADE,
    scan_id             TEXT NOT NULL REFERENCES supply_chain_scans(scan_id) ON DELETE CASCADE,
    policy_version      TEXT NOT NULL DEFAULT '1.0',
    result              TEXT NOT NULL,
    reason              TEXT NOT NULL,
    critical_count      INTEGER DEFAULT 0,
    high_count          INTEGER DEFAULT 0,
    medium_count        INTEGER DEFAULT 0,
    low_count           INTEGER DEFAULT 0,
    evaluated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Indexes for efficient graph traversal
CREATE INDEX IF NOT EXISTS idx_asset_images_asset ON supply_chain_asset_images(asset_id);
CREATE INDEX IF NOT EXISTS idx_sboms_image ON supply_chain_sboms(image_id);
CREATE INDEX IF NOT EXISTS idx_vulnerabilities_image ON supply_chain_vulnerabilities(image_id);
CREATE INDEX IF NOT EXISTS idx_vulnerabilities_package ON supply_chain_vulnerabilities(package_id);
CREATE INDEX IF NOT EXISTS idx_policy_decisions_image ON supply_chain_policy_decisions(image_id);
