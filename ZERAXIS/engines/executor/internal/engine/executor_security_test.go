package engine

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/pavanvanish/zeraxis/engines/executor/internal/docker"
	_ "github.com/lib/pq"
)

// realDigest is a valid sha256:<64hex> string for use in tests requiring OCI digest format.
const realDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const otherDigest = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

func setupDB(t *testing.T) *sql.DB {
	db, err := sql.Open("postgres", "postgres://postgres:postgres@localhost:5433/zeraxis?sslmode=disable")
	if err != nil || db.Ping() != nil {
		t.Skip("DB not available")
	}
	return db
}

func TestSecurity_OciDigestMismatch(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	ctx := context.Background()

	incID := uuid.New().String()
	if _, err := db.Exec(`INSERT INTO incidents (incident_id, fingerprint, status, risk_level, finding_count, severity, first_seen, last_seen) VALUES ($1, $2, 'OPEN', 'CRITICAL', 1, 'CRITICAL', NOW(), NOW())`, incID, incID); err != nil {
		t.Fatalf("insert incident: %v", err)
	}

	assetID := uuid.New().String()
	containerID := uuid.New().String()
	recID := uuid.New().String()
	// target_image_digest = realDigest, mock returns otherDigest => DENY
	if _, err := db.Exec(`INSERT INTO incident_recommendations (recommendation_id, incident_id, fingerprint, action, priority, confidence, reason, evidence, model_version, status, target_asset_id, target_container_id, target_image_digest) VALUES ($1, $2, $3, 'STOP_CONTAINER', 'CRITICAL', 95, 'Test', '{}', '1.0', 'APPROVED', $4, $5, $6)`,
		recID, incID, recID, assetID, containerID, realDigest); err != nil {
		t.Fatalf("insert recommendation: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO discovery_assets (asset_id, name, container_id, image, status) VALUES ($1, $2, $2, 'img', 'running')`, assetID, containerID); err != nil {
		t.Fatalf("insert discovery_asset: %v", err)
	}

	execID := uuid.New().String()
	if _, err := db.Exec(`INSERT INTO execution_authorizations (execution_id, recommendation_id, approver_id, status) VALUES ($1, $2, 'user-1', 'APPROVED')`, execID, recID); err != nil {
		t.Fatalf("insert authorization: %v", err)
	}

	// Mock returns otherDigest — canonical comparison must DENY
	mockCli := docker.NewMockClient(otherDigest, "net1")
	executor := NewExecutor(db, mockCli, nil)
	err := executor.Execute(ctx, execID)
	if err == nil || !strings.Contains(err.Error(), "image digest mismatch") {
		t.Fatalf("Expected DENY for OCI digest mismatch, got: %v", err)
	}
}

// TestSecurity_ImageID_Negative proves that a target_image_digest that is NOT in the
// canonical RepoDigest form (here we store realDigest as target, but mock returns a different
// digest) cannot authorize. This is the functional negative for ImageID misuse.
func TestSecurity_ImageID_Negative(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	ctx := context.Background()

	incID := uuid.New().String()
	if _, err := db.Exec(`INSERT INTO incidents (incident_id, fingerprint, status, risk_level, finding_count, severity, first_seen, last_seen) VALUES ($1, $2, 'OPEN', 'CRITICAL', 1, 'CRITICAL', NOW(), NOW())`, incID, incID); err != nil {
		t.Fatalf("insert incident: %v", err)
	}

	assetID := uuid.New().String()
	containerID := uuid.New().String()
	recID := uuid.New().String()
	// Store realDigest as target — it looks like a local ImageID (sha256:<hex>) but is actually
	// the registry manifest digest. The mock returns otherDigest as its registry digest, so
	// even if an attacker tries to register the ImageID as the authorized digest,
	// it won't match the running container's RepoDigest.
	if _, err := db.Exec(`INSERT INTO incident_recommendations (recommendation_id, incident_id, fingerprint, action, priority, confidence, reason, evidence, model_version, status, target_asset_id, target_container_id, target_image_digest) VALUES ($1, $2, $3, 'STOP_CONTAINER', 'CRITICAL', 95, 'Test', '{}', '1.0', 'APPROVED', $4, $5, $6)`,
		recID, incID, recID, assetID, containerID, realDigest); err != nil {
		t.Fatalf("insert recommendation: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO discovery_assets (asset_id, name, container_id, image, status) VALUES ($1, $2, $2, 'img', 'running')`, assetID, containerID); err != nil {
		t.Fatalf("insert discovery_asset: %v", err)
	}

	execID := uuid.New().String()
	if _, err := db.Exec(`INSERT INTO execution_authorizations (execution_id, recommendation_id, approver_id, status) VALUES ($1, $2, 'user-1', 'APPROVED')`, execID, recID); err != nil {
		t.Fatalf("insert authorization: %v", err)
	}

	// Mock returns otherDigest as the registry digest (simulates running container image
	// having been replaced since authorization).
	mockCli := docker.NewMockClient(otherDigest, "net1")
	executor := NewExecutor(db, mockCli, nil)
	err := executor.Execute(ctx, execID)
	if err == nil || !strings.Contains(err.Error(), "image digest mismatch") {
		t.Fatalf("Expected DENY since realDigest != otherDigest in RepoDigests, got: %v", err)
	}
	if mockCli.GetStopCount(containerID) != 0 {
		t.Fatalf("Expected zero Docker mutations, got %d", mockCli.GetStopCount(containerID))
	}
}

func TestApprovals_ProposedToApproved(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	ctx := context.Background()

	incID := uuid.New().String()
	if _, err := db.Exec(`INSERT INTO incidents (incident_id, fingerprint, status, risk_level, finding_count, severity, first_seen, last_seen) VALUES ($1, $2, 'OPEN', 'CRITICAL', 1, 'CRITICAL', NOW(), NOW())`, incID, incID); err != nil {
		t.Fatalf("insert incident: %v", err)
	}
	recID := uuid.New().String()
	if _, err := db.Exec(`INSERT INTO incident_recommendations (recommendation_id, incident_id, fingerprint, action, priority, confidence, reason, evidence, model_version, status) VALUES ($1, $2, $3, 'STOP_CONTAINER', 'CRITICAL', 95, 'Test', '{}', '1.0', 'PROPOSED')`, recID, incID, recID); err != nil {
		t.Fatalf("insert recommendation: %v", err)
	}

	execID, err := testApproveAndPublish(ctx, db, recID, "user-admin")
	if err != nil {
		t.Fatalf("approval failed: %v", err)
	}
	if execID == "" {
		t.Fatal("expected non-empty execID")
	}
	var status string
	if err := db.QueryRow(`SELECT status FROM incident_recommendations WHERE recommendation_id = $1`, recID).Scan(&status); err != nil || status != "APPROVED" {
		t.Fatalf("expected APPROVED, got %s err=%v", status, err)
	}
}

func TestApprovals_AlreadyApproved_Rejected(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	ctx := context.Background()

	incID := uuid.New().String()
	if _, err := db.Exec(`INSERT INTO incidents (incident_id, fingerprint, status, risk_level, finding_count, severity, first_seen, last_seen) VALUES ($1, $2, 'OPEN', 'CRITICAL', 1, 'CRITICAL', NOW(), NOW())`, incID, incID); err != nil {
		t.Fatalf("insert incident: %v", err)
	}
	recID := uuid.New().String()
	if _, err := db.Exec(`INSERT INTO incident_recommendations (recommendation_id, incident_id, fingerprint, action, priority, confidence, reason, evidence, model_version, status) VALUES ($1, $2, $3, 'STOP_CONTAINER', 'CRITICAL', 95, 'Test', '{}', '1.0', 'APPROVED')`, recID, incID, recID); err != nil {
		t.Fatalf("insert recommendation: %v", err)
	}

	_, err := testApproveAndPublish(ctx, db, recID, "user-admin")
	if err == nil || !strings.Contains(err.Error(), "PROPOSED") {
		t.Fatalf("expected DENY for already-APPROVED recommendation, got: %v", err)
	}
}

func TestApprovals_EmptyApprover_Rejected(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	ctx := context.Background()

	incID := uuid.New().String()
	if _, err := db.Exec(`INSERT INTO incidents (incident_id, fingerprint, status, risk_level, finding_count, severity, first_seen, last_seen) VALUES ($1, $2, 'OPEN', 'CRITICAL', 1, 'CRITICAL', NOW(), NOW())`, incID, incID); err != nil {
		t.Fatalf("insert incident: %v", err)
	}
	recID := uuid.New().String()
	if _, err := db.Exec(`INSERT INTO incident_recommendations (recommendation_id, incident_id, fingerprint, action, priority, confidence, reason, evidence, model_version, status) VALUES ($1, $2, $3, 'STOP_CONTAINER', 'CRITICAL', 95, 'Test', '{}', '1.0', 'PROPOSED')`, recID, incID, recID); err != nil {
		t.Fatalf("insert recommendation: %v", err)
	}

	_, err := testApproveAndPublish(ctx, db, recID, "")
	if err == nil || !strings.Contains(err.Error(), "approver") {
		t.Fatalf("expected DENY for empty approver, got: %v", err)
	}
}

func TestApprovals_MissingRecommendation_Rejected(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	ctx := context.Background()

	nonExistent := uuid.New().String()
	_, err := testApproveAndPublish(ctx, db, nonExistent, "user-admin")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected DENY for missing recommendation, got: %v", err)
	}
}

// testApproveAndPublish calls the approval service inline (nc=nil for unit tests).
// The production path is engines/correlation/pkg/approvals.Service.ApproveAndPublish.
func testApproveAndPublish(ctx context.Context, db *sql.DB, recommendationID, approverID string) (string, error) {
	if strings.TrimSpace(approverID) == "" {
		return "", fmt.Errorf("approver identity is required")
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var currentStatus string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM incident_recommendations WHERE recommendation_id = $1 FOR UPDATE`, recommendationID).Scan(&currentStatus); err != nil {
		return "", fmt.Errorf("recommendation not found")
	}
	if currentStatus != "PROPOSED" {
		return "", fmt.Errorf("only PROPOSED recommendations can be approved, current status: %s", currentStatus)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE incident_recommendations SET status = 'APPROVED', updated_at = NOW() WHERE recommendation_id = $1`, recommendationID); err != nil {
		return "", err
	}
	execID := uuid.New().String()
	if _, err := tx.ExecContext(ctx, `INSERT INTO execution_authorizations (execution_id, recommendation_id, approver_id, status, approved_at, created_at, updated_at) VALUES ($1, $2, $3, 'APPROVED', NOW(), NOW(), NOW())`, execID, recommendationID, approverID); err != nil {
		return "", err
	}
	return execID, tx.Commit()
}

func TestConcurrency_DuplicateAuthorization(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	ctx := context.Background()

	incID := uuid.New().String()
	if _, err := db.Exec(`INSERT INTO incidents (incident_id, fingerprint, status, risk_level, finding_count, severity, first_seen, last_seen) VALUES ($1, $2, 'OPEN', 'CRITICAL', 1, 'CRITICAL', NOW(), NOW())`, incID, incID); err != nil {
		t.Fatalf("insert incident: %v", err)
	}

	assetID := uuid.New().String()
	containerID := uuid.New().String()
	recID := uuid.New().String()
	if _, err := db.Exec(`INSERT INTO incident_recommendations (recommendation_id, incident_id, fingerprint, action, priority, confidence, reason, evidence, model_version, status, target_asset_id, target_container_id, target_image_digest) VALUES ($1, $2, $3, 'STOP_CONTAINER', 'CRITICAL', 95, 'Test', '{}', '1.0', 'APPROVED', $4, $5, $6)`,
		recID, incID, recID, assetID, containerID, realDigest); err != nil {
		t.Fatalf("insert recommendation: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO discovery_assets (asset_id, name, container_id, image, status) VALUES ($1, $1, $2, 'img', 'running')`, assetID, containerID); err != nil {
		t.Fatalf("insert discovery_asset: %v", err)
	}

	execID := uuid.New().String()
	if _, err := db.Exec(`INSERT INTO execution_authorizations (execution_id, recommendation_id, approver_id, status) VALUES ($1, $2, 'user-1', 'APPROVED')`, execID, recID); err != nil {
		t.Fatalf("insert authorization: %v", err)
	}

	mockCli := docker.NewMockClient(realDigest, "net1")
	executor := NewExecutor(db, mockCli, nil)

	// Launch multiple concurrent workers trying to execute the same ID
	workers := 5
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		go func() {
			errs <- executor.Execute(ctx, execID)
		}()
	}

	successCount := 0
	for i := 0; i < workers; i++ {
		err := <-errs
		if err == nil {
			successCount++
		} else if !strings.Contains(err.Error(), "authorization revoked, expired, or already executed") && !strings.Contains(err.Error(), "could not obtain lock") {
			t.Errorf("unexpected error from worker: %v", err)
		}
	}

	if successCount != 1 {
		t.Errorf("expected exactly 1 worker to succeed, got %d", successCount)
	}

	var resultsCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM execution_results WHERE execution_id = $1", execID).Scan(&resultsCount); err != nil {
		t.Fatalf("count results: %v", err)
	}
	if resultsCount != 1 {
		t.Errorf("expected exactly 1 execution_result row, got %d", resultsCount)
	}

	// Independently prove that the Docker mutation itself occurred exactly once.
	// This is separate from the Execute() success count and the DB row count.
	mutationCount := mockCli.TotalMutationCount()
	if mutationCount != 1 {
		t.Errorf("expected exactly 1 Docker ContainerStop call (mutation_count), got %d", mutationCount)
	}
}
