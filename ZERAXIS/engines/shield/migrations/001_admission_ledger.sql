CREATE TABLE policy_definitions (
    policy_id UUID PRIMARY KEY,
    rego_text TEXT NOT NULL,
    checksum TEXT NOT NULL UNIQUE,
    policy_status TEXT NOT NULL, -- DRAFT, STAGING, ACTIVE, DEPRECATED, RETIRED
    version_number INT NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    activated_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX one_active_policy ON policy_definitions(policy_status) WHERE policy_status = 'ACTIVE';

CREATE TABLE admission_requests (
    request_id UUID PRIMARY KEY,
    fingerprint TEXT NOT NULL,
    asset_metadata JSONB,
    policy_version UUID NOT NULL,
    status TEXT NOT NULL, -- ALLOWED, BLOCKED, OVERRIDDEN
    decision_reason TEXT,
    latency_ms INT,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    FOREIGN KEY (policy_version) REFERENCES policy_definitions(policy_id)
);

CREATE TABLE override_ledger (
    override_id UUID PRIMARY KEY,
    admission_request_id UUID NOT NULL,
    approver_ids TEXT[] NOT NULL, -- M-of-N signatures
    approval_threshold INT NOT NULL, -- e.g., 2 of 3
    granted_at TIMESTAMPTZ DEFAULT NOW(),
    reason TEXT NOT NULL,
    FOREIGN KEY (admission_request_id) REFERENCES admission_requests(request_id)
);

-- Assuming raise_immutable_error() is defined from Phase 7.5. If not, it will be added during testing.
CREATE TRIGGER prevent_override_ledger_modification
BEFORE UPDATE OR DELETE ON override_ledger
FOR EACH ROW EXECUTE FUNCTION raise_immutable_error();
