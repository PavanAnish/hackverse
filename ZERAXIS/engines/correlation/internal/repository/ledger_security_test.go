package repository_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pavanvanish/zeraxis/engines/correlation/internal/repository"
)

func TestPhase77_LedgerImmutability(t *testing.T) {
	repo, err := repository.SetupTestDB(t)
	if err != nil {
		t.Skipf("DB not available: %v", err)
	}
	defer repo.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	incID, recID, authID, execID := createLineage(t, repo, "OPEN", "PROPOSED", "APPROVED", "")
	_ = incID
	_ = recID
	_ = authID

	transID := uuid.New().String()
	_, err = repo.DB().ExecContext(ctx, `
		INSERT INTO execution_state_transitions (transition_id, execution_id, from_status, to_status, actor_type, actor_id, timestamp, reason)
		VALUES ($1, $2, 'PROPOSED', 'APPROVED', 'human', 'human-1', NOW(), 'Initial')
	`, transID, execID)
	if err != nil {
		t.Fatalf("Failed to insert ledger row: %v", err)
	}

	// 1. Attempt UPDATE
	_, err = repo.DB().ExecContext(ctx, `UPDATE execution_state_transitions SET reason = 'tampered' WHERE transition_id = $1`, transID)
	if err == nil {
		t.Fatalf("SECURITY FAILURE: UPDATE on execution_state_transitions succeeded!")
	}
	if !strings.Contains(err.Error(), "prohibited") {
		t.Fatalf("Expected append-only trigger rejection, got: %v", err)
	}

	// 2. Attempt DELETE
	_, err = repo.DB().ExecContext(ctx, `DELETE FROM execution_state_transitions WHERE transition_id = $1`, transID)
	if err == nil {
		t.Fatalf("SECURITY FAILURE: DELETE on execution_state_transitions succeeded!")
	}
	if !strings.Contains(err.Error(), "prohibited") {
		t.Fatalf("Expected append-only trigger rejection, got: %v", err)
	}
}
