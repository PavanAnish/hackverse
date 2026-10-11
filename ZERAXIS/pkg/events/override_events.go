package events

import "time"

type OverrideRequestedEvent struct {
    RequestID         string
    Fingerprint       string
    RequestedBy       string
    Reason            string
    ClusterID         string
    ApprovalThreshold int
    Timestamp         time.Time
}

type OverrideApprovedEvent struct {
    OverrideRequestID string
    ApproverID        string
    Timestamp         time.Time
}

type OverrideExecutedEvent struct {
    OverrideRequestID string
    Fingerprint       string
    ClusterID         string
    Timestamp         time.Time
}

type PolicyAbuseIncidentEvent struct {
    UserID        string
    OverrideCount int
    TimeWindow    string // e.g., "1h"
    Timestamp     time.Time
}

type PolicyDistributionEvent struct {
    SnapshotID     string
    PolicyVersion  int
    RolloutStatus  string // STAGING, CANARY_10, CANARY_50, GENERAL_AVAILABILITY
    TargetClusters []string
    Timestamp      time.Time
}
