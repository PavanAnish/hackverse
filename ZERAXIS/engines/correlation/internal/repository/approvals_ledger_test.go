package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pavanvanish/zeraxis/engines/correlation/internal/repository"
)

func TestPhase75_ApprovalsLedger(t *testing.T) {
	repo, err := repository.SetupTestDB(t)
	if err != nil {
		t.Skipf("DB not available: %v", err)
	}
	defer repo.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	incID := uuid.New().String()
	recID := uuid.New().String()

	// Setup fixture
	_, err = repo.DB().ExecContext(ctx, `INSERT INTO incidents (incident_id, fingerprint, status, severity, finding_count, first_seen, last_seen) VALUES ($1, $2, 'OPEN', 'HIGH', 1, NOW(), NOW())`, incID, incID)
	if err != nil {
		t.Fatalf("Failed to insert incident: %v", err)
	}
	_, err = repo.DB().ExecContext(ctx, `
		INSERT INTO incident_recommendations (recommendation_id, incident_id, fingerprint, action, status, priority, reason, evidence, model_version, target_asset_id, target_container_id, target_image_digest)
		VALUES ($1, $2, $3, 'STOP_CONTAINER', 'PROPOSED', 'HIGH', 'Test', '{}', '1.0', 'asset-1', 'container-1', 'sha256:123')
	`, recID, incID, recID)
	if err != nil {
		t.Fatalf("Failed to insert recommendation: %v", err)
	}

	// 1. Approve
	execID, err := repo.ApproveRecommendation(ctx, recID, "human-1")
	if err != nil {
		t.Fatalf("ApproveRecommendation failed: %v", err)
	}

	// Verify ledger
	var count int
	err = repo.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM execution_state_transitions WHERE execution_id = $1 AND to_status = 'APPROVED'`, execID).Scan(&count)
	if err != nil || count != 1 {
		t.Fatalf("expected 1 APPROVED ledger entry, got %d (err: %v)", count, err)
	}

	// 2. Revoke
	err = repo.RevokeRecommendation(ctx, recID)
	if err != nil {
		t.Fatalf("RevokeRecommendation failed: %v", err)
	}

	// Verify ledger
	err = repo.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM execution_state_transitions WHERE execution_id = $1 AND to_status = 'REVOKED'`, execID).Scan(&count)
	if err != nil || count != 1 {
		t.Fatalf("expected 1 REVOKED ledger entry, got %d (err: %v)", count, err)
	}
}
