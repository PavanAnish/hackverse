package events

import "time"

type AdmissionRequestedEvent struct {
    RequestID string
    Fingerprint string
    AssetMetadata map[string]interface{}
    PolicyVersion string
    Timestamp time.Time
}

type AdmissionBlockedEvent struct {
    RequestID string
    Fingerprint string
    BlockReason string
    PolicyViolations []string
    Timestamp time.Time
}

type AdmissionAllowedEvent struct {
    RequestID string
    Fingerprint string
    Timestamp time.Time
}

type AdmissionOverrideGrantedEvent struct {
    OverrideID string
    AdmissionRequestID string
    Approvers []string
    Reason string
    Timestamp time.Time
}
