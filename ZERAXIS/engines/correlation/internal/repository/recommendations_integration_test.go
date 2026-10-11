package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pavanvanish/zeraxis/engines/correlation/internal/models"
)

func ptr(s string) *string { return &s }
func intPtr(i int) *int { return &i }

func TestIntegration_AuthoritativeTargetPopulation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping DB integration test in short mode")
	}

	// This assumes the shared test DB setup from other tests
	repo, err := SetupTestDB(t)
	if err != nil {
		t.Skipf("DB not available: %v", err)
	}

	ctx := context.Background()

	// 1. Create Incident
	incID := uuid.New().String()
	inc := &models.Incident{
		IncidentID:  incID,
		Fingerprint: "fp-" + incID,
		Status:      models.StatusOpen,
		AssetID:     "A-123",
		ContainerID: "C-456",
		ImageDigest: "sha256:abcd",
		Severity:    "CRITICAL",
		FirstSeen:   time.Now(),
		LastSeen:    time.Now(),
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	err = repo.CreateIncidentWithFinding(ctx, inc, "finding-123")
	if err != nil {
		t.Fatalf("Failed to create incident: %v", err)
	}

	// 2. Generate Recommendation (simulate recommender.go buildRec)
	tt := "container"
	tid := "C-456"
	rec := &models.Recommendation{
		RecommendationID:  uuid.New().String(),
		IncidentID:        incID,
		Action:            "QUARANTINE_ASSET",
		TargetType:        &tt,
		TargetID:          &tid,
		TargetAssetID:     ptr("A-123"),
		TargetContainerID: ptr("C-456"),
		TargetImageDigest: ptr("sha256:abcd"),
		Priority:          "CRITICAL",
		Confidence:        intPtr(95),
		Reason:            "Integration test",
		Evidence:          json.RawMessage(`{"risk_level":"CRITICAL"}`),
		ModelVersion:      "1.0",
		Status:            "PROPOSED",
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
	}

	// 3. Persist Recommendation
	_, _, err = repo.UpsertRecommendation(ctx, rec, "rec-fp-"+rec.RecommendationID)
	if err != nil {
		t.Fatalf("Failed to upsert recommendation: %v", err)
	}

	// 4. Read back and Assert Authoritative Target Identity
	readRecs, err := repo.GetIncidentRecommendations(ctx, incID)
	if err != nil {
		t.Fatalf("Failed to read recommendations: %v", err)
	}

	if len(readRecs) != 1 {
		t.Fatalf("Expected 1 recommendation, got %d", len(readRecs))
	}

	readRec := readRecs[0]
	if readRec.TargetAssetID == nil || *readRec.TargetAssetID != "A-123" {
		t.Errorf("TargetAssetID mismatch: got %v", readRec.TargetAssetID)
	}
	if readRec.TargetContainerID == nil || *readRec.TargetContainerID != "C-456" {
		t.Errorf("TargetContainerID mismatch: got %v", readRec.TargetContainerID)
	}
	if readRec.TargetImageDigest == nil || *readRec.TargetImageDigest != "sha256:abcd" {
		t.Errorf("TargetImageDigest mismatch: got %v", readRec.TargetImageDigest)
	}
}
