package engine_test

import (
	"github.com/pavanvanish/zeraxis/engines/executor/internal/docker"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"testing"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/pavanvanish/zeraxis/engines/executor/internal/engine"
)

func getDB(t *testing.T) *sql.DB {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:5433/zeraxis?sslmode=disable"
	}
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		t.Fatalf("Failed to connect to DB: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Skipf("DB not available: %v", err)
	}
	return db
}

// AdversarialPayload represents an attacker trying to inject alternative identities.
type AdversarialPayload struct {
	ExecutionID       string `json:"execution_id"`
	RecommendationID  string `json:"recommendation_id"`
	IncidentID        string `json:"incident_id"`
	TargetAssetID     string `json:"asset_id"`
	TargetContainerID string `json:"container_id"`
	TargetImageDigest string `json:"target_image_digest"`
}

func TestExecutor_IdentityBinding_AdversarialPayload(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	
	ctx := context.Background()
	incID := uuid.New().String()
	if _, err := db.Exec("INSERT INTO incidents (incident_id, fingerprint, status, risk_level, first_seen, last_seen) VALUES ($1, $2, 'OPEN', 'CRITICAL', NOW(), NOW())", incID, uuid.New().String()); err != nil { t.Fatalf("inc: %v", err) }
	
	recID := uuid.New().String()
	assetID := uuid.New().String()
	containerID := uuid.New().String()
	if _, err := db.Exec("INSERT INTO incident_recommendations (recommendation_id, incident_id, fingerprint, action, target_container_id, model_version, status, priority, reason, evidence, target_asset_id, target_image_digest) VALUES ($1, $2, $3, 'STOP_CONTAINER', $4, 'v1', 'APPROVED', 'HIGH', 'test', '{}', $5, 'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa')", recID, incID, uuid.New().String(), containerID, assetID); err != nil { t.Fatalf("rec: %v", err) }
	
	execID := uuid.New().String()
	if _, err := db.Exec("INSERT INTO discovery_assets (asset_id, name, container_id, image, status) VALUES ($1, $1, $2, 'img', 'running')", assetID, containerID); err != nil { t.Fatalf("asset: %v", err) }
	if _, err := db.Exec("INSERT INTO execution_authorizations (execution_id, recommendation_id, approver_id, status) VALUES ($1, $2, 'human', 'APPROVED')", execID, recID); err != nil { t.Fatalf("auth: %v", err) }

	// We pass a mock docker client to prevent actual Docker calls, but here we just want to ensure it fails/passes based on legit container.
	// We'll just construct the executor with nil for docker (it will fail the mutation step, but the binding occurs before that).
	mockDocker := docker.NewMockClient("sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "net-1")
	exec := engine.NewExecutor(db, mockDocker, nil)
	
	// Malicious payload trying to substitute another container
	malicious := AdversarialPayload{
		ExecutionID:       execID,
		RecommendationID:  uuid.New().String(), // attempt to hijack recommendation
		IncidentID:        uuid.New().String(), // attempt to hijack incident
		TargetAssetID:     "attacker-asset",
		TargetContainerID: "attacker-container",
		TargetImageDigest: "attacker-digest",
	}
	
	payloadBytes, _ := json.Marshal(malicious)
	
	// Trigger the wakeup
	exec.HandleNatsWakeup(ctx, payloadBytes)
	
	// Because Docker is nil, it will fail mutation, but we can check the execution_results to see what recommendation it bound to.
	var resultRecID string
	err := db.QueryRow("SELECT recommendation_id FROM execution_results WHERE execution_id = $1", execID).Scan(&resultRecID)
	if err != nil {
		t.Fatalf("Failed to fetch execution result: %v", err)
	}
	
	if resultRecID != recID {
		t.Fatalf("IDENTITY BINDING FAILED: Expected executor to bind to original recommendation %s, but got %s", recID, resultRecID)
	}
	if resultRecID == malicious.RecommendationID {
		t.Fatalf("SECURITY VULNERABILITY: Executor accepted malicious recommendation_id injection!")
	}
	
	// Also ensure Tx2 completed with the legitimate bindings
	var authStatus string
	db.QueryRow("SELECT status FROM execution_authorizations WHERE execution_id = $1", execID).Scan(&authStatus)
	if authStatus != "EXECUTED" && authStatus != "FAILED" {
		t.Fatalf("Expected auth to be EXECUTED or FAILED, got %s", authStatus)
	}
}
