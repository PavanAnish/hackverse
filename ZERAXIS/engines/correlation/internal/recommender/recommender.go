package recommender

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/pavanvanish/zeraxis/engines/correlation/internal/models"
)

const ModelVersion = "1.0"

// GenerateRecommendations evaluates an incident and deterministically yields recommendations.
func GenerateRecommendations(inc *models.Incident) []models.Recommendation {
	var recs []models.Recommendation

	if inc.Status == models.StatusClosed || inc.RiskLevel == nil || inc.Confidence == nil {
		return recs
	}

	riskLvl := *inc.RiskLevel
	conf := *inc.Confidence

	// Only act if there's sufficient confidence
	if conf < 40 {
		return recs // Do not recommend disruptive actions for very low confidence
	}

	// Matrix evaluation
	switch riskLvl {
	case "CRITICAL":
		if conf >= 90 {
			if inc.AssetID != "" {
				recs = append(recs, buildRec(inc, "QUARANTINE_ASSET", "asset", inc.AssetID, "CRITICAL", "Critical risk with strong evidence warrants isolation."))
			}
			if inc.ImageDigest != "" {
				recs = append(recs, buildRec(inc, "REBUILD_IMAGE", "image", inc.ImageDigest, "CRITICAL", "Image is compromised and must be rebuilt."))
			}
		}
		recs = append(recs, buildRec(inc, "ESCALATE_INCIDENT", "incident", inc.IncidentID, "HIGH", "Critical risk requires human analyst escalation."))

	case "HIGH":
		if conf >= 70 {
			if inc.ImageDigest != "" {
				recs = append(recs, buildRec(inc, "REDEPLOY_CLEAN_IMAGE", "image", inc.ImageDigest, "HIGH", "High risk vulnerability execution detected, redeploy clean image."))
			}
			if inc.ContainerID != "" {
				recs = append(recs, buildRec(inc, "INVESTIGATE_PROCESS", "container", inc.ContainerID, "HIGH", "High risk anomalous process detected."))
			}
		} else {
			recs = append(recs, buildRec(inc, "REVIEW_VULNERABILITY", "incident", inc.IncidentID, "MEDIUM", "High risk but lower confidence, requires review."))
		}

	case "MEDIUM":
		recs = append(recs, buildRec(inc, "INVESTIGATE_PROCESS", "container", inc.ContainerID, "MEDIUM", "Anomalous behavior requires investigation."))
		recs = append(recs, buildRec(inc, "REVIEW_VULNERABILITY", "incident", inc.IncidentID, "LOW", "Medium risk findings require review."))

	case "LOW", "INFO":
		recs = append(recs, buildRec(inc, "REVIEW_VULNERABILITY", "incident", inc.IncidentID, "LOW", "Low risk anomaly detected."))
	}

	return recs
}

func buildRec(inc *models.Incident, action, targetType, targetID, priority, reason string) models.Recommendation {
	now := time.Now().UTC()
	
	// Create evidence payload from incident state
	ev := map[string]interface{}{
		"risk_level":         *inc.RiskLevel,
		"confidence":         *inc.Confidence,
		"attack_chain_state": "UNKNOWN",
	}
	if inc.AttackChainState != nil {
		ev["attack_chain_state"] = *inc.AttackChainState
	}
	if inc.ImageDigest != "" {
		ev["image_digest"] = inc.ImageDigest
	}
	evBytes, _ := json.Marshal(ev)

	tt := targetType
	tid := targetID

	var tAsset, tContainer, tDigest *string
	if inc.AssetID != "" {
		a := inc.AssetID
		tAsset = &a
	}
	if inc.ContainerID != "" {
		c := inc.ContainerID
		tContainer = &c
	}
	if inc.ImageDigest != "" {
		d := inc.ImageDigest
		tDigest = &d
	}

	return models.Recommendation{
		RecommendationID:  uuid.New().String(),
		IncidentID:        inc.IncidentID,
		Action:            action,
		TargetType:        &tt,
		TargetID:          &tid,
		TargetAssetID:     tAsset,
		TargetContainerID: tContainer,
		TargetImageDigest: tDigest,
		Priority:          priority,
		Confidence:        inc.Confidence,
		Reason:            reason,
		Evidence:          json.RawMessage(evBytes),
		ModelVersion:      ModelVersion,
		Status:            "PROPOSED",
		CreatedAt:         now,
		UpdatedAt:         now,
	}
}

// ComputeFingerprint returns the deterministic identity of a recommendation for an incident.
func ComputeFingerprint(rec *models.Recommendation) string {
	tt := ""
	if rec.TargetType != nil {
		tt = *rec.TargetType
	}
	tid := ""
	if rec.TargetID != nil {
		tid = *rec.TargetID
	}
	raw := fmt.Sprintf("%s:%s:%s:%s:%s", rec.IncidentID, rec.Action, tt, tid, rec.ModelVersion)
	h := sha256.Sum256([]byte(raw))
	return fmt.Sprintf("%x", h)
}

// ValidationResult represents the output of target identity validation.
type ValidationResult string

const (
	ValidationValid   ValidationResult = "VALID"
	ValidationDeny    ValidationResult = "DENY"
)

// ValidateExecutionTarget strictly validates a recommendation's identity against the current runtime state.
// This enforces the invariant: target_asset_id AND target_container_id AND target_image_digest must match.
func ValidateExecutionTarget(rec *models.Recommendation, currentAssetID, currentContainerID, currentImageDigest string) ValidationResult {
	if rec.TargetAssetID == nil || *rec.TargetAssetID == "" {
		return ValidationDeny
	}
	if rec.TargetContainerID == nil || *rec.TargetContainerID == "" {
		return ValidationDeny
	}
	if rec.TargetImageDigest == nil || *rec.TargetImageDigest == "" {
		return ValidationDeny
	}

	if *rec.TargetAssetID != currentAssetID {
		return ValidationDeny
	}
	if *rec.TargetContainerID != currentContainerID {
		return ValidationDeny
	}
	if *rec.TargetImageDigest != currentImageDigest {
		return ValidationDeny
	}

	return ValidationValid
}
