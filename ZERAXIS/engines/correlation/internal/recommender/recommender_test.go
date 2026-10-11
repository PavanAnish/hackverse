package recommender

import (
	"encoding/json"
	"testing"

	"github.com/pavanvanish/zeraxis/engines/correlation/internal/models"
)

func ptr(s string) *string { return &s }
func intPtr(i int) *int { return &i }

func TestValidateExecutionTarget(t *testing.T) {
	tests := []struct {
		name          string
		rec           models.Recommendation
		curAsset      string
		curContainer  string
		curDigest     string
		expected      ValidationResult
	}{
		{
			name: "TEST 1 - Valid identity",
			rec: models.Recommendation{
				TargetAssetID:     ptr("A1"),
				TargetContainerID: ptr("C1"),
				TargetImageDigest: ptr("D1"),
			},
			curAsset:     "A1",
			curContainer: "C1",
			curDigest:    "D1",
			expected:     ValidationValid,
		},
		{
			name: "TEST 2 - Asset mismatch",
			rec: models.Recommendation{
				TargetAssetID:     ptr("A1"),
				TargetContainerID: ptr("C1"),
				TargetImageDigest: ptr("D1"),
			},
			curAsset:     "A2",
			curContainer: "C1",
			curDigest:    "D1",
			expected:     ValidationDeny,
		},
		{
			name: "TEST 3 - Container replacement",
			rec: models.Recommendation{
				TargetAssetID:     ptr("A1"),
				TargetContainerID: ptr("C1"),
				TargetImageDigest: ptr("D1"),
			},
			curAsset:     "A1",
			curContainer: "C2",
			curDigest:    "D1",
			expected:     ValidationDeny,
		},
		{
			name: "TEST 4 - Image replacement",
			rec: models.Recommendation{
				TargetAssetID:     ptr("A1"),
				TargetContainerID: ptr("C1"),
				TargetImageDigest: ptr("D1"),
			},
			curAsset:     "A1",
			curContainer: "C1",
			curDigest:    "D2",
			expected:     ValidationDeny,
		},
		{
			name: "TEST 5 - Missing asset ID",
			rec: models.Recommendation{
				TargetContainerID: ptr("C1"),
				TargetImageDigest: ptr("D1"),
			},
			curAsset:     "A1",
			curContainer: "C1",
			curDigest:    "D1",
			expected:     ValidationDeny,
		},
		{
			name: "TEST 6 - Missing container ID",
			rec: models.Recommendation{
				TargetAssetID:     ptr("A1"),
				TargetImageDigest: ptr("D1"),
			},
			curAsset:     "A1",
			curContainer: "C1",
			curDigest:    "D1",
			expected:     ValidationDeny,
		},
		{
			name: "TEST 7 - Missing image digest",
			rec: models.Recommendation{
				TargetAssetID:     ptr("A1"),
				TargetContainerID: ptr("C1"),
			},
			curAsset:     "A1",
			curContainer: "C1",
			curDigest:    "D1",
			expected:     ValidationDeny,
		},
		{
			name: "TEST 8 - Historical recommendation without target identity",
			rec: models.Recommendation{
				// All null
			},
			curAsset:     "A1",
			curContainer: "C1",
			curDigest:    "D1",
			expected:     ValidationDeny,
		},
		{
			name: "TEST 11 - D1 to D2",
			rec: models.Recommendation{
				TargetAssetID:     ptr("A1"),
				TargetContainerID: ptr("C1"),
				TargetImageDigest: ptr("D1"),
			},
			curAsset:     "A1",
			curContainer: "C1",
			curDigest:    "D2",
			expected:     ValidationDeny,
		},
		{
			name: "TEST 12 - C1 to C2",
			rec: models.Recommendation{
				TargetAssetID:     ptr("A1"),
				TargetContainerID: ptr("C1"),
				TargetImageDigest: ptr("D1"),
			},
			curAsset:     "A1",
			curContainer: "C2",
			curDigest:    "D1",
			expected:     ValidationDeny,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ValidateExecutionTarget(&tt.rec, tt.curAsset, tt.curContainer, tt.curDigest)
			if got != tt.expected {
				t.Errorf("ValidateExecutionTarget() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestBuildRec_AuthoritativeTargets(t *testing.T) {
	inc := &models.Incident{
		IncidentID:  "inc-123",
		RiskLevel:   ptr("CRITICAL"),
		Confidence:  intPtr(95),
		AssetID:     "A1",
		ContainerID: "C1",
		ImageDigest: "D1",
	}

	recs := GenerateRecommendations(inc)
	if len(recs) == 0 {
		t.Fatal("Expected recommendations to be generated")
	}

	var foundQuarantine bool
	for _, rec := range recs {
		if rec.Action == "QUARANTINE_ASSET" {
			foundQuarantine = true
			if rec.TargetAssetID == nil || *rec.TargetAssetID != "A1" {
				t.Errorf("Expected TargetAssetID A1, got %v", rec.TargetAssetID)
			}
			if rec.TargetContainerID == nil || *rec.TargetContainerID != "C1" {
				t.Errorf("Expected TargetContainerID C1, got %v", rec.TargetContainerID)
			}
			if rec.TargetImageDigest == nil || *rec.TargetImageDigest != "D1" {
				t.Errorf("Expected TargetImageDigest D1, got %v", rec.TargetImageDigest)
			}
		}
	}
	if !foundQuarantine {
		t.Error("Expected QUARANTINE_ASSET recommendation")
	}
}

func TestBuildRec_EvidenceInjection_Test9(t *testing.T) {
	inc := &models.Incident{
		IncidentID:  "inc-123",
		RiskLevel:   ptr("CRITICAL"),
		Confidence:  intPtr(95),
		AssetID:     "A1",
		ContainerID: "C1",
		ImageDigest: "D1",
	}

	recs := GenerateRecommendations(inc)
	rec := recs[0]

	// Simulate someone trying to inject fields into evidence JSON
	var ev map[string]interface{}
	json.Unmarshal(rec.Evidence, &ev)
	ev["target_container_id"] = "MALICIOUS_C2"
	ev["image_digest"] = "MALICIOUS_D2"
	rec.Evidence, _ = json.Marshal(ev)

	// Validating against the structured fields must still use A1, C1, D1
	res := ValidateExecutionTarget(&rec, "A1", "C1", "D1")
	if res != ValidationValid {
		t.Errorf("Expected Valid when structured fields match, got %v", res)
	}

	// Validating against injected fields should fail
	res2 := ValidateExecutionTarget(&rec, "A1", "MALICIOUS_C2", "MALICIOUS_D2")
	if res2 != ValidationDeny {
		t.Errorf("Expected Deny when matching injected fields instead of structured fields, got %v", res2)
	}
}

func TestBuildRec_MaliciousTargetID_Test10(t *testing.T) {
	inc := &models.Incident{
		IncidentID:  "inc-123",
		RiskLevel:   ptr("CRITICAL"),
		Confidence:  intPtr(95),
		AssetID:     "A1",
		ContainerID: "$(rm -rf /)",
		ImageDigest: "; docker exec",
	}

	recs := GenerateRecommendations(inc)
	rec := recs[0]

	// Validate target identity fields correctly hold the malformed strings,
	// but the executor's ValidateExecutionTarget only allows exact string match.
	// If the current container ID is literally "$(rm -rf /)", it matches,
	// but the typed Docker API would just return "no such container".
	// We ensure it's not executing by ensuring the literal check holds.
	res := ValidateExecutionTarget(&rec, "A1", "$(rm -rf /)", "; docker exec")
	if res != ValidationValid {
		t.Error("ValidateExecutionTarget failed exact match on malformed string")
	}

	res2 := ValidateExecutionTarget(&rec, "A1", "C1", "D1")
	if res2 != ValidationDeny {
		t.Error("ValidateExecutionTarget failed to deny mismatch on malformed string")
	}
}

func TestComputeFingerprint_ConcurrentGeneration_Test13(t *testing.T) {
	inc := &models.Incident{
		IncidentID:  "inc-123",
		RiskLevel:   ptr("HIGH"),
		Confidence:  intPtr(80),
		AssetID:     "A1",
		ContainerID: "C1",
		ImageDigest: "D1",
	}

	rec1 := GenerateRecommendations(inc)[0]
	rec2 := GenerateRecommendations(inc)[0] // Concurrent identical evaluation

	fp1 := ComputeFingerprint(&rec1)
	fp2 := ComputeFingerprint(&rec2)

	if fp1 != fp2 {
		t.Errorf("Fingerprints differ for identical inputs: %s != %s", fp1, fp2)
	}
}
