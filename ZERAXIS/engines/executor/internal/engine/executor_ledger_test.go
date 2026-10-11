package engine_test

import (
	"context"
	"database/sql"
	"strings"
	"os"
	"sync"
	"testing"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/pavanvanish/zeraxis/engines/executor/internal/docker"
	"github.com/pavanvanish/zeraxis/engines/executor/internal/engine"
)

func testDB(t *testing.T) *sql.DB {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:5433/zeraxis?sslmode=disable"
	}
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		t.Skipf("DB not available: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Skipf("DB not reachable: %v", err)
	}
	return db
}

func insertFixture(t *testing.T, db *sql.DB, incStatus, authStatus string) (string, string) {
	t.Helper()
	incID := uuid.New().String()
	recID := uuid.New().String()
	execID := uuid.New().String()
	assetID := uuid.New().String()

	_, err := db.Exec(`INSERT INTO discovery_assets (asset_id, name, container_id, image, status) VALUES ($1, $1, $1, 'img', 'running')`, assetID)
	if err != nil {
		t.Fatalf("setup asset: %v", err)
	}

	_, err = db.Exec(`INSERT INTO incidents (incident_id, fingerprint, status, severity, finding_count, first_seen, last_seen, risk_level) VALUES ($1, $2, $3, 'HIGH', 1, NOW(), NOW(), 'CRITICAL')`, incID, incID, incStatus)
	if err != nil {
		t.Fatalf("setup incident: %v", err)
	}
	_, err = db.Exec(`
		INSERT INTO incident_recommendations (recommendation_id, incident_id, fingerprint, action, status, priority, reason, evidence, model_version, target_asset_id, target_container_id, target_image_digest)
		VALUES ($1, $2, $3, 'STOP_CONTAINER', 'APPROVED', 'HIGH', 'Test', '{}', '1.0', $4, $4, 'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa')
	`, recID, incID, recID, assetID)
	if err != nil {
		t.Fatalf("setup rec: %v", err)
	}
	_, err = db.Exec(`
		INSERT INTO execution_authorizations (execution_id, recommendation_id, approver_id, status, approved_at, created_at, updated_at)
		VALUES ($1, $2, 'test-approver', $3, NOW(), NOW(), NOW())
	`, execID, recID, authStatus)
	if err != nil {
		t.Fatalf("setup auth: %v", err)
	}
	
	// Create an initial ledger entry for APPROVED
	transID := uuid.New().String()
	_, _ = db.Exec(`
		INSERT INTO execution_state_transitions (transition_id, execution_id, from_status, to_status, actor_type, actor_id, timestamp, reason)
		VALUES ($1, $2, NULL, 'APPROVED', 'human', 'test-approver', NOW(), 'Test setup')
	`, transID, execID)

	return execID, assetID
}

func countLedger(t *testing.T, db *sql.DB, execID, toStatus string) int {
	var c int
	err := db.QueryRow(`SELECT COUNT(*) FROM execution_state_transitions WHERE execution_id = $1 AND to_status = $2`, execID, toStatus).Scan(&c)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestPhase75_ConcurrencyClaim(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	execID, _ := insertFixture(t, db, "OPEN", "APPROVED")

	mockDocker := docker.NewMockClient("sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "net-1")

	var wg sync.WaitGroup
	successCount := 0
	var mu sync.Mutex

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e := engine.NewExecutor(db, mockDocker, nil)
			err := e.Execute(context.Background(), execID)
			mu.Lock()
			if err == nil {
				successCount++
			}
			mu.Unlock()
		}()
	}
	wg.Wait()

	if successCount != 1 {
		t.Errorf("expected exactly 1 execution to succeed, got %d", successCount)
	}
	if c := countLedger(t, db, execID, "EXECUTING"); c != 1 {
		t.Errorf("expected exactly 1 EXECUTING ledger entry, got %d", c)
	}
}

func TestPhase75_ResolvedIncidentRejection(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	execID, _ := insertFixture(t, db, "RESOLVED", "APPROVED")

	mockDocker := docker.NewMockClient("sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "net-1")

	e := engine.NewExecutor(db, mockDocker, nil)
	err := e.Execute(context.Background(), execID)

	if err == nil || err.Error() != "DENY: Incident closed" {
		t.Errorf("expected 'DENY: Incident closed', got %v", err)
	}
	if mockDocker.TotalMutationCount() != 0 {
		t.Errorf("expected 0 docker mutations, got %d", mockDocker.TotalMutationCount())
	}
	if c := countLedger(t, db, execID, "EXECUTING"); c != 0 {
		t.Errorf("expected 0 EXECUTING ledger entries, got %d", c)
	}
	if c := countLedger(t, db, execID, "FAILED"); c != 1 {
		t.Errorf("expected exactly 1 FAILED ledger entry for stale incident, got %d", c)
	}
}

func TestPhase75_CrashAfterMutation(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	execID, _ := insertFixture(t, db, "OPEN", "APPROVED")

	mockDocker := docker.NewMockClient("sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "net-1")
	mockDocker.ContainerStopPanic = true
	
	e := engine.NewExecutor(db, mockDocker, nil)
	
	defer func() {
		r := recover()
		if r == nil {
			t.Error("expected panic")
		}

		// Check states
		var status string
		_ = db.QueryRow(`SELECT status FROM execution_authorizations WHERE execution_id = $1`, execID).Scan(&status)
		if status != "EXECUTING" {
			t.Errorf("expected EXECUTING after crash, got %s", status)
		}

		if c := countLedger(t, db, execID, "EXECUTED"); c != 0 {
			t.Errorf("expected 0 EXECUTED ledger entries (no fabricated terminal entry), got %d", c)
		}
		if c := countLedger(t, db, execID, "FAILED"); c != 0 {
			t.Errorf("expected 0 FAILED ledger entries (no fabricated terminal entry), got %d", c)
		}
	}()

	_ = e.Execute(context.Background(), execID)
}

func TestPhase75_LedgerUpdateDeleteRejection(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	execID, _ := insertFixture(t, db, "OPEN", "APPROVED")

	// Verify trigger blocks UPDATE
	_, err := db.Exec(`UPDATE execution_state_transitions SET reason = 'tampered' WHERE execution_id = $1`, execID)
	if err == nil {
		t.Error("expected UPDATE to fail, but it succeeded")
	} else if !strings.Contains(err.Error(), "prohibited") {
		t.Errorf("unexpected error: %v", err)
	}

	// Verify trigger blocks DELETE
	_, err = db.Exec(`DELETE FROM execution_state_transitions WHERE execution_id = $1`, execID)
	if err == nil {
		t.Error("expected DELETE to fail, but it succeeded")
	} else if !strings.Contains(err.Error(), "prohibited") {
		t.Errorf("unexpected error: %v", err)
	}
}
