package approvals

import (
	pkgevents "github.com/pavanvanish/zeraxis/pkg/events"
	"time"
	"context"
	"database/sql"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
)

// ErrDispatchFailed is returned when the DB approval committed but NATS dispatch failed.
// The executionID is still valid and the authorization row exists.
var ErrDispatchFailed = errors.New("authorization committed but NATS dispatch failed")

// ErrDispatchUnavailable is returned when nc == nil, meaning no transport is configured.
var ErrDispatchUnavailable = errors.New("authorization committed but NATS transport unavailable")

type Service struct {
	db *sql.DB
	nc *nats.Conn
}

func NewService(db *sql.DB, nc *nats.Conn) *Service {
	return &Service{db: db, nc: nc}
}

// ApproveAndPublish executes the PROPOSED → APPROVED state transition and dispatches the
// authorization event to NATS.
//
// IMPORTANT — Dual-Write Semantics:
//
//	The database commit (recommendation APPROVED + execution_authorizations row) is performed
//	first and is INDEPENDENT of the NATS publish. Core NATS provides no transactional guarantee.
//	A crash or publish failure after DB commit leaves an APPROVED authorization row with no
//	dispatched transport signal. The caller must distinguish these outcomes:
//
//	  execID, nil              — committed AND dispatched
//	  execID, ErrDispatchFailed   — committed, NATS publish failed
//	  execID, ErrDispatchUnavailable — committed, no NATS transport configured
//	  "",    err              — failed before commit (DB error)
//
// No JetStream or Outbox pattern is introduced. Delivery is NOT guaranteed or exactly-once.
func (s *Service) ApproveAndPublish(ctx context.Context, recommendationID, approverID string) (string, error) {
	// FIX 4: Validate approver identity before any state change.
	if strings.TrimSpace(approverID) == "" {
		return "", errors.New("approver identity is required and must not be empty")
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return "", fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Enforce Phase 7.1 state machine: only PROPOSED may transition to APPROVED.
	var currentStatus string
	err = tx.QueryRowContext(ctx,
		`SELECT status FROM incident_recommendations WHERE recommendation_id = $1 FOR UPDATE`,
		recommendationID).Scan(&currentStatus)
	if err != nil {
		return "", errors.New("recommendation not found")
	}
	if currentStatus != "PROPOSED" {
		return "", fmt.Errorf("only PROPOSED recommendations can be approved, current status: %s", currentStatus)
	}

	if _, err = tx.ExecContext(ctx,
		`UPDATE incident_recommendations SET status = 'APPROVED', updated_at = NOW() WHERE recommendation_id = $1`,
		recommendationID); err != nil {
		return "", fmt.Errorf("update recommendation: %w", err)
	}

	execID := uuid.New().String()
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO execution_authorizations
		  (execution_id, recommendation_id, approver_id, status, approved_at, created_at, updated_at)
		VALUES ($1, $2, $3, 'APPROVED', NOW(), NOW(), NOW())
	`, execID, recommendationID, approverID); err != nil {
		return "", fmt.Errorf("insert execution_authorization: %w", err)
	}

	
	// Phase 7.7: Append ledger entry for human approval
	transID := uuid.New().String()
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO execution_state_transitions (transition_id, execution_id, from_status, to_status, actor_type, actor_id, timestamp, reason)
		VALUES ($1, $2, 'PROPOSED', 'APPROVED', 'human', $3, NOW(), 'Human authorized execution')
	`, transID, execID, approverID); err != nil {
		return "", fmt.Errorf("insert ledger entry: %w", err)
	}

if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit: %w", err)
	}

	// DB committed. Now publish to NATS. This is non-atomic with the DB commit.
	// FIX 3: return execID AND a clear dispatch error — caller can distinguish outcomes.
	if s.nc == nil {
		log.Printf("WARNING: NATS transport unavailable for execution_id=%s; authorization committed but not dispatched", execID)
		return execID, ErrDispatchUnavailable
	}

	payload, _ := json.Marshal(map[string]string{"execution_id": execID})
	if pubErr := s.nc.Publish("zeraxis.events.execution.authorized", payload); pubErr != nil {
		log.Printf("CRITICAL: NATS publish failed for execution_id=%s; authorization committed in DB but transport dispatch failed: %v", execID, pubErr)
		return execID, fmt.Errorf("%w: %v", ErrDispatchFailed, pubErr)
	}

	return execID, nil
}


// Revoke transitions a stalled APPROVED authorization to REVOKED, and resets the recommendation to PROPOSED.
func (s *Service) Revoke(ctx context.Context, recommendationID, approverID string) error {
	if strings.TrimSpace(approverID) == "" {
		return errors.New("approver identity is required and must not be empty")
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	var currentRecStatus string
	err = tx.QueryRowContext(ctx,
		`SELECT status FROM incident_recommendations WHERE recommendation_id = $1 FOR UPDATE`,
		recommendationID).Scan(&currentRecStatus)
	if err != nil {
		return errors.New("recommendation not found")
	}

	if currentRecStatus != "APPROVED" {
		return fmt.Errorf("only APPROVED recommendations can be revoked, current status: %s", currentRecStatus)
	}

	var execID, currentAuthStatus string
	err = tx.QueryRowContext(ctx,
		`SELECT execution_id, status FROM execution_authorizations WHERE recommendation_id = $1 ORDER BY created_at DESC LIMIT 1 FOR UPDATE`,
		recommendationID).Scan(&execID, &currentAuthStatus)
	if err != nil {
		return errors.New("execution authorization not found")
	}

	if currentAuthStatus != "APPROVED" {
		return fmt.Errorf("authorization cannot be revoked, current status: %s", currentAuthStatus)
	}

	if _, err = tx.ExecContext(ctx,
		`UPDATE incident_recommendations SET status = 'PROPOSED', updated_at = NOW() WHERE recommendation_id = $1`,
		recommendationID); err != nil {
		return fmt.Errorf("update recommendation: %w", err)
	}

	if _, err = tx.ExecContext(ctx,
		`UPDATE execution_authorizations SET status = 'REVOKED', updated_at = NOW() WHERE execution_id = $1`,
		execID); err != nil {
		return fmt.Errorf("update authorization: %w", err)
	}

	transID := uuid.New().String()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO execution_state_transitions (transition_id, execution_id, from_status, to_status, actor_type, actor_id, timestamp, reason)
		VALUES ($1, $2, 'APPROVED', 'REVOKED', 'human', $3, NOW(), 'Human revoked stalled authorization')
	`, transID, execID, approverID)
	if err != nil {
		return fmt.Errorf("insert ledger entry: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	// Publish wake-up signal (best effort)
	if s.nc != nil {
		payload, _ := json.Marshal(map[string]string{"execution_id": execID})
		_ = s.nc.Publish("zeraxis.events.execution.revoked", payload)
	}

	return nil
}

// Abandon transitions an EXECUTING authorization to ABANDONED, and the recommendation to MANUAL_INVESTIGATION_REQUIRED.
func (s *Service) Abandon(ctx context.Context, recommendationID, approverID string) error {
	if strings.TrimSpace(approverID) == "" {
		return errors.New("approver identity is required and must not be empty")
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	var currentRecStatus string
	err = tx.QueryRowContext(ctx,
		`SELECT status FROM incident_recommendations WHERE recommendation_id = $1 FOR UPDATE`,
		recommendationID).Scan(&currentRecStatus)
	if err != nil {
		return errors.New("recommendation not found")
	}

	if currentRecStatus != "APPROVED" {
		return fmt.Errorf("only APPROVED recommendations can be abandoned, current status: %s", currentRecStatus)
	}

	var execID, currentAuthStatus string
	err = tx.QueryRowContext(ctx,
		`SELECT execution_id, status FROM execution_authorizations WHERE recommendation_id = $1 ORDER BY created_at DESC LIMIT 1 FOR UPDATE`,
		recommendationID).Scan(&execID, &currentAuthStatus)
	if err != nil {
		return errors.New("execution authorization not found")
	}

	if currentAuthStatus != "EXECUTING" {
		return fmt.Errorf("only EXECUTING authorizations can be abandoned, current status: %s", currentAuthStatus)
	}

	if _, err = tx.ExecContext(ctx,
		`UPDATE incident_recommendations SET status = 'MANUAL_INVESTIGATION_REQUIRED', updated_at = NOW() WHERE recommendation_id = $1`,
		recommendationID); err != nil {
		return fmt.Errorf("update recommendation: %w", err)
	}

	if _, err = tx.ExecContext(ctx,
		`UPDATE execution_authorizations SET status = 'ABANDONED', updated_at = NOW() WHERE execution_id = $1`,
		execID); err != nil {
		return fmt.Errorf("update authorization: %w", err)
	}

	transID := uuid.New().String()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO execution_state_transitions (transition_id, execution_id, from_status, to_status, actor_type, actor_id, timestamp, reason)
		VALUES ($1, $2, 'EXECUTING', 'ABANDONED', 'human', $3, NOW(), 'Human abandoned uncertain execution')
	`, transID, execID, approverID)
	if err != nil {
		return fmt.Errorf("insert ledger entry: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	return nil
}

// ResolveInvestigation handles human resolution of MANUAL_INVESTIGATION_REQUIRED quarantines.
func (s *Service) ResolveInvestigation(ctx context.Context, recommendationID, operatorID, outcome, reason string) error {
	if strings.TrimSpace(operatorID) == "" {
		return errors.New("operator identity is required")
	}
	if outcome != "OUTCOME_MANUALLY_REMEDIATED" && outcome != "OUTCOME_SAFE_TO_RETRY" {
		return errors.New("invalid outcome")
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// 1. SELECT FOR UPDATE
	var currentStatus, incID, action, priority, rReason, modelVersion string
	var evidence []byte
	var tAsset, tContainer, tDigest sql.NullString

	err = tx.QueryRowContext(ctx, `
		SELECT status, incident_id, action, priority, reason, evidence, model_version, target_asset_id, target_container_id, target_image_digest
		FROM incident_recommendations
		WHERE recommendation_id = $1 FOR UPDATE
	`, recommendationID).Scan(&currentStatus, &incID, &action, &priority, &rReason, &evidence, &modelVersion, &tAsset, &tContainer, &tDigest)
	
	if err != nil {
		return errors.New("recommendation not found")
	}

	// 2. Verify state
	if currentStatus != "MANUAL_INVESTIGATION_REQUIRED" {
		return fmt.Errorf("only MANUAL_INVESTIGATION_REQUIRED can be resolved, current status: %s", currentStatus)
	}

	// Lock the incident as well if remediating
	if outcome == "OUTCOME_MANUALLY_REMEDIATED" {
		var incStatus string
		tx.QueryRowContext(ctx, `SELECT status FROM incidents WHERE incident_id = $1 FOR UPDATE`, incID).Scan(&incStatus)
	}

	// 3/4. Insert human attestation (copy authoritative target identity)
	attestID := uuid.New().String()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO human_remediation_attestations (attestation_id, recommendation_id, incident_id, operator_id, outcome, reason, target_asset_id, target_container_id, target_image_digest)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, attestID, recommendationID, incID, operatorID, outcome, reason, tAsset, tContainer, tDigest)
	if err != nil {
		return fmt.Errorf("insert attestation: %w", err)
	}

	var newRecID string
	if outcome == "OUTCOME_MANUALLY_REMEDIATED" {
		// Transition to MANUALLY_REMEDIATED
		_, err = tx.ExecContext(ctx, `UPDATE incident_recommendations SET status = 'MANUALLY_REMEDIATED', updated_at = NOW() WHERE recommendation_id = $1`, recommendationID)
		if err != nil {
			return err
		}
		
		// Update incident to RESOLVED
		_, err = tx.ExecContext(ctx, `UPDATE incidents SET status = 'RESOLVED', resolved_at = NOW(), updated_at = NOW() WHERE incident_id = $1`, incID)
		if err != nil {
			return err
		}
	} else if outcome == "OUTCOME_SAFE_TO_RETRY" {
		// Transition to SUPERSEDED
		_, err = tx.ExecContext(ctx, `UPDATE incident_recommendations SET status = 'SUPERSEDED', updated_at = NOW() WHERE recommendation_id = $1`, recommendationID)
		if err != nil {
			return err
		}
		
		// Create new fresh PROPOSED recommendation
		newRecID = uuid.New().String()
		bucketStr := time.Now().UTC().Format("2006-01-02-15")
		rawFP := fmt.Sprintf("%s:%s:%s", tAsset.String, tDigest.String, bucketStr)
		h := sha256.Sum256([]byte(rawFP))
		fingerprint := fmt.Sprintf("%x", h)
		_, err = tx.ExecContext(ctx, `
			INSERT INTO incident_recommendations (recommendation_id, incident_id, fingerprint, action, status, priority, reason, evidence, model_version, target_asset_id, target_container_id, target_image_digest)
			VALUES ($1, $2, $3, $4, 'PROPOSED', $5, $6, $7, $8, $9, $10, $11)
		`, newRecID, incID, fingerprint, action, priority, rReason, evidence, modelVersion, tAsset, tContainer, tDigest)
		if err != nil {
			return fmt.Errorf("insert new recommendation: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	// 8. Publish NATS Event
	if outcome == "OUTCOME_MANUALLY_REMEDIATED" {
		evt := pkgevents.NewEvent("INCIDENT_RESOLVED", pkgevents.SourceCorrelation, time.Now().UTC())
		evt.Subject = "zeraxis.events.correlation.incident.resolved"
		if tAsset.Valid { evt.AssetID = tAsset.String }
		if tDigest.Valid { evt.ImageDigest = tDigest.String }
		
		meta := map[string]interface{}{
			"incident_id": incID,
			"recommendation_id": recommendationID,
			"resolution_type": "HUMAN_ATTESTATION",
			"operator_id": operatorID,
			"reason": reason,
		}
		b, _ := json.Marshal(meta)
		evt.Metadata = b
		publishEvent(s.nc, evt)
	} else if outcome == "OUTCOME_SAFE_TO_RETRY" {
		evt := pkgevents.NewEvent(pkgevents.EventTypeRecommendationCreated, pkgevents.SourceCorrelation, time.Now().UTC())
		evt.Subject = "zeraxis.events.correlation.recommendation.created"
		if tAsset.Valid { evt.AssetID = tAsset.String }
		if tDigest.Valid { evt.ImageDigest = tDigest.String }
		
		meta := map[string]interface{}{
			"incident_id": incID,
			"recommendation_id": newRecID,
			"superseded_recommendation_id": recommendationID,
			"operator_id": operatorID,
		}
		b, _ := json.Marshal(meta)
		evt.Metadata = b
		publishEvent(s.nc, evt)
	}

	return nil
}

func publishEvent(nc *nats.Conn, evt *pkgevents.Event) {
	if nc == nil {
		return
	}
	b, _ := json.Marshal(evt)
	nc.Publish(evt.Subject, b)
}
