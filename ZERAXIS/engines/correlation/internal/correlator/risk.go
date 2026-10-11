package correlator

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/pavanvanish/zeraxis/engines/correlation/internal/models"
	"github.com/pavanvanish/zeraxis/engines/correlation/internal/recommender"
	"github.com/pavanvanish/zeraxis/engines/correlation/internal/risk"
	pkgevents "github.com/pavanvanish/zeraxis/pkg/events"
)

// RecalculateIncidentRisk fetches all required context for an incident, calculates the risk,
// updates the incident object in-memory, and persists it to PostgreSQL.
func (c *Correlator) RecalculateIncidentRisk(ctx context.Context, inc *models.Incident, calcTime time.Time) error {
	// Fetch all linked findings (this ensures we evaluate the attack chain properly)
	findings, err := c.repo.GetIncidentFindingRefs(ctx, inc.IncidentID)
	if err != nil {
		return fmt.Errorf("fetch findings for risk: %w", err)
	}

	// Fetch supply chain risk state (Phase 3)
	supplyState, err := c.repo.GetSupplyChainRiskState(ctx, inc.ImageDigest)
	if err != nil {
		return fmt.Errorf("fetch supply chain state: %w", err)
	}

	// Calculate deterministic risk
	risk.CalculateRisk(inc, supplyState.MaxCVESeverity, supplyState.PolicyResult, calcTime, findings)

	// Persist the updated risk profile
	if err := c.repo.UpdateIncidentRisk(ctx, inc); err != nil {
		return fmt.Errorf("persist incident risk: %w", err)
	}

	// Phase 6.4: Generate Recommendations
	recs := recommender.GenerateRecommendations(inc)
	if len(recs) == 0 {
		c.m.inc(&c.m.recsSuppressed)
	} else {
		for range recs {
			c.m.inc(&c.m.recsGenerated)
		}
	}
	
	for _, rec := range recs {
		fp := recommender.ComputeFingerprint(&rec)
		
		savedRec, isNew, err := c.repo.UpsertRecommendation(ctx, &rec, fp)
		if err != nil {
			slog.Error("Failed to upsert recommendation", "incident_id", inc.IncidentID, "error", err)
			continue
		}

		// Publish NATS event AFTER successful durable persistence
		evtType := pkgevents.EventTypeRecommendationUpdated
		if isNew {
			evtType = pkgevents.EventTypeRecommendationCreated
		}
		c.publishRecommendationEvent(ctx, evtType, savedRec)
	}

	return nil
}

func (c *Correlator) publishRecommendationEvent(ctx context.Context, eventType string, rec *models.Recommendation) {
	// Re-use Unified Event Envelope
	evt := pkgevents.NewEvent(eventType, pkgevents.SourceCorrelation, time.Now().UTC())
	evt.Severity = rec.Priority
	
	// Must set subject
	// Subject: zeraxis.events.correlation.recommendation.created OR .updated
	action := "created"
	if eventType == pkgevents.EventTypeRecommendationUpdated {
		action = "updated"
	}
	evt.Subject = fmt.Sprintf("zeraxis.events.correlation.recommendation.%s", action)

	// Pack Recommendation ID into CorrelationID pointer if desired, or into metadata
	meta := map[string]interface{}{
		"recommendation_id": rec.RecommendationID,
		"incident_id":       rec.IncidentID,
		"action":            rec.Action,
		"priority":          rec.Priority,
		"status":            rec.Status,
	}
	if rec.TargetID != nil {
		meta["target_id"] = *rec.TargetID
	}
	if rec.TargetType != nil {
		meta["target_type"] = *rec.TargetType
	}
	
	metaBytes, _ := json.Marshal(meta)
	evt.Metadata = metaBytes

	if err := c.publisher.Publish(ctx, evt); err != nil {
		slog.Warn("Failed to publish recommendation event to NATS",
			"recommendation_id", rec.RecommendationID,
			"event_type", eventType,
			"error", err,
		)
	}
}
