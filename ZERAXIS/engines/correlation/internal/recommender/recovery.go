package recommender

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pavanvanish/zeraxis/engines/correlation/internal/models"
	"github.com/pavanvanish/zeraxis/engines/correlation/pkg/constants"
)

// FailedExecutionEvidence represents the context passed from the event to the recommender.
type FailedExecutionEvidence struct {
	ExecutionID       string
	IncidentID        string
	TargetAssetID     string
	TargetContainerID string
	TargetImageDigest string
	ErrorLog          string // The error_log from execution_results
}

// GenerateRecoveryRecommendation evaluates a failed execution and deterministically
// returns a PROPOSED recovery recommendation, or nil if no recovery applies.
func GenerateRecoveryRecommendation(inc *models.Incident, ev FailedExecutionEvidence) *models.Recommendation {
	// 1. Validate Incident Status
	if inc.Status != models.StatusOpen && inc.Status != models.StatusActive {
		return nil
	}

	// 2. Classify Failure & Map to Action
	action := mapFailureToAction(ev.ErrorLog)
	if action == constants.ActionNoRecovery {
		return nil
	}

	// 3. Build Confidence
	// Confidence is fixed deterministically for system-generated recovery proposals
	// to flag it strongly for human review. It does NOT inherit previous approval weight.
	confidence := 95 

	now := time.Now().UTC()
	reason := fmt.Sprintf("Recovery recommendation for failed execution %s. Original error: %s", ev.ExecutionID, ev.ErrorLog)

	evidenceBytes, _ := json.Marshal(map[string]string{
		"failed_execution_id": ev.ExecutionID,
		"error_log":           ev.ErrorLog,
	})

	tt := "container"
	tid := ev.TargetContainerID

	var tAsset, tContainer, tDigest *string
	if ev.TargetAssetID != "" {
		a := ev.TargetAssetID
		tAsset = &a
	}
	if ev.TargetContainerID != "" {
		c := ev.TargetContainerID
		tContainer = &c
	}
	if ev.TargetImageDigest != "" {
		d := ev.TargetImageDigest
		tDigest = &d
	}

	recExecID := ev.ExecutionID

	rec := &models.Recommendation{
		RecommendationID:       uuid.New().String(),
		IncidentID:             inc.IncidentID,
		Action:                 action,
		TargetType:             &tt,
		TargetID:               &tid,
		TargetAssetID:          tAsset,
		TargetContainerID:      tContainer,
		TargetImageDigest:      tDigest,
		RecoveryForExecutionID: &recExecID,
		Priority:               "HIGH", // Elevated priority for recovery
		Confidence:             &confidence,
		Reason:                 reason,
		Evidence:               json.RawMessage(evidenceBytes),
		ModelVersion:           ModelVersion,
		Status:                 "PROPOSED",
		CreatedAt:              now,
		UpdatedAt:              now,
	}

	return rec
}

// ComputeRecoveryFingerprint returns the strict duplicate prevention fingerprint.
func ComputeRecoveryFingerprint(failedExecutionID, action string) string {
	return fmt.Sprintf("recovery:%s:%s", failedExecutionID, action)
}

// mapFailureToAction uses deterministic string prefix matching against the execution error log
// to classify the failure and return the required recovery action.
func mapFailureToAction(errorLog string) string {
	// DOCKER_UNAVAILABLE logic
	if strings.Contains(errorLog, constants.PrefixDockerUnavailable) || strings.Contains(errorLog, constants.PrefixDockerConnection) {
		return constants.ActionNoRecovery
	}

	// TARGET_MISSING logic
	if strings.Contains(errorLog, constants.PrefixTargetMissing) || strings.Contains(errorLog, "container missing in docker") {
		return constants.ActionNoRecovery
	}

	// Some general Docker inspect generic errors that do not specify container missing or daemon down.
	// We map them to NO_RECOVERY as unrecognized/unknown Docker inspect failures.
	if strings.Contains(errorLog, constants.PrefixDockerInspectGeneric) {
		return constants.ActionNoRecovery
	}

	if strings.Contains(errorLog, constants.PrefixTargetIdentityMismatch) {
		return constants.ActionQuarantineAsset
	}
	if strings.Contains(errorLog, constants.PrefixImageDigestMismatch) {
		return constants.ActionQuarantineAsset
	}
	if strings.Contains(errorLog, constants.PrefixPostconditionNotMet) {
		return constants.ActionInvestigateProcess
	}

	// MUTATION_FAILED explicitly detected by standard Docker errors outside of inspect,
	// or specific mutation-phase errors we can deterministically identify.
	// We will treat ANY unknown/unrecognized error as NO_RECOVERY per the prompt instruction.
	// But we need to ensure known mutation errors map to INVESTIGATE_PROCESS.
	// Let's identify MUTATION_FAILED deterministically.
	// For instance, Phase 7.3 Executor mock uses "Mutation error: " for ContainerStop errors
	// (Actually, wait, executor.go emits "Mutation error: %s" for stop failures).
	if strings.Contains(errorLog, "Mutation error:") || strings.Contains(errorLog, "docker daemon returned error") {
		return constants.ActionInvestigateProcess
	}

	// Fallback: Unknown failure -> fail closed -> NO_RECOVERY.
	return constants.ActionNoRecovery
}
