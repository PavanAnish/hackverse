package approvals_test

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	"github.com/pavanvanish/zeraxis/engines/correlation/pkg/approvals"
)

func TestConcurrency_Abandon_Vs_Tx2(t *testing.T) {
	db := getDB(t)
	if db == nil {
		return // DB not available
	}
	defer db.Close()

	_, recID := setupRec(t, db, "APPROVED")
	execID := setupExec(t, db, recID, "EXECUTING") // Simulate Executor running

	svc := approvals.NewService(db, nil)
	var wg sync.WaitGroup
	wg.Add(2)

	// We will simulate Tx2 and Abandon running concurrently.
	// We'll just run them at the exact same time and assert the deterministic outcome.
	// PostgreSQL SELECT FOR UPDATE will serialize them.
	
	var abandonErr error
	var tx2Err error

	go func() {
		defer wg.Done()
		abandonErr = svc.Abandon(context.Background(), recID, "admin")
	}()

	go func() {
		defer wg.Done()
		// Simulate Executor Tx2
		tx, err := db.BeginTx(context.Background(), &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if err != nil {
			tx2Err = err
			return
		}
		defer tx.Rollback()

		var status string
		err = tx.QueryRow(`SELECT status FROM execution_authorizations WHERE execution_id = $1 FOR UPDATE`, execID).Scan(&status)
		if err != nil {
			tx2Err = err
			return
		}

		if status != "EXECUTING" && status != "ABANDONED" {
			tx2Err = err
			return
		}
		
		time.Sleep(50 * time.Millisecond) // artificially hold lock slightly to force race serialization

		_, _ = tx.Exec(`UPDATE execution_authorizations SET status = 'EXECUTED' WHERE execution_id = $1`, execID)
		_, _ = tx.Exec(`INSERT INTO execution_results (execution_id, recommendation_id, status) VALUES ($1, $2, 'EXECUTED')`, execID, recID)
		_, _ = tx.Exec(`INSERT INTO execution_state_transitions (transition_id, execution_id, from_status, to_status, actor_type, actor_id) VALUES (gen_random_uuid(), $1, $2, 'EXECUTED', 'system', 'executor')`, execID, status)
		tx2Err = tx.Commit()
	}()

	wg.Wait()

	// Analyze the serialized result
	var finalAuthStatus string
	db.QueryRow(`SELECT status FROM execution_authorizations WHERE execution_id = $1`, execID).Scan(&finalAuthStatus)

	if abandonErr == nil && tx2Err == nil {
		// Abandon won the lock first (auth became ABANDONED), then Tx2 acquired the lock, read ABANDONED, and updated to EXECUTED.
		if finalAuthStatus != "EXECUTED" {
			t.Fatalf("Expected EXECUTED, got %s", finalAuthStatus)
		}
		var ledgerCount int
		db.QueryRow(`SELECT COUNT(*) FROM execution_state_transitions WHERE execution_id = $1 AND from_status = 'ABANDONED' AND to_status = 'EXECUTED'`, execID).Scan(&ledgerCount)
		if ledgerCount != 1 {
			t.Fatalf("Expected ledger row for ABANDONED -> EXECUTED")
		}
	} else if abandonErr != nil && tx2Err == nil {
		// Tx2 won the lock first. Auth became EXECUTED. Abandon failed with 409 because auth was no longer EXECUTING.
		if finalAuthStatus != "EXECUTED" {
			t.Fatalf("Expected EXECUTED, got %s", finalAuthStatus)
		}
	} else {
		t.Fatalf("Unexpected error combination: Abandon=%v, Tx2=%v", abandonErr, tx2Err)
	}
}
