package repository

import (
	"context"
	"database/sql"
	"errors"
)

// ResolveIncidentFromExecution processes a verified execution event and atomically transitions
// the exact associated recommendation to EXECUTED and the originating incident to RESOLVED.
func (r *Repository) ResolveIncidentFromExecution(ctx context.Context, executionID string) (string, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	// Authoritative DB Lineage Lookup & Validation
	// 1. execution exists (execution_results)
	// 2. execution is terminal EXECUTED (execution_results OR execution_authorizations)
	// 3. verification_status = VERIFIED
	// 4. authorization exists and corresponds to execution
	// 5. recommendation exists and corresponds to authorization
	// 6. incident exists and recommendation belongs to it
	// 7. Lock the incident for concurrency.
	
	var incidentID, recID, incStatus, recStatus string
	err = tx.QueryRowContext(ctx, `
		SELECT i.incident_id, r.recommendation_id, i.status, r.status
		FROM execution_results er
		JOIN execution_authorizations ea ON er.execution_id = ea.execution_id
		JOIN incident_recommendations r ON ea.recommendation_id = r.recommendation_id
		JOIN incidents i ON r.incident_id = i.incident_id
		WHERE er.execution_id = $1
		  AND er.verification_status = 'VERIFIED'
		  AND ea.status = 'EXECUTED'
		FOR UPDATE OF i, r
	`, executionID).Scan(&incidentID, &recID, &incStatus, &recStatus)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", errors.New("DENY: Invalid execution lineage or not verified")
		}
		return "", err
	}

	if incStatus == "CLOSED" || incStatus == "RESOLVED" {
		if incStatus == "CLOSED" {
			return "", errors.New("DENY: Incident is already CLOSED (fail closed)")
		}
		// If RESOLVED, it's an idempotent no-op
		return "", nil
	}

	if incStatus != "OPEN" && incStatus != "ACTIVE" {
		return "", errors.New("DENY: Unexpected incident state")
	}

	// It's possible that multiple recommendations exist, but we only mutate the EXACT recommendation.
	if recStatus == "EXECUTED" {
		// Just in case it was already marked but incident was not resolved?
		// We still proceed to resolve incident if it is OPEN/ACTIVE.
	} else if recStatus != "APPROVED" && recStatus != "MANUAL_INVESTIGATION_REQUIRED" {
		return "", errors.New("DENY: Recommendation is not APPROVED")
	} else {
		// Transition Recommendation to EXECUTED
		_, err = tx.ExecContext(ctx, `
			UPDATE incident_recommendations 
			SET status = 'EXECUTED', updated_at = NOW() 
			WHERE recommendation_id = $1
		`, recID)
		if err != nil {
			return "", err
		}
	}

	// Transition Incident to RESOLVED
	_, err = tx.ExecContext(ctx, `
		UPDATE incidents 
		SET status = 'RESOLVED', updated_at = NOW(), resolved_at = NOW() 
		WHERE incident_id = $1
	`, incidentID)
	if err != nil {
		return "", err
	}

	if err := tx.Commit(); err != nil {
		return "", err
	}

	return incidentID, nil
}
