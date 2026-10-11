package repository

import (
	"context"
	"encoding/json"
	"sort"
	"time"
)

type TimelineEvent struct {
	Timestamp   time.Time   `json:"timestamp"`
	EventType   string      `json:"event_type"`
	EventID     string      `json:"event_id,omitempty"`
	FindingID   string      `json:"finding_id,omitempty"`
	IncidentID  string      `json:"incident_id,omitempty"`
	Severity    string      `json:"severity,omitempty"`
	Details     interface{} `json:"details,omitempty"`
}

func (r *Repository) GetIncidentTimeline(ctx context.Context, incidentID string) ([]TimelineEvent, error) {
	var timeline []TimelineEvent

	// 1. Get Incident creation
	queryInc := `SELECT created_at, status, severity, risk_level, confidence, attack_chain_state FROM incidents WHERE incident_id = $1`
	var createdAt time.Time
	var status, severity string
	var riskLevel, attackChainState *string
	var confidence *int
	err := r.db.QueryRowContext(ctx, queryInc, incidentID).Scan(&createdAt, &status, &severity, &riskLevel, &confidence, &attackChainState)
	if err != nil {
		return nil, err
	}

	timeline = append(timeline, TimelineEvent{
		Timestamp:  createdAt,
		EventType:  "INCIDENT_CREATED",
		IncidentID: incidentID,
		Severity:   severity,
		Details: map[string]interface{}{
			"status": status,
		},
	})

	// 2. Get Findings and their underlying events
	queryFindings := `
		SELECT f.finding_id, f.created_at, f.rule_id, f.severity, i.linked_at
		FROM incident_findings i
		JOIN findings f ON f.finding_id = i.finding_id
		WHERE i.incident_id = $1
	`
	rowsF, err := r.db.QueryContext(ctx, queryFindings, incidentID)
	if err == nil {
		defer rowsF.Close()
		for rowsF.Next() {
			var fID, ruleID, fSev string
			var fCreated, linkedAt time.Time
			if err := rowsF.Scan(&fID, &fCreated, &ruleID, &fSev, &linkedAt); err == nil {
				timeline = append(timeline, TimelineEvent{
					Timestamp:  fCreated,
					EventType:  "FINDING_CREATED",
					FindingID:  fID,
					Severity:   fSev,
					Details: map[string]string{"rule_id": ruleID},
				})

				// Get associated events for this finding
				queryEvents := `
					SELECT e.event_id, e.event_type, e.timestamp, e.severity, e.metadata
					FROM finding_events fe
					JOIN events e ON e.event_id = fe.event_id
					WHERE fe.finding_id = $1
				`
				rowsE, errE := r.db.QueryContext(ctx, queryEvents, fID)
				if errE == nil {
					for rowsE.Next() {
						var eID, eType string
						var eTS time.Time
						var eSev *string
						var eMeta []byte
						if err := rowsE.Scan(&eID, &eType, &eTS, &eSev, &eMeta); err == nil {
							var sev string
							if eSev != nil {
								sev = *eSev
							}
							var meta map[string]interface{}
							_ = json.Unmarshal(eMeta, &meta)
							
							timeline = append(timeline, TimelineEvent{
								Timestamp: eTS,
								EventType: "RUNTIME_EVENT",
								EventID:   eID,
								Severity:  sev,
								Details: map[string]interface{}{
									"original_type": eType,
									"metadata": meta,
								},
							})
						}
					}
					rowsE.Close()
				}
			}
		}
	}

	// 3. Get Recommendations
	queryRecs := `
		SELECT recommendation_id, action, created_at, priority, confidence, status
		FROM incident_recommendations
		WHERE incident_id = $1
	`
	rowsR, err := r.db.QueryContext(ctx, queryRecs, incidentID)
	if err == nil {
		defer rowsR.Close()
		for rowsR.Next() {
			var rID, rAction, rPriority, rStatus string
			var rConf *int
			var rCreated time.Time
			if err := rowsR.Scan(&rID, &rAction, &rCreated, &rPriority, &rConf, &rStatus); err == nil {
				timeline = append(timeline, TimelineEvent{
					Timestamp:  rCreated,
					EventType:  "RECOMMENDATION_CREATED",
					IncidentID: incidentID,
					Severity:   rPriority,
					Details: map[string]interface{}{
						"recommendation_id": rID,
						"action": rAction,
						"confidence": rConf,
						"status": rStatus,
					},
				})
			}
		}
	}

	// 4. Sort the timeline by timestamp (ascending)
	sort.Slice(timeline, func(i, j int) bool {
		return timeline[i].Timestamp.Before(timeline[j].Timestamp)
	})

	return timeline, nil
}
