package models

import (
	"time"
)

// Rule Definition
type Rule struct {
	RuleID             string         `json:"rule_id" yaml:"rule_id"`
	RuleVersion        int            `json:"rule_version" yaml:"rule_version"`
	Name               string         `json:"name" yaml:"name"`
	Description        string         `json:"description" yaml:"description"`
	Severity           string         `json:"severity" yaml:"severity"`
	Confidence         string         `json:"confidence" yaml:"confidence"`
	Enabled            bool           `json:"enabled" yaml:"enabled"`
	
	// Stage 2A fields
	RequiredEventTypes []string       `json:"required_event_types,omitempty" yaml:"required_event_types,omitempty"`
	Conditions         string         `json:"conditions,omitempty" yaml:"conditions,omitempty"`
	GroupByFields      []string       `json:"group_by_fields,omitempty" yaml:"group_by_fields,omitempty"`

	// Stage 2B Temporal fields
	TimeWindow         time.Duration  `json:"time_window,omitempty" yaml:"time_window,omitempty"`
	Correlation        Correlation    `json:"correlation,omitempty" yaml:"correlation,omitempty"`
	Sequence           []SequenceStep `json:"sequence,omitempty" yaml:"sequence,omitempty"`
}

// Stage 2B specific types
type Correlation struct {
	Fields []string `json:"fields" yaml:"fields"`
}

type SequenceStep struct {
	Name       string `json:"name" yaml:"name"`
	EventType  string `json:"event_type" yaml:"event_type"`
	Condition  string `json:"condition" yaml:"condition"`
}

// Finding Model
type Finding struct {
	FindingID   string    `json:"finding_id"`
	Fingerprint string    `json:"fingerprint"`
	RuleID      string    `json:"rule_id"`
	RuleVersion int       `json:"rule_version"`
	Severity    string    `json:"severity"`
	Confidence  string    `json:"confidence"`
	Status      string    `json:"status"` // OPEN, UPDATED, RESOLVED
	FirstSeen   time.Time `json:"first_seen"`
	LastSeen    time.Time `json:"last_seen"`
	AssetID     string    `json:"asset_id,omitempty"`
	ImageDigest string    `json:"image_digest,omitempty"`
	Evidence    string    `json:"evidence"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// FindingEvent Model (published over NATS)
type FindingEvent struct {
	FindingID         string `json:"finding_id"`
	Status            string `json:"status"`
	RuleID            string `json:"rule_id"`
	Severity          string `json:"severity"`
	AssetID           string `json:"asset_id,omitempty"`
	ImageDigest       string `json:"image_digest,omitempty"`
	ContainerID       string `json:"container_id,omitempty"`
	TriggeringEventID string `json:"triggering_event_id"`
}
