package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/pavanvanish/zeraxis/engines/executor/internal/docker"
	"github.com/pavanvanish/zeraxis/pkg/events"
)

// terminalContainerStates lists the Docker container states that satisfy the
// STOP_CONTAINER postcondition. Any other state is treated as VERIFICATION_FAILED.
var terminalContainerStates = map[string]bool{
	"exited": true,
	"dead":   true,
}

// verificationResult holds the outcome of a post-mutation Docker verification pass.
type verificationResult struct {
	// status is either "VERIFIED" or "FAILED".
	status string
	// reason is recorded only when status is "FAILED".
	reason string
}

// Executor is the Phase 7.2/7.3 execution engine.
type Executor struct {
	db     *sql.DB
	docker docker.Client
	nc     *nats.Conn
}

// AuthEvent is the NATS payload carrying a single execution_id.
type AuthEvent struct {
	ExecutionID string `json:"execution_id"`
}

// NewExecutor returns an Executor backed by the given DB, Docker client, and NATS connection.
// nc may be nil in tests; audit events will be logged but not published.
func NewExecutor(db *sql.DB, dockerClient docker.Client, nc *nats.Conn) *Executor {
	return &Executor{
		db:     db,
		docker: dockerClient,
		nc:     nc,
	}
}

// ─── Audit event emission ─────────────────────────────────────────────────────

// emitAudit publishes a Phase 4 unified audit event to NATS.
// If NATS is unavailable the failure is logged; the DB state is authoritative.
func (e *Executor) emitAudit(eventType, details, assetID, imageDigest string) {
	if e.nc == nil {
		log.Printf("NATS not configured, cannot emit event: %s - %s", eventType, details)
		return
	}

	metadata, _ := json.Marshal(map[string]string{"details": details})

	ev := events.NewEvent(eventType, "executor", time.Now())
	ev.Subject = "zeraxis.events.execution.audit"
	if assetID != "" {
		ev.AssetID = assetID
	}
	ev.ImageDigest = imageDigest
	ev.Metadata = json.RawMessage(metadata)

	b, _ := json.Marshal(ev)
	if err := e.nc.Publish("zeraxis.events.execution.audit", b); err != nil {
		log.Printf("CRITICAL: Failed to publish audit event %s: %v", eventType, err)
	}
}

// ─── NATS entry point ─────────────────────────────────────────────────────────

// HandleNatsWakeup parses a zeraxis.events.execution.authorized NATS payload and
// routes to Execute. Malformed or non-UUID payloads are silently dropped.
func (e *Executor) HandleNatsWakeup(ctx context.Context, payload []byte) {
	var ev AuthEvent
	if err := json.Unmarshal(payload, &ev); err != nil {
		return // malformed payload
	}
	if _, err := uuid.Parse(ev.ExecutionID); err != nil {
		return // invalid uuid
	}

	e.emitAudit("EXECUTION_REQUESTED", fmt.Sprintf("Execution %s requested via NATS", ev.ExecutionID), "", "")
	err := e.Execute(ctx, ev.ExecutionID)
	if err != nil {
		log.Printf("CRITICAL: Execution failed for execution_id %s: %v", ev.ExecutionID, err)
	}
}

// ─── Main execution path ──────────────────────────────────────────────────────

// Execute implements the Phase 7.3 dual-transaction execution and verification model:
//
//	Transaction 1 (lock):
//	  - validate authorization state
//	  - transition APPROVED → EXECUTING
//	  - commit
//
//	Out-of-transaction:
//	  - Docker mutation
//	  - Docker verification (read-only)
//
//	Transaction 2 (result):
//	  - persist execution_authorizations final status
//	  - persist execution_results (status + verification_status)
//	  - commit
//
//	Then:
//	  - emit Phase 4 audit event
//
// Docker mutation and DB persistence are NOT atomic. The crash-window limitation
// from Phase 7.2 is intentionally preserved. No exactly-once semantics are claimed.
func (e *Executor) Execute(ctx context.Context, execID string) error {
	// ── Transaction 1: Claim and lock the authorization ───────────────────
	tx, err := e.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		e.emitAudit("EXECUTION_FAILED", "DB unavailable", "", "")
		return err
	}
	defer tx.Rollback()

	var status, recID string
	var approvedAt time.Time
	err = tx.QueryRowContext(ctx,
		`SELECT status, recommendation_id, approved_at FROM execution_authorizations WHERE execution_id = $1 FOR UPDATE`,
		execID,
	).Scan(&status, &recID, &approvedAt)
	if err != nil {
		e.emitAudit("EXECUTION_ABORTED", "Authorization missing", "", "")
		return errors.New("DENY: authorization missing")
	}

	if status != "APPROVED" {
		e.emitAudit("EXECUTION_ABORTED", "Authorization revoked or already processed", "", "")
		return errors.New("DENY: authorization revoked, expired, or already executed")
	}

	if time.Since(approvedAt) > 1*time.Hour {
		e.emitAudit("EXECUTION_ABORTED", "Approval expired", "", "")
		return errors.New("DENY: approval expired")
	}

	var action, recStatus string
	var tAsset, tContainer, tDigest sql.NullString
	var incidentID string

	err = tx.QueryRowContext(ctx, `
		SELECT action, status, target_asset_id, target_container_id, target_image_digest, incident_id 
		FROM incident_recommendations WHERE recommendation_id = $1
	`, recID).Scan(&action, &recStatus, &tAsset, &tContainer, &tDigest, &incidentID)
	if err != nil {
		e.emitAudit("EXECUTION_ABORTED", "Recommendation missing", "", "")
		return errors.New("DENY: recommendation missing")
	}

	if recStatus != "APPROVED" {
		e.emitAudit("EXECUTION_ABORTED", "Recommendation not APPROVED", "", "")
		return errors.New("DENY: recommendation must be APPROVED")
	}

	var currentRiskLevel string
	err = tx.QueryRowContext(ctx, `SELECT risk_level FROM incidents WHERE incident_id = $1`, incidentID).Scan(&currentRiskLevel)
	if err != nil {
		e.emitAudit("EXECUTION_ABORTED", "Incident missing", tAsset.String, tDigest.String)
		return errors.New("DENY: incident missing")
	}

	if currentRiskLevel != "CRITICAL" && currentRiskLevel != "HIGH" {
		e.emitAudit("EXECUTION_ABORTED", "Insufficient risk", tAsset.String, tDigest.String)
		return errors.New("DENY: current risk level is insufficient for mutation")
	}

	if !tAsset.Valid || tAsset.String == "" || !tContainer.Valid || tContainer.String == "" || !tDigest.Valid || tDigest.String == "" {
		e.emitAudit("EXECUTION_ABORTED", "NULL target fields", "", "")
		return errors.New("DENY: explicit target identity is incomplete (NULL fields detected)")
	}

	// Validate target_image_digest is a well-formed OCI digest (sha256:<64hex>).
	if err := ValidateTargetDigest(tDigest.String); err != nil {
		e.emitAudit("EXECUTION_ABORTED", "target_image_digest is not a valid OCI digest: "+err.Error(), tAsset.String, tDigest.String)
		return fmt.Errorf("DENY: %w", err)
	}

	containerState, err := e.docker.InspectContainer(ctx, tContainer.String)
	if err != nil {
		e.emitAudit("EXECUTION_ABORTED", "Container missing in Docker", tAsset.String, tDigest.String)
		return errors.New("DENY: container missing in docker")
	}

	if !strings.HasPrefix(containerState.ID, tContainer.String) {
		e.emitAudit("EXECUTION_ABORTED", "Container ID mismatch", tAsset.String, tDigest.String)
		return errors.New("DENY: ambiguous or mismatched container ID")
	}

	if len(containerState.RepoDigests) == 0 {
		e.emitAudit("EXECUTION_ABORTED", "Authoritative registry digest unavailable — RepoDigests empty", tAsset.String, tDigest.String)
		return errors.New("DENY: authoritative registry digest unavailable")
	}

	if !RepoDigestsContain(containerState.RepoDigests, tDigest.String) {
		e.emitAudit("EXECUTION_ABORTED", "Image digest mismatch: no RepoDigest canonical digest equals target", tAsset.String, tDigest.String)
		return errors.New("DENY: image digest mismatch (target requires Phase 3 authoritative registry digest)")
	}

	var authoritativeAssetID string
	err = tx.QueryRowContext(ctx, `SELECT asset_id FROM discovery_assets WHERE container_id = $1`, tContainer.String).Scan(&authoritativeAssetID)
	if err != nil {
		e.emitAudit("EXECUTION_ABORTED", "Authoritative asset identity could not be resolved", tAsset.String, tDigest.String)
		return errors.New("DENY: authoritative asset identity could not be resolved")
	}
	if authoritativeAssetID != tAsset.String {
		e.emitAudit("EXECUTION_ABORTED", "Asset mismatch", tAsset.String, tDigest.String)
		return errors.New("DENY: asset identity mismatch")
	}

	var selectedNetwork string
	if action == "QUARANTINE_ASSET" {
		authNet := os.Getenv("AUTHORIZED_QUARANTINE_NETWORK")
		if authNet == "" {
			return errors.New("DENY: AUTHORIZED_QUARANTINE_NETWORK not configured")
		}
		for _, net := range containerState.NetworkIDs {
			if net == authNet {
				selectedNetwork = net
				break
			}
		}
		if selectedNetwork == "" {
			return errors.New("DENY: container not attached to authorized quarantine network")
		}
	}

	// Authoritative incident check & claim
	var latestIncStatus string
	var currentAuthStatus string
	err = tx.QueryRowContext(ctx, `
		SELECT i.status, ea.status 
		FROM execution_authorizations ea
		JOIN incident_recommendations ir ON ea.recommendation_id = ir.recommendation_id
		JOIN incidents i ON ir.incident_id = i.incident_id
		WHERE ea.execution_id = $1 FOR UPDATE OF ea
	`, execID).Scan(&latestIncStatus, &currentAuthStatus)

	if err != nil {
		return errors.New("DENY: failed to fetch authorization state")
	}

	if currentAuthStatus != "APPROVED" {
		return fmt.Errorf("DENY: authorization is not APPROVED (current: %s)", currentAuthStatus)
	}

	if latestIncStatus != "OPEN" && latestIncStatus != "ACTIVE" {
		// Incident closed! Must transition APPROVED -> FAILED and perform zero docker mutations.
		_, err = tx.ExecContext(ctx, `UPDATE execution_authorizations SET status = 'FAILED', updated_at = NOW() WHERE execution_id = $1`, execID)
		if err != nil {
			return err
		}
		// Insert FAILED result
		_, err = tx.ExecContext(ctx, `INSERT INTO execution_results (execution_id, recommendation_id, status, error_log) VALUES ($1, $2, 'FAILED', 'DENY: Incident closed')`, execID, recID)
		if err != nil {
			return err
		}
		// Insert ledger entry
		transID := uuid.New().String()
		_, err = tx.ExecContext(ctx, `INSERT INTO execution_state_transitions (transition_id, execution_id, from_status, to_status, actor_type, actor_id, timestamp, reason) VALUES ($1, $2, 'APPROVED', 'FAILED', 'system', 'executor', NOW(), 'DENY: Incident closed')`, transID, execID)
		if err != nil {
			return err
		}

		if err := tx.Commit(); err != nil {
			return errors.New("DENY: failed to commit failure claim")
		}
		return errors.New("DENY: Incident closed")
	}

	// Transition APPROVED → EXECUTING within Transaction 1.
	_, err = tx.ExecContext(ctx,
		`UPDATE execution_authorizations SET status = 'EXECUTING', updated_at = NOW() WHERE execution_id = $1`,
		execID)
	if err != nil {
		return errors.New("DENY: failed to claim execution lock")
	}

	// Insert ledger entry APPROVED -> EXECUTING
	transID := uuid.New().String()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO execution_state_transitions (transition_id, execution_id, from_status, to_status, actor_type, actor_id, timestamp, reason)
		VALUES ($1, $2, 'APPROVED', 'EXECUTING', 'system', 'executor', NOW(), 'Claimed by executor')
	`, transID, execID)
	if err != nil {
		return errors.New("DENY: failed to write ledger for claim")
	}

	if err := tx.Commit(); err != nil {
		return errors.New("DENY: failed to commit execution claim")
	}

	// Publish EXECUTION_CLAIMED only after successful Tx1 commit.
	e.emitAudit("EXECUTION_CLAIMED", fmt.Sprintf("Execution %s claimed by executor", execID), tAsset.String, tDigest.String)
	e.emitAudit("EXECUTION_STARTED", fmt.Sprintf("Execution %s started", execID), tAsset.String, tDigest.String)

	// ── Docker mutation (outside any DB transaction) ──────────────────────
	//
	// NOTE: Docker mutation and DB persistence are NOT atomic.
	// A crash between here and Transaction 2 leaves the authorization in EXECUTING
	// with no execution_result row. This crash-window is a known limitation and is
	// intentionally NOT resolved in Phase 7.3.
	var mutationErr error
	switch action {
	case "STOP_CONTAINER":
		mutationErr = e.docker.ContainerStop(ctx, tContainer.String)
	case "QUARANTINE_ASSET":
		mutationErr = e.docker.NetworkDisconnect(ctx, tContainer.String, selectedNetwork)
	default:
		mutationErr = errors.New("DENY: unsupported action")
	}

	// ── Docker verification (read-only, outside any DB transaction) ───────
	//
	// Verification is performed only when the mutation command succeeded.
	// It is strictly read-only: it never retries, restarts, removes, or
	// mutates any Docker resource.
	var vResult verificationResult
	if mutationErr == nil {
		vResult = e.verify(ctx, action, tContainer.String, tDigest.String)
	}
	// For mutation failures: vResult is zero-value with status="" which maps to NULL.

	// ── Transaction 2: Persist execution result and verification status ───
	finalExecStatus := "EXECUTED"
	errMsg := ""
	if mutationErr != nil {
		finalExecStatus = "FAILED"
		errMsg = mutationErr.Error()
	}

	resCtx, resCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer resCancel()

	_, updateErr := e.db.ExecContext(resCtx, `
		UPDATE execution_authorizations 
		SET status = $1, updated_at = NOW() 
		WHERE execution_id = $2`, finalExecStatus, execID)
	if updateErr != nil {
		e.emitAudit("EXECUTION_FAILED", "Failed to write authorization status: "+updateErr.Error(), tAsset.String, tDigest.String)
	}

	// Insert execution_results with Phase 7.3 verification columns.
	// NULL verification_status and verified_at are used for FAILED mutations only.
	// EXECUTED rows from this code path always carry VERIFIED or FAILED verification_status.
	if vResult.status != "" {
		// Mutation succeeded — write dual-status row.
		var verifiedAt interface{}
		if vResult.status == "VERIFIED" {
			verifiedAt = time.Now()
		}
		_, insertErr := e.db.ExecContext(resCtx, `
			INSERT INTO execution_results (execution_id, recommendation_id, status, error_log, verification_status, verified_at)
			VALUES ($1, $2, $3, $4, $5, $6)`,
			execID, recID, finalExecStatus, errMsg, vResult.status, verifiedAt)
		if insertErr != nil {
			e.emitAudit("EXECUTION_FAILED", "Failed to write execution result: "+insertErr.Error(), tAsset.String, tDigest.String)
		}
	} else {
		// Mutation failed — verification_status and verified_at remain NULL.
		_, insertErr := e.db.ExecContext(resCtx, `
			INSERT INTO execution_results (execution_id, recommendation_id, status, error_log, verification_status, verified_at)
			VALUES ($1, $2, $3, $4, NULL, NULL)`,
			execID, recID, finalExecStatus, errMsg)
		if insertErr != nil {
			e.emitAudit("EXECUTION_FAILED", "Failed to write execution result: "+insertErr.Error(), tAsset.String, tDigest.String)
		}
	}

	// Ledger entry for Tx2 completion
	transID2 := uuid.New().String()
	_, ledgerErr := e.db.ExecContext(resCtx, `
		INSERT INTO execution_state_transitions (transition_id, execution_id, from_status, to_status, actor_type, actor_id, timestamp, reason)
		VALUES ($1, $2, 'EXECUTING', $3, 'system', 'executor', NOW(), $4)
	`, transID2, execID, finalExecStatus, errMsg)
	if ledgerErr != nil {
		e.emitAudit("EXECUTION_FAILED", "Failed to write ledger for completion: "+ledgerErr.Error(), tAsset.String, tDigest.String)
	}

	// ── Phase 4 event emission ────────────────────────────────────────────
	//
	// DB is authoritative. If NATS publish fails the state is logged; no rollback occurs.
	switch {
	case mutationErr != nil:
		e.emitAudit("EXECUTION_FAILED", "Mutation error: "+mutationErr.Error(), tAsset.String, tDigest.String)
	case vResult.status == "VERIFIED":
		e.emitAudit("EXECUTION_SUCCEEDED", fmt.Sprintf("Execution %s completed successfully", execID), tAsset.String, tDigest.String)
		e.emitAudit("EXECUTION_VERIFIED", fmt.Sprintf("Execution %s verified: postcondition confirmed", execID), tAsset.String, tDigest.String)
	case vResult.status == "FAILED":
		e.emitAudit("EXECUTION_SUCCEEDED", fmt.Sprintf("Execution %s completed but verification failed", execID), tAsset.String, tDigest.String)
		e.emitAudit("EXECUTION_VERIFICATION_FAILED", "Verification failed: "+vResult.reason, tAsset.String, tDigest.String)
	}

	return mutationErr
}

// ─── Verification ─────────────────────────────────────────────────────────────

// verify performs a synchronous, read-only post-mutation verification against Docker.
// It is called only when the mutation command returned nil (success).
// It reuses the existing Phase 7.2 CanonicalRepoDigest / RepoDigestsContain helpers.
// It never invokes any Docker mutation method.
//
// For STOP_CONTAINER the required postcondition is:
//   - Docker container ID matches targetContainerID exactly
//   - Docker RepoDigests contain the canonical targetDigest
//   - Docker container state is terminal (exited or dead)
func (e *Executor) verify(ctx context.Context, action, targetContainerID, targetDigest string) verificationResult {
	state, err := e.docker.InspectContainer(ctx, targetContainerID)
	if err != nil {
		return verificationResult{status: "FAILED", reason: "Docker inspect failed: " + err.Error()}
	}

	// Step A — Container ID must match exactly (using prefix match for short IDs).
	if !strings.HasPrefix(state.ID, targetContainerID) {
		return verificationResult{
			status: "FAILED",
			reason: fmt.Sprintf("container ID mismatch: expected prefix %q got %q", targetContainerID, state.ID),
		}
	}

	// Step B — Authoritative image digest must be present in RepoDigests.
	// Reuses the existing Phase 7.2 helpers; ImageID is never used.
	if !RepoDigestsContain(state.RepoDigests, targetDigest) {
		return verificationResult{
			status: "FAILED",
			reason: fmt.Sprintf("image digest verification failed: target %q not found in RepoDigests %v", targetDigest, state.RepoDigests),
		}
	}

	// Step C — Container state must be terminal for the given action.
	switch action {
	case "STOP_CONTAINER":
		if !terminalContainerStates[state.State] {
			return verificationResult{
				status: "FAILED",
				reason: fmt.Sprintf("container state %q is not terminal (want exited or dead)", state.State),
			}
		}
	case "QUARANTINE_ASSET":
		authNet := os.Getenv("AUTHORIZED_QUARANTINE_NETWORK")
		if authNet == "" {
			return verificationResult{
				status: "FAILED",
				reason: "AUTHORIZED_QUARANTINE_NETWORK not configured",
			}
		}
		for _, net := range state.NetworkIDs {
			if net == authNet {
				return verificationResult{
					status: "FAILED",
					reason: fmt.Sprintf("container still attached to quarantine network %q", authNet),
				}
			}
		}
	}

	return verificationResult{status: "VERIFIED"}
}
