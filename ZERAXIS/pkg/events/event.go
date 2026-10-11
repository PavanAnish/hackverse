package events

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Event is the canonical envelope for all ZERAXIS events.
type Event struct {
	EventID            string          `json:"event_id"`
	EventType          string          `json:"event_type"`
	EventVersion       int             `json:"event_version"`
	Timestamp          time.Time       `json:"timestamp"`
	IngestionTimestamp *time.Time      `json:"ingestion_timestamp,omitempty"` // Set by PostgreSQL
	Source             string          `json:"source"`
	AssetID            string          `json:"asset_id,omitempty"`
	ImageDigest        string          `json:"image_digest,omitempty"`
	Severity           string          `json:"severity,omitempty"`
	CorrelationID      *string         `json:"correlation_id,omitempty"`
	TraceID            *string         `json:"trace_id,omitempty"`
	Metadata           json.RawMessage `json:"metadata"`
	Subject            string          `json:"subject"`
}

// NewEvent creates a new base event.
func NewEvent(eventType, source string, timestamp time.Time) *Event {
	return &Event{
		EventID:      uuid.New().String(),
		EventType:    eventType,
		EventVersion: 1,
		Timestamp:    timestamp,
		Source:       source,
	}
}

// Validate ensures the event meets the canonical schema requirements.
func (e *Event) Validate() error {
	if e.EventID == "" {
		return fmt.Errorf("event_id is required")
	}
	if _, err := uuid.Parse(e.EventID); err != nil {
		return fmt.Errorf("event_id must be a valid UUID")
	}
	if e.EventType == "" {
		return fmt.Errorf("event_type is required")
	}
	if e.EventVersion < 1 {
		return fmt.Errorf("event_version must be >= 1")
	}
	if e.Timestamp.IsZero() {
		return fmt.Errorf("timestamp is required")
	}
	if e.Source == "" {
		return fmt.Errorf("source is required")
	}
	if e.AssetID != "" {
		if _, err := uuid.Parse(e.AssetID); err != nil {
			return fmt.Errorf("asset_id must be a valid UUID")
		}
	}
	if e.CorrelationID != nil && *e.CorrelationID != "" {
		if _, err := uuid.Parse(*e.CorrelationID); err != nil {
			return fmt.Errorf("correlation_id must be a valid UUID")
		}
	}
	if e.Subject == "" {
		return fmt.Errorf("subject is required")
	}
	return nil
}
