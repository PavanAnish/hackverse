package repository_test


import (
	"context"
	"database/sql"
	"os"
	"testing"
	
	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/pavanvanish/zeraxis/engines/correlation/internal/repository"
)

func getDB2(t *testing.T) *sql.DB {
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


func TestExecutionIdentityBinding(t *testing.T) {
	repo, err := repository.SetupTestDB(t)
	if err != nil {
		t.Skipf("DB not available: %v", err)
	}
	db := repo.DB()
	defer db.Close()
	
	ctx := context.Background()

	incID1 := uuid.New().String()
	db.Exec("INSERT INTO incidents (incident_id, fingerprint, status, risk_level, first_seen, last_seen) VALUES ($1, 'f1', 'OPEN', 'CRITICAL', NOW(), NOW())", incID1)
	
	recID1 := uuid.New().String()
	db.Exec("INSERT INTO incident_recommendations (recommendation_id, incident_id, fingerprint, action, target_container_id, model_version, status, priority, reason, evidence) VALUES ($1, $2, 'fp1', 'STOP_CONTAINER', 'container1', 'v1', 'APPROVED', 'HIGH', 'test reason', '{}')", recID1, incID1)
	
	execID := uuid.New().String()
	db.Exec("INSERT INTO execution_authorizations (execution_id, recommendation_id, approver_id, status) VALUES ($1, $2, 'sys', 'EXECUTED')", execID, recID1)
	
	db.Exec("INSERT INTO execution_results (execution_id, recommendation_id, status, verification_status) VALUES ($1, $2, 'EXECUTED', 'VERIFIED')", execID, recID1)

	// Create a completely unrelated incident and recommendation
	incID2 := uuid.New().String()
	db.Exec("INSERT INTO incidents (incident_id, fingerprint, status, risk_level, first_seen, last_seen) VALUES ($1, 'f2', 'OPEN', 'CRITICAL', NOW(), NOW())", incID2)
	
	recID2 := uuid.New().String()
	db.Exec("INSERT INTO incident_recommendations (recommendation_id, incident_id, fingerprint, action, target_container_id, model_version, status, priority, reason, evidence) VALUES ($1, $2, 'fp2', 'STOP_CONTAINER', 'container2', 'v1', 'APPROVED', 'HIGH', 'test reason', '{}')", recID2, incID2)

	// Try to resolve the UNRELATED incident using the old execution_id
	// But Phase 7.6 only uses the execution_id to derive the lineage.
	resolvedIncID, err := repo.ResolveIncidentFromExecution(ctx, execID)
	if err != nil {
		t.Fatalf("Expected old execution to resolve its own incident, got error: %v", err)
	}

	if resolvedIncID != incID1 {
		t.Fatalf("Expected execution to resolve incID1 (%s), but resolved %s", incID1, resolvedIncID)
	}

	// Try to fetch the old execution record. It should be permanently bound to container1, not container2.
	execRec, err := repo.GetExecutionRecord(ctx, execID)
	if err != nil {
		t.Fatalf("Failed to fetch execution record: %v", err)
	}
	if execRec.TargetContainerID != "container1" {
		t.Fatalf("Expected target container to remain permanently bound to container1, got %s", execRec.TargetContainerID)
	}
	if execRec.RecommendationID != recID1 {
		t.Fatalf("Expected recommendation to remain permanently bound to %s, got %s", recID1, execRec.RecommendationID)
	}
}
