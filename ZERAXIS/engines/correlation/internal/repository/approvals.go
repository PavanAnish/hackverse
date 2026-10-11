package repository

import (
	"context"
	"database/sql"
	"errors"
	"github.com/google/uuid"
)

// ApproveRecommendation establishes the required Phase 7.2 Human Approval workflow.
func (r *Repository) ApproveRecommendation(ctx context.Context, recommendationID, approverID string) (string, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	// 1. Verify recommendation exists and is PROPOSED
	var status string
	err = tx.QueryRowContext(ctx, `SELECT status FROM incident_recommendations WHERE recommendation_id = $1 FOR UPDATE`, recommendationID).Scan(&status)
	if err != nil {
		return "", errors.New("recommendation not found")
	}
	if status != "PROPOSED" {
		return "", errors.New("only PROPOSED recommendations can be approved")
	}

	// 2. Transition recommendation to APPROVED
	_, err = tx.ExecContext(ctx, `UPDATE incident_recommendations SET status = 'APPROVED', updated_at = NOW() WHERE recommendation_id = $1`, recommendationID)
	if err != nil {
		return "", err
	}

	// 3. Create execution authorization
	execID := uuid.New().String()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO execution_authorizations (execution_id, recommendation_id, approver_id, status, approved_at, created_at, updated_at)
		VALUES ($1, $2, $3, 'APPROVED', NOW(), NOW(), NOW())
	`, execID, recommendationID, approverID)
	if err != nil {
		return "", err
	}

	// 4. Ledger: (NULL) -> APPROVED
	transitionID := uuid.New().String()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO execution_state_transitions (transition_id, execution_id, from_status, to_status, actor_type, actor_id, timestamp, reason)
		VALUES ($1, $2, NULL, 'APPROVED', 'human', $3, NOW(), 'Human approval')
	`, transitionID, execID, approverID)
	if err != nil {
		return "", err
	}

	return execID, tx.Commit()
}

// RevokeRecommendation revokes an existing approval, ensuring Executor will DENY.
func (r *Repository) RevokeRecommendation(ctx context.Context, recommendationID string) error {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `UPDATE incident_recommendations SET status = 'PROPOSED', updated_at = NOW() WHERE recommendation_id = $1`, recommendationID)
	if err != nil {
		return err
	}

	// Revoke in authorizations table
	var execID string
	err = tx.QueryRowContext(ctx, `
		UPDATE execution_authorizations 
		SET status = 'REVOKED', updated_at = NOW() 
		WHERE recommendation_id = $1 AND status = 'APPROVED'
		RETURNING execution_id
	`, recommendationID).Scan(&execID)
	
	if err == nil {
		// Insert ledger entry for the revoked authorization
		transitionID := uuid.New().String()
		_, err = tx.ExecContext(ctx, `
			INSERT INTO execution_state_transitions (transition_id, execution_id, from_status, to_status, actor_type, actor_id, timestamp, reason)
			VALUES ($1, $2, 'APPROVED', 'REVOKED', 'human', 'system', NOW(), 'Approval revoked')
		`, transitionID, execID)
		if err != nil {
			return err
		}
	} else if err != sql.ErrNoRows {
		return err
	}

	return tx.Commit()
}
