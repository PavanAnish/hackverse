package engine

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/google/uuid"
)

func TestExecutorTx2_Deadlock(t *testing.T) {
	dbURL := "postgres://postgres:postgres@localhost:5433/zeraxis?sslmode=disable"
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		t.Skip("Skipping deadlock test: postgres not available at localhost:5433")
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Skip("Skipping deadlock test: postgres not available")
	}

	execID := uuid.New().String()
	recID := uuid.New().String()
	incID := uuid.New().String()

	// Clean up
	defer func() {
		db.Exec("DELETE FROM execution_results WHERE execution_id = $1", execID)
		db.Exec("DELETE FROM execution_state_transitions WHERE execution_id = $1", execID)
		db.Exec("DELETE FROM execution_authorizations WHERE execution_id = $1", execID)
		db.Exec("DELETE FROM incident_recommendations WHERE recommendation_id = $1", recID)
		db.Exec("DELETE FROM incidents WHERE incident_id = $1", incID)
	}()

	// Setup
	_, err = db.Exec("INSERT INTO incidents (incident_id, fingerprint, status, first_seen, last_seen) VALUES ($1, $2, 'OPEN', NOW(), NOW())", incID, incID)
	if err != nil { t.Fatal(err) }
	_, err = db.Exec("INSERT INTO incident_recommendations (recommendation_id, incident_id, target_asset_id, action, priority, reason, status, evidence, fingerprint, model_version) VALUES ($1, $2, 'a', 'STOP', 'HIGH', 'r', 'APPROVED', '{}', $3, '1.0')", recID, incID, recID)
	if err != nil { t.Fatal(err) }
	_, err = db.Exec("INSERT INTO execution_authorizations (execution_id, recommendation_id, approver_id, status) VALUES ($1, $2, 'admin', 'EXECUTING')", execID, recID)
	if err != nil { t.Fatal(err) }

	var wg sync.WaitGroup
	wg.Add(2)
	
	// Mock Abandon (Tx1)
	go func() {
		defer wg.Done()
		tx, err := db.BeginTx(context.Background(), nil)
		if err != nil { t.Errorf("Tx1 begin failed: %v", err); return }
		
		var s string
		err = tx.QueryRow("SELECT status FROM execution_authorizations WHERE recommendation_id = $1 LIMIT 1 FOR UPDATE", recID).Scan(&s)
		if err != nil { t.Errorf("Tx1 lock failed: %v", err); tx.Rollback(); return }
		
		time.Sleep(200 * time.Millisecond) // Give Tx2 a chance to get blocked by our FOR UPDATE lock
		
		_, err = tx.Exec("UPDATE incident_recommendations SET status = 'DISMISSED' WHERE recommendation_id = $1", recID)
		if err != nil { t.Errorf("Tx1 update failed: %v", err) }
		tx.Commit()
	}()

	// Mock Executor (Tx2)
	go func() {
		defer wg.Done()
		time.Sleep(50 * time.Millisecond) // Let Tx1 get the FOR UPDATE lock first
		
		tx2, err := db.BeginTx(context.Background(), nil)
		if err != nil { t.Errorf("Tx2 begin failed: %v", err); return }
		
		_, err = tx2.Exec("UPDATE execution_authorizations SET status = 'EXECUTED', updated_at = NOW() WHERE execution_id = $1", execID)
		if err != nil { t.Errorf("Tx2 update failed: %v", err); tx2.Rollback(); return }
		
		_, err = tx2.Exec("INSERT INTO execution_results (execution_id, recommendation_id, status) VALUES ($1, $2, 'EXECUTED')", execID, recID)
		if err != nil { t.Errorf("Tx2 insert failed: %v", err); tx2.Rollback(); return }
		
		tx2.Commit()
	}()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success, no deadlock!
	case <-time.After(5 * time.Second):
		t.Fatal("Deadlock occurred!")
	}
}
