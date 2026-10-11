// Package models defines Phase 6.2 data structures.
// Risk fields (static_risk, dynamic_risk, combined_risk, confidence) are present as
// nullable integers for schema compatibility but are never computed in Phase 6.2.
// They will be populated by Phase 6.3.
package models

import (
	"encoding/json"
	"time"
)

// IncidentStatus represents the lifecycle state of an incident.
type IncidentStatus string

const (
	StatusOpen     IncidentStatus = "OPEN"
	StatusActive   IncidentStatus = "ACTIVE"
	StatusResolved IncidentStatus = "RESOLVED"
	StatusClosed   IncidentStatus = "CLOSED"
)

// Incident is the Phase 6 top-level security scenario.
// It groups one or more Findings that share deterministic correlation criteria.
type Incident struct {
	IncidentID  string         `json:"incident_id"`
	Fingerprint string         `json:"fingerprint"`
	Status      IncidentStatus `json:"status"`
	AssetID     string         `json:"asset_id,omitempty"`
	ContainerID string         `json:"container_id,omitempty"`
	ImageDigest string         `json:"image_digest,omitempty"`
	Severity    string         `json:"severity"`
	FindingCount int           `json:"finding_count"`

	// Phase 6.3 risk and attack chain data
	StaticRisk          *int       `json:"static_risk,omitempty"`
	DynamicRisk         *int       `json:"dynamic_risk,omitempty"`
	CombinedRisk        *int       `json:"combined_risk,omitempty"`
	Confidence          *int       `json:"confidence,omitempty"`
	RiskModelVersion    *string    `json:"risk_model_version,omitempty"`
	RiskLevel           *string    `json:"risk_level,omitempty"`
	CalculatedAt        *time.Time `json:"calculated_at,omitempty"`
	AttackChainState    *string    `json:"attack_chain_state,omitempty"`
	AttackChainEvidence []byte     `json:"attack_chain_evidence,omitempty"` // JSONB payload of evidence

	FirstSeen  time.Time  `json:"first_seen"`
	LastSeen   time.Time  `json:"last_seen"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// FindingRef is the wire format of the finding metadata that the Detection Engine
// publishes on zeraxis.events.detection.finding.open and .updated.
// NOTE: Detection Engine publishes a raw FindingEvent struct (not a full events.Event
// envelope). This is the actual schema we must parse (confirmed from detection/main.go line 268).
type FindingRef struct {
	FindingID         string `json:"finding_id"`
	Status            string `json:"status"`
	RuleID            string `json:"rule_id"`
	Severity          string `json:"severity"`
	AssetID           string `json:"asset_id,omitempty"`
	ImageDigest       string `json:"image_digest,omitempty"`
	ContainerID       string `json:"container_id,omitempty"`
	TriggeringEventID string `json:"triggering_event_id"`
}

// IncidentFinding links an incident to a finding.
type IncidentFinding struct {
	IncidentID string    `json:"incident_id"`
	FindingID  string    `json:"finding_id"`
	LinkedAt   time.Time `json:"linked_at"`
}

// maxSeverity returns the higher of two severity strings.
// Order: INFO < LOW < MEDIUM < HIGH < CRITICAL
func MaxSeverity(a, b string) string {
	order := map[string]int{
		"INFO": 0, "LOW": 1, "MEDIUM": 2, "HIGH": 3, "CRITICAL": 4,
	}
	if order[b] > order[a] {
		return b
	}
	return a
}

// Phase 6.4 Recommendation Model
type Recommendation struct {
	RecommendationID  string          `json:"recommendation_id"`
	IncidentID        string          `json:"incident_id"`
	Action            string          `json:"action"`
	TargetType        *string         `json:"target_type,omitempty"`
	TargetID          *string         `json:"target_id,omitempty"`
	TargetAssetID     *string         `json:"target_asset_id,omitempty"`
	TargetContainerID *string         `json:"target_container_id,omitempty"`
	TargetImageDigest *string         `json:"target_image_digest,omitempty"`
	RecoveryForExecutionID *string    `json:"recovery_for_execution_id,omitempty"`
	Priority          string          `json:"priority"`
	Confidence        *int            `json:"confidence,omitempty"`
	Reason            string          `json:"reason"`
	Evidence          json.RawMessage `json:"evidence"`
	ModelVersion      string          `json:"model_version"`
	Status            string          `json:"status"` // PROPOSED, ACKNOWLEDGED, DISMISSED
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
}
