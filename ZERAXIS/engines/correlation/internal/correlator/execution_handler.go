package correlator

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/pavanvanish/zeraxis/engines/correlation/internal/models"
	"github.com/pavanvanish/zeraxis/engines/correlation/internal/recommender"
	pkgevents "github.com/pavanvanish/zeraxis/pkg/events"
)

// HandleExecutionAudit processes execution/verification events to potentially generate recovery recommendations.
func (c *Correlator) HandleExecutionAudit(ctx context.Context, data []byte) {
	var ev pkgevents.Event
	if err := json.Unmarshal(data, &ev); err != nil {
		slog.Error("HandleExecutionAudit: decode error", "error", err)
		return
	}

	var meta map[string]interface{}
	if len(ev.Metadata) > 0 {
		_ = json.Unmarshal(ev.Metadata, &meta)
	}

	execID, _ := meta["execution_id"].(string)
	if execID == "" {
		// Attempt to extract from details (Executor output format: "Execution <id> ...")
		if details, ok := meta["details"].(string); ok {
			var parsed string
			_, err := fmt.Sscanf(details, "Execution %s", &parsed)
			if err == nil && parsed != "" {
				if _, err := uuid.Parse(parsed); err == nil {
					execID = parsed
				} else {
					slog.Warn("HandleExecutionAudit: rejected malformed UUID from adversarial payload", "parsed", parsed)
				}
			}
		}
	}

	if execID == "" {
		slog.Error("HandleExecutionAudit: missing execution_id in metadata", "eventID", ev.EventID)
		return
	}

	if ev.EventType == "EXECUTION_VERIFIED" {
		slog.Info("HandleExecutionAudit: evaluating verified execution for resolution", "execution_id", execID)
		
		resolvedIncidentID, err := c.repo.ResolveIncidentFromExecution(ctx, execID)
		if err != nil {
			slog.Error("HandleExecutionAudit: failed to resolve incident", "error", err, "execution_id", execID)
			return
		}
		if resolvedIncidentID != "" {
			slog.Info("HandleExecutionAudit: successfully resolved incident from execution", "execution_id", execID, "incident_id", resolvedIncidentID)
			
			// Publish INCIDENT_RESOLVED
			outEvent := pkgevents.NewEvent(
				"INCIDENT_RESOLVED",
				"correlation-engine",
				time.Now().UTC(),
			)
			outEvent.Metadata = json.RawMessage(fmt.Sprintf(`{"incident_id":"%s", "execution_id":"%s"}`, resolvedIncidentID, execID))
			outEvent.Subject = "zeraxis.events.correlation.incident.resolved"
			
			if pubErr := c.publisher.Publish(ctx, outEvent); pubErr != nil {
				slog.Error("HandleExecutionAudit: failed to emit INCIDENT_RESOLVED", "error", pubErr)
			}
		}
		return
	}

	if ev.EventType != "EXECUTION_FAILED" && ev.EventType != "EXECUTION_VERIFICATION_FAILED" {
		return
	}

	slog.Info("HandleExecutionAudit: evaluating failed execution", "execution_id", execID, "event_type", ev.EventType)

	// Fetch authoritative state from DB
	execRec, err := c.repo.GetExecutionRecord(ctx, execID)
	if err != nil {
		slog.Error("HandleExecutionAudit: failed to fetch authoritative execution record", "error", err)
		return
	}

	// Incident status check is also performed in recommender, but we double-check here just in case.
	inc := &models.Incident{
		IncidentID: execRec.IncidentID,
		Status:     models.IncidentStatus(execRec.IncidentStatus),
	}

	// Construct evidence wrapper
	evidence := recommender.FailedExecutionEvidence{
		ExecutionID:       execRec.ExecutionID,
		IncidentID:        execRec.IncidentID,
		TargetAssetID:     execRec.TargetAssetID,
		TargetContainerID: execRec.TargetContainerID,
		TargetImageDigest: execRec.TargetImageDigest,
		ErrorLog:          execRec.ErrorLog, // For EXECUTION_FAILED
	}

	
	// Phase 7.7: If the original recommendation is quarantined for manual investigation (because of abandonment),
	// DO NOT generate an autonomous recovery recommendation.
	if execRec.RecommendationStatus == "MANUAL_INVESTIGATION_REQUIRED" {
		slog.Warn("HandleExecutionAudit: aborting recovery, parent recommendation is quarantined (MANUAL_INVESTIGATION_REQUIRED)", "execution_id", execID)
		return
	}

// Enforce max depth = 1: if the failed execution was spawned by a recovery rec, abort.
	if execRec.RecoveryForExecutionID != nil {
		slog.Warn("HandleExecutionAudit: aborting recovery, max depth reached (parent was already a recovery attempt)", "execution_id", execID)
		return
	}

	rec := recommender.GenerateRecoveryRecommendation(inc, evidence)
	if rec == nil {
		slog.Info("HandleExecutionAudit: no recovery generated (likely NO_RECOVERY mapped or incident closed)")
		return
	}

	fp := recommender.ComputeRecoveryFingerprint(execID, rec.Action)
	inserted, err := c.repo.InsertRecoveryRecommendation(ctx, rec, fp)
	if err != nil {
		slog.Error("HandleExecutionAudit: database error inserting recovery recommendation", "error", err)
		return
	}

	if !inserted {
		slog.Info("HandleExecutionAudit: duplicate recovery ignored (ON CONFLICT DO NOTHING)", "fingerprint", fp)
		return
	}

	slog.Info("HandleExecutionAudit: recovery recommendation successfully created", "recommendation_id", rec.RecommendationID)

	// Emit Phase 4 Event
	outEvent := pkgevents.NewEvent(
		"RECOVERY_RECOMMENDATION_CREATED",
		"correlation-engine",
		time.Now().UTC(),
	)
	outEvent.Metadata = json.RawMessage(fmt.Sprintf(`{"recommendation_id":"%s", "incident_id":"%s", "recovery_for_execution_id":"%s"}`, rec.RecommendationID, rec.IncidentID, execID))
	outEvent.Subject = "zeraxis.events.correlation.recovery"
	
	if err := c.publisher.Publish(ctx, outEvent); err != nil {
		slog.Error("HandleExecutionAudit: failed to emit RECOVERY_RECOMMENDATION_CREATED", "error", err)
		// Do not roll back DB. Persistence is authoritative.
	}
}
