package approvals_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/pavanvanish/zeraxis/engines/correlation/pkg/approvals"
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

	// Clean schema
	_, _ = db.Exec(`DROP SCHEMA public CASCADE; CREATE SCHEMA public;`)

	// Run migrations
	migrations := []string{
		"../../migrations/001_incidents.sql",
		"../../migrations/002_risk_model.sql",
		"../../migrations/003_recommendations.sql",
		"../../migrations/004_recommendation_targets.sql",
		"../../migrations/005_execution_authorizations.sql",
		"../../migrations/006_execution_results.sql",
		"../../migrations/007_recommendation_status_update.sql",
		"../../migrations/008_execution_verification.sql",
		"../../migrations/009_recovery_lineage.sql",
		"../../migrations/010_execution_audit_ledger.sql",
		"../../migrations/011_recommendation_executed_status.sql",
		"../../migrations/012_recommendation_investigation_status.sql",
		"../../migrations/013_human_attestations.sql",
	}
	for _, m := range migrations {
		b, err := os.ReadFile(m)
		if err != nil {
			t.Fatalf("Failed to read migration %s: %v", m, err)
		}
		if _, err := db.Exec(string(b)); err != nil {
			t.Fatalf("Failed to exec migration %s: %v", m, err)
		}
	}

	return db
}

func setupRec(t *testing.T, db *sql.DB, status string) (string, string) {
	incID := uuid.New().String()
	recID := uuid.New().String()
	_, err := db.Exec("INSERT INTO incidents (incident_id, fingerprint, status, first_seen, last_seen) VALUES ($1, $2, 'OPEN', NOW(), NOW())", incID, uuid.New().String())
	if err != nil { t.Fatalf("setup inc: %v", err) }
	_, err = db.Exec("INSERT INTO incident_recommendations (recommendation_id, incident_id, fingerprint, action, status, priority, target_asset_id, target_container_id, target_image_digest, reason, evidence, model_version) VALUES ($1, $2, $3, 'STOP_CONTAINER', $4, 'HIGH', 'asset-1', 'container-1', 'digest-1', 'test reason', '{}', 'v1')", recID, incID, uuid.New().String(), status)
	if err != nil { t.Fatalf("setup rec: %v", err) }
	return incID, recID
}

func setupExec(t *testing.T, db *sql.DB, recID, status string) string {
	execID := uuid.New().String()
	_, err := db.Exec(`INSERT INTO execution_authorizations (execution_id, recommendation_id, approver_id, status) VALUES ($1, $2, 'sys', $3)`, execID, recID, status)
	if err != nil {
		t.Fatalf("setup exec: %v", err)
	}
	return execID
}

func TestApproveAndPublish_Valid(t *testing.T) {
	db := getDB(t)
	defer db.Close()

	_, recID := setupRec(t, db, "PROPOSED")
	svc := approvals.NewService(db, nil)

	execID, err := svc.ApproveAndPublish(context.Background(), recID, "user-123")
	if err != approvals.ErrDispatchUnavailable {
		t.Fatalf("Expected dispatch unavailable, got %v", err)
	}

	var authStatus string
	_ = db.QueryRow(`SELECT status FROM execution_authorizations WHERE execution_id = $1`, execID).Scan(&authStatus)
	if authStatus != "APPROVED" {
		t.Fatalf("Expected APPROVED auth, got %s", authStatus)
	}

	var recStatus string
	_ = db.QueryRow(`SELECT status FROM incident_recommendations WHERE recommendation_id = $1`, recID).Scan(&recStatus)
	if recStatus != "APPROVED" {
		t.Fatalf("Expected APPROVED rec, got %s", recStatus)
	}

	// Ledger
	var count int
	_ = db.QueryRow(`SELECT COUNT(*) FROM execution_state_transitions WHERE execution_id = $1 AND to_status = 'APPROVED'`, execID).Scan(&count)
	if count != 1 {
		t.Fatalf("Expected 1 ledger entry, got %d", count)
	}
}

func TestApprove_MissingIdentity(t *testing.T) {
	db := getDB(t)
	defer db.Close()
	svc := approvals.NewService(db, nil)

	_, err := svc.ApproveAndPublish(context.Background(), "123", "")
	if err == nil || !strings.Contains(err.Error(), "identity is required") {
		t.Fatalf("Expected identity error, got %v", err)
	}
}

func TestApprove_NotProposed(t *testing.T) {
	db := getDB(t)
	defer db.Close()
	_, recID := setupRec(t, db, "APPROVED")
	svc := approvals.NewService(db, nil)

	_, err := svc.ApproveAndPublish(context.Background(), recID, "user-123")
	if err == nil || !strings.Contains(err.Error(), "only PROPOSED") {
		t.Fatalf("Expected only PROPOSED error, got %v", err)
	}
}

func TestRevoke_Valid(t *testing.T) {
	db := getDB(t)
	defer db.Close()
	_, recID := setupRec(t, db, "APPROVED")
	execID := setupExec(t, db, recID, "APPROVED")
	svc := approvals.NewService(db, nil)

	err := svc.Revoke(context.Background(), recID, "user-123")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	var recStatus string
	_ = db.QueryRow(`SELECT status FROM incident_recommendations WHERE recommendation_id = $1`, recID).Scan(&recStatus)
	if recStatus != "PROPOSED" {
		t.Fatalf("Expected PROPOSED rec, got %s", recStatus)
	}

	var authStatus string
	_ = db.QueryRow(`SELECT status FROM execution_authorizations WHERE execution_id = $1`, execID).Scan(&authStatus)
	if authStatus != "REVOKED" {
		t.Fatalf("Expected REVOKED auth, got %s", authStatus)
	}

	var count int
	_ = db.QueryRow(`SELECT COUNT(*) FROM execution_state_transitions WHERE execution_id = $1 AND to_status = 'REVOKED'`, execID).Scan(&count)
	if count != 1 {
		t.Fatalf("Expected 1 revoke ledger entry, got %d", count)
	}
}

func TestRevoke_NotApprovedRec(t *testing.T) {
	db := getDB(t)
	defer db.Close()
	_, recID := setupRec(t, db, "PROPOSED")
	svc := approvals.NewService(db, nil)
	err := svc.Revoke(context.Background(), recID, "user")
	if err == nil || !strings.Contains(err.Error(), "only APPROVED") {
		t.Fatalf("Expected error, got %v", err)
	}
}

func TestRevoke_ExecutingAuth(t *testing.T) {
	db := getDB(t)
	defer db.Close()
	_, recID := setupRec(t, db, "APPROVED")
	_ = setupExec(t, db, recID, "EXECUTING") // Auth is executing
	svc := approvals.NewService(db, nil)
	err := svc.Revoke(context.Background(), recID, "user")
	if err == nil || !strings.Contains(err.Error(), "authorization cannot be revoked") {
		t.Fatalf("Expected error, got %v", err)
	}
}

func TestAbandon_Valid(t *testing.T) {
	db := getDB(t)
	defer db.Close()
	_, recID := setupRec(t, db, "APPROVED")
	execID := setupExec(t, db, recID, "EXECUTING")
	svc := approvals.NewService(db, nil)

	err := svc.Abandon(context.Background(), recID, "user-123")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	var recStatus string
	_ = db.QueryRow(`SELECT status FROM incident_recommendations WHERE recommendation_id = $1`, recID).Scan(&recStatus)
	if recStatus != "MANUAL_INVESTIGATION_REQUIRED" {
		t.Fatalf("Expected MANUAL_INVESTIGATION_REQUIRED rec, got %s", recStatus)
	}

	var authStatus string
	_ = db.QueryRow(`SELECT status FROM execution_authorizations WHERE execution_id = $1`, execID).Scan(&authStatus)
	if authStatus != "ABANDONED" {
		t.Fatalf("Expected ABANDONED auth, got %s", authStatus)
	}

	var count int
	_ = db.QueryRow(`SELECT COUNT(*) FROM execution_state_transitions WHERE execution_id = $1 AND to_status = 'ABANDONED'`, execID).Scan(&count)
	if count != 1 {
		t.Fatalf("Expected 1 abandon ledger entry, got %d", count)
	}
}

func TestAbandon_NotExecuting(t *testing.T) {
	db := getDB(t)
	defer db.Close()
	_, recID := setupRec(t, db, "APPROVED")
	_ = setupExec(t, db, recID, "APPROVED")
	svc := approvals.NewService(db, nil)
	err := svc.Abandon(context.Background(), recID, "user")
	if err == nil || !strings.Contains(err.Error(), "only EXECUTING") {
		t.Fatalf("Expected error, got %v", err)
	}
}

func TestConcurrency_Approve_Revoke(t *testing.T) {
	// A bit hard to test race reliably without sleep, but we can try basic concurrent requests
	db := getDB(t)
	defer db.Close()
	_, recID := setupRec(t, db, "PROPOSED")
	svc := approvals.NewService(db, nil)

	// Since we can't revoke until approved, the race is: if Approve happens at same time as Revoke,
	// Revoke will fail if Approve hasn't committed. If Approve commits, Revoke will succeed.
	// What about Revoke vs Abandon?
	// Revoke needs APPROVED, Abandon needs EXECUTING. They are disjoint.
	
	// A true race test would involve blocking inside the Tx, but standard go test -race ensures we don't have data races.
	ch := make(chan error, 2)
	go func() {
		_, err := svc.ApproveAndPublish(context.Background(), recID, "u1")
		ch <- err
	}()
	go func() {
		// Wait a tiny bit to give approve a chance to start
		time.Sleep(10 * time.Millisecond)
		err := svc.Revoke(context.Background(), recID, "u1")
		ch <- err
	}()

	err1 := <-ch
	err2 := <-ch
	
	// Just ensure no panics and standard errors. One might succeed, one fail.
	fmt.Printf("err1: %v\nerr2: %v\n", err1, err2)
}
