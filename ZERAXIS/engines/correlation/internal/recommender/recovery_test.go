package recommender

import (
	"testing"

	"github.com/pavanvanish/zeraxis/engines/correlation/internal/models"
	"github.com/pavanvanish/zeraxis/engines/correlation/pkg/constants"
)

func TestMapFailureToAction(t *testing.T) {
	tests := []struct {
		name     string
		errorLog string
		want     string
	}{
		{
			name:     "POSTCONDITION_NOT_MET",
			errorLog: constants.PrefixPostconditionNotMet + " running is not terminal",
			want:     constants.ActionInvestigateProcess,
		},
		{
			name:     "MUTATION_FAILED",
			errorLog: "Mutation error: Docker daemon returned error 500",
			want:     constants.ActionInvestigateProcess,
		},
		{
			name:     "IMAGE_DIGEST_MISMATCH",
			errorLog: constants.PrefixImageDigestMismatch + " sha256:abc not found",
			want:     constants.ActionQuarantineAsset,
		},
		{
			name:     "TARGET_IDENTITY_MISMATCH",
			errorLog: constants.PrefixTargetIdentityMismatch + " got abc",
			want:     constants.ActionQuarantineAsset,
		},
		{
			name:     "TARGET_MISSING",
			errorLog: constants.PrefixTargetMissing + " No such container",
			want:     constants.ActionNoRecovery,
		},
		{
			name:     "DOCKER_UNAVAILABLE",
			errorLog: constants.PrefixDockerUnavailable + " connection refused",
			want:     constants.ActionNoRecovery,
		},
		{
			name:     "DENY from phase 7.2",
			errorLog: "DENY: authorization missing",
			want:     constants.ActionNoRecovery,
		},
		{
			name:     "Unknown failure",
			errorLog: "Something completely different",
			want:     constants.ActionNoRecovery, // Fail closed to NO_RECOVERY
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapFailureToAction(tt.errorLog)
			if got != tt.want {
				t.Errorf("mapFailureToAction(%q) = %v, want %v", tt.errorLog, got, tt.want)
			}
		})
	}
}

func TestGenerateRecoveryRecommendation(t *testing.T) {
	inc := &models.Incident{
		IncidentID: "inc-1",
		Status:     models.StatusOpen,
	}

	ev := FailedExecutionEvidence{
		ExecutionID:       "exec-1",
		IncidentID:        "inc-1",
		TargetAssetID:     "asset-1",
		TargetContainerID: "cont-1",
		TargetImageDigest: "sha256:abc",
		ErrorLog:          constants.PrefixImageDigestMismatch + " sha256:abc",
	}

	rec := GenerateRecoveryRecommendation(inc, ev)
	if rec == nil {
		t.Fatal("expected recommendation, got nil")
	}

	if rec.Action != constants.ActionQuarantineAsset {
		t.Errorf("expected QUARANTINE_ASSET, got %s", rec.Action)
	}
	if rec.Status != "PROPOSED" {
		t.Errorf("expected PROPOSED, got %s", rec.Status)
	}
	if *rec.RecoveryForExecutionID != "exec-1" {
		t.Errorf("expected exec-1, got %v", *rec.RecoveryForExecutionID)
	}
	if *rec.TargetAssetID != "asset-1" {
		t.Errorf("expected target asset to be preserved")
	}
	if *rec.Confidence != 95 {
		t.Errorf("expected deterministic confidence 95, got %d", *rec.Confidence)
	}

	// Test resolved incident
	incResolved := &models.Incident{Status: models.StatusResolved}
	if GenerateRecoveryRecommendation(incResolved, ev) != nil {
		t.Error("expected nil for resolved incident")
	}
}
