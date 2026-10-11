CREATE TABLE k8s_policy_snapshots (
    snapshot_id UUID PRIMARY KEY,
    policy_id UUID NOT NULL,
    policy_version INT NOT NULL,
    rego_text TEXT NOT NULL,
    checksum TEXT NOT NULL,
    rollout_status TEXT NOT NULL, -- STAGING, CANARY_10, CANARY_50, GENERAL_AVAILABILITY, ROLLED_BACK
    rollout_started_at TIMESTAMPTZ,
    rollout_completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    FOREIGN KEY (policy_id) REFERENCES policy_definitions(policy_id)
);

CREATE TABLE k8s_cluster_policy_state (
    state_id UUID PRIMARY KEY,
    cluster_id TEXT NOT NULL UNIQUE,
    current_snapshot_id UUID NOT NULL,
    previous_snapshot_id UUID,
    applied_at TIMESTAMPTZ DEFAULT NOW(),
    FOREIGN KEY (current_snapshot_id) REFERENCES k8s_policy_snapshots(snapshot_id),
    FOREIGN KEY (previous_snapshot_id) REFERENCES k8s_policy_snapshots(snapshot_id)
);

CREATE TABLE k8s_webhook_registrations (
    registration_id UUID PRIMARY KEY,
    cluster_id TEXT NOT NULL,
    webhook_name TEXT NOT NULL,
    service_namespace TEXT NOT NULL,
    service_name TEXT NOT NULL,
    failure_policy TEXT NOT NULL, -- Fail (fail-closed), Ignore (fail-open)
    timeout_seconds INT NOT NULL DEFAULT 2,
    registered_at TIMESTAMPTZ DEFAULT NOW(),
    last_heartbeat_at TIMESTAMPTZ
);
