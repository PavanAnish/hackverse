package breakglass

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

var (
	ErrExpired      = errors.New("override request has expired")
	ErrNotPending   = errors.New("override request is not in PENDING state")
	ErrInvalidSig   = errors.New("invalid HMAC signature")
	ErrCannotRevoke = errors.New("cannot revoke approval after threshold is met")
	ErrNotApproved  = errors.New("override request is not in APPROVED state")
	ErrUserBlocked  = errors.New("user is blocked from requesting overrides due to policy abuse")
)

type OverrideService struct {
	db        *sql.DB
	validator *HMACValidator
	secret    string
	detector  *ElevationDetector
}

func NewOverrideService(db *sql.DB, validator *HMACValidator, secret string, detector *ElevationDetector) *OverrideService {
	return &OverrideService{
		db:        db,
		validator: validator,
		secret:    secret,
		detector:  detector,
	}
}

func (s *OverrideService) RequestOverride(ctx context.Context, fingerprint, requestedBy, reason, clusterID string) (string, error) {
	// Check if user is blocked
	var blocked bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM shield_blocked_users WHERE user_id = $1)`, requestedBy).Scan(&blocked)
	if err == nil && blocked {
		return "", ErrUserBlocked
	}

	reqID := uuid.New().String()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, 
		`INSERT INTO override_requests (request_id, fingerprint, requested_by, reason, cluster_id, status, approval_threshold) 
		 VALUES ($1, $2, $3, $4, $5, 'PENDING', 2)`,
		reqID, fingerprint, requestedBy, reason, clusterID)
	if err != nil {
		return "", err
	}

	s.auditLog(ctx, tx, "OVERRIDE_REQUESTED", requestedBy, clusterID, map[string]interface{}{
		"request_id":  reqID,
		"fingerprint": fingerprint,
		"reason":      reason,
	})

	if err := tx.Commit(); err != nil {
		return "", err
	}

	// Trigger elevation check asynchronously or synchronously
	if s.detector != nil {
		s.detector.CheckAndElevate(ctx, requestedBy)
	}

	return reqID, nil
}

func (s *OverrideService) ApproveOverride(ctx context.Context, requestID, approverID, hmacSignature string) (int, bool, error) {
	if !s.validator.ValidateSignature(requestID, approverID, hmacSignature, s.secret) {
		return 0, false, ErrInvalidSig
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback()

	var status, clusterID string
	var threshold, count int
	var expiresAt time.Time

	err = tx.QueryRowContext(ctx, 
		`SELECT status, approval_threshold, approval_count, expires_at, cluster_id 
		 FROM override_requests WHERE request_id = $1 FOR UPDATE`, requestID).
		Scan(&status, &threshold, &count, &expiresAt, &clusterID)
	
	if err != nil {
		return 0, false, err
	}

	if time.Now().After(expiresAt) {
		tx.ExecContext(ctx, `UPDATE override_requests SET status = 'EXPIRED' WHERE request_id = $1`, requestID)
		tx.Commit()
		return count, false, ErrExpired
	}

	if status != "PENDING" {
		return count, false, ErrNotPending
	}

	approvalID := uuid.New().String()
	_, err = tx.ExecContext(ctx, 
		`INSERT INTO override_approvals (approval_id, override_request_id, approver_id, signature_hmac) 
		 VALUES ($1, $2, $3, $4)`,
		approvalID, requestID, approverID, hmacSignature)
	if err != nil {
		return count, false, fmt.Errorf("duplicate or invalid approval: %v", err)
	}

	newCount := count + 1
	newStatus := "PENDING"
	thresholdMet := newCount >= threshold
	if thresholdMet {
		newStatus = "APPROVED"
	}

	_, err = tx.ExecContext(ctx, 
		`UPDATE override_requests SET approval_count = $1, status = $2 WHERE request_id = $3`,
		newCount, newStatus, requestID)
	if err != nil {
		return 0, false, err
	}

	s.auditLog(ctx, tx, "APPROVAL_GRANTED", approverID, clusterID, map[string]interface{}{
		"request_id":  requestID,
		"approval_id": approvalID,
		"new_count":   newCount,
	})

	if thresholdMet {
		s.auditLog(ctx, tx, "OVERRIDE_GRANTED", "SYSTEM", clusterID, map[string]interface{}{
			"request_id": requestID,
		})
	}

	if err := tx.Commit(); err != nil {
		return 0, false, err
	}

	return newCount, thresholdMet, nil
}

func (s *OverrideService) RevokeApproval(ctx context.Context, approvalID, approverID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var reqID, clusterID string
	err = tx.QueryRowContext(ctx, 
		`SELECT a.override_request_id, r.cluster_id 
		 FROM override_approvals a 
		 JOIN override_requests r ON a.override_request_id = r.request_id 
		 WHERE a.approval_id = $1 AND a.approver_id = $2 FOR UPDATE`, approvalID, approverID).Scan(&reqID, &clusterID)
	if err != nil {
		return err
	}

	var status string
	var count int
	err = tx.QueryRowContext(ctx, `SELECT status, approval_count FROM override_requests WHERE request_id = $1 FOR UPDATE`, reqID).Scan(&status, &count)
	if err != nil {
		return err
	}

	if status != "PENDING" {
		return ErrCannotRevoke
	}

	_, err = tx.ExecContext(ctx, 
		`UPDATE override_approvals SET revoked_at = NOW(), can_revoke = FALSE WHERE approval_id = $1`, approvalID)
	if err != nil {
		return fmt.Errorf("failed to revoke: %v", err)
	}

	_, err = tx.ExecContext(ctx, 
		`UPDATE override_requests SET approval_count = approval_count - 1 WHERE request_id = $1`, reqID)
	if err != nil {
		return err
	}

	s.auditLog(ctx, tx, "REVOCATION", approverID, clusterID, map[string]interface{}{
		"request_id":  reqID,
		"approval_id": approvalID,
	})

	return tx.Commit()
}

func (s *OverrideService) ExecuteOverride(ctx context.Context, requestID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var status, clusterID string
	var expiresAt time.Time
	err = tx.QueryRowContext(ctx, `SELECT status, expires_at, cluster_id FROM override_requests WHERE request_id = $1 FOR UPDATE`, requestID).Scan(&status, &expiresAt, &clusterID)
	if err != nil {
		return err
	}

	if time.Now().After(expiresAt) {
		tx.ExecContext(ctx, `UPDATE override_requests SET status = 'EXPIRED' WHERE request_id = $1`, requestID)
		tx.Commit()
		return ErrExpired
	}

	if status != "APPROVED" {
		return ErrNotApproved
	}

	_, err = tx.ExecContext(ctx, `UPDATE override_requests SET status = 'EXECUTED', executed_at = NOW() WHERE request_id = $1`, requestID)
	if err != nil {
		return err
	}

	s.auditLog(ctx, tx, "OVERRIDE_EXECUTED", "SYSTEM", clusterID, map[string]interface{}{
		"request_id": requestID,
	})

	return tx.Commit()
}

func (s *OverrideService) GetOverrideStatus(ctx context.Context, requestID string) (string, int, error) {
	var status string
	var count, threshold int
	err := s.db.QueryRowContext(ctx, `SELECT status, approval_count, approval_threshold FROM override_requests WHERE request_id = $1`, requestID).Scan(&status, &count, &threshold)
	if err != nil {
		return "", 0, err
	}
	needed := threshold - count
	if needed < 0 {
		needed = 0
	}
	return status, needed, nil
}

func (s *OverrideService) auditLog(ctx context.Context, tx *sql.Tx, eventType, actorID, clusterID string, details map[string]interface{}) {
	b, _ := json.Marshal(details)
	tx.ExecContext(ctx, 
		`INSERT INTO policy_audit_trail (entry_id, event_type, actor_id, cluster_id, details) VALUES ($1, $2, $3, $4, $5)`,
		uuid.New().String(), eventType, actorID, clusterID, b)
}
