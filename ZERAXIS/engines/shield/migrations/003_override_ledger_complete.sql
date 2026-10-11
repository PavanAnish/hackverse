CREATE TABLE override_requests (
    request_id UUID PRIMARY KEY,
    fingerprint TEXT NOT NULL,
    requested_by UUID NOT NULL,
    reason TEXT NOT NULL,
    cluster_id TEXT NOT NULL,
    status TEXT NOT NULL, -- PENDING, APPROVED, REJECTED, EXPIRED, EXECUTED
    approval_threshold INT NOT NULL, -- e.g., 2
    approval_count INT DEFAULT 0,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    expires_at TIMESTAMPTZ DEFAULT NOW() + INTERVAL '1 hour',
    executed_at TIMESTAMPTZ
);

CREATE TABLE override_approvals (
    approval_id UUID PRIMARY KEY,
    override_request_id UUID NOT NULL,
    approver_id UUID NOT NULL,
    signature_hmac TEXT NOT NULL, -- HMAC-SHA256(request_id || approver_id || timestamp || secret)
    signature_timestamp TIMESTAMPTZ DEFAULT NOW(),
    can_revoke BOOLEAN DEFAULT TRUE,
    revoked_at TIMESTAMPTZ,
    FOREIGN KEY (override_request_id) REFERENCES override_requests(request_id),
    CONSTRAINT append_only CHECK (true)
);

-- Trigger function: raises an exception to enforce append-only semantics.
-- This function is referenced by both override_approvals and policy_audit_trail triggers.
-- Exception: override_approvals allows revoked_at to be set once (NULL → timestamp).
CREATE OR REPLACE FUNCTION raise_immutable_error() RETURNS trigger AS $$
BEGIN
    -- Allow the single permitted lifecycle update on override_approvals only:
    -- an approver may revoke their signature (revoked_at NULL → timestamp) before threshold.
    IF TG_TABLE_NAME = 'override_approvals' THEN
        IF OLD.revoked_at IS NULL AND NEW.revoked_at IS NOT NULL THEN
            RETURN NEW;
        END IF;
    END IF;
    RAISE EXCEPTION 'This table is append-only. Updates and deletes are prohibited. Table: %', TG_TABLE_NAME;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER prevent_override_approvals_modification
BEFORE UPDATE OR DELETE ON override_approvals
FOR EACH ROW EXECUTE FUNCTION raise_immutable_error();

CREATE TABLE policy_audit_trail (
    entry_id UUID PRIMARY KEY,
    event_type TEXT NOT NULL, -- OVERRIDE_GRANTED, ELEVATION_TRIGGERED, POLICY_ABUSE_INCIDENT, REVOCATION
    actor_id UUID NOT NULL,
    cluster_id TEXT NOT NULL,
    details JSONB,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    CONSTRAINT audit_immutable CHECK (true)
);

CREATE TRIGGER prevent_policy_audit_modification
BEFORE UPDATE OR DELETE ON policy_audit_trail
FOR EACH ROW EXECUTE FUNCTION raise_immutable_error();
