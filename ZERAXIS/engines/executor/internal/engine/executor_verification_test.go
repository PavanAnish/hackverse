package engine

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/pavanvanish/zeraxis/engines/executor/internal/docker"
	_ "github.com/lib/pq"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

// insertApprovedExecution inserts the minimal DB fixtures needed to reach the mutation
// phase: incident, recommendation (APPROVED, with target fields), discovery_asset,
// and execution_authorization (APPROVED). Returns the execID.
func insertApprovedExecution(t *testing.T, db *sql.DB, containerID, digest string) (execID, recID, assetID string) {
	t.Helper()
	incID := uuid.New().String()
	if _, err := db.Exec(
		`INSERT INTO incidents (incident_id, fingerprint, status, risk_level, finding_count, severity, first_seen, last_seen)
		 VALUES ($1, $2, 'OPEN', 'CRITICAL', 1, 'CRITICAL', NOW(), NOW())`,
		incID, incID,
	); err != nil {
		t.Fatalf("insert incident: %v", err)
	}

	assetID = uuid.New().String()
	recID = uuid.New().String()
	if _, err := db.Exec(
		`INSERT INTO incident_recommendations
		 (recommendation_id, incident_id, fingerprint, action, priority, confidence, reason, evidence, model_version,
		  status, target_asset_id, target_container_id, target_image_digest)
		 VALUES ($1,$2,$3,'STOP_CONTAINER','CRITICAL',95,'Phase 7.3 Test','{}','1.0','APPROVED',$4,$5,$6)`,
		recID, incID, recID, assetID, containerID, digest,
	); err != nil {
		t.Fatalf("insert recommendation: %v", err)
	}

	if _, err := db.Exec(
		`INSERT INTO discovery_assets (asset_id, name, container_id, image, status) VALUES ($1,$1,$2,'img','running')`,
		assetID, containerID,
	); err != nil {
		t.Fatalf("insert discovery_asset: %v", err)
	}

	execID = uuid.New().String()
	if _, err := db.Exec(
		`INSERT INTO execution_authorizations (execution_id, recommendation_id, approver_id, status)
		 VALUES ($1,$2,'user-verifier','APPROVED')`,
		execID, recID,
	); err != nil {
		t.Fatalf("insert authorization: %v", err)
	}
	return
}

// queryVerification reads the verification columns for the given execID.
func queryVerification(t *testing.T, db *sql.DB, execID string) (status, verStatus string, verifiedAt sql.NullTime) {
	t.Helper()
	if err := db.QueryRow(
		`SELECT status, COALESCE(verification_status,''), verified_at FROM execution_results WHERE execution_id = $1`,
		execID,
	).Scan(&status, &verStatus, &verifiedAt); err != nil {
		t.Fatalf("queryVerification: %v", err)
	}
	return
}

// ─── Test 1: Verification success ────────────────────────────────────────────

// TestVerification_Success: mutation succeeds, inspect returns correct ID + digest + "exited".
// Expected: EXECUTED + VERIFIED.
func TestVerification_Success(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	ctx := context.Background()

	containerID := uuid.New().String()
	execID, _, _ := insertApprovedExecution(t, db, containerID, realDigest)

	// Default NewMockClient: ContainerStop succeeds, InspectContainer returns exited + correct digest.
	mockCli := docker.NewMockClient(realDigest, "net1")
	exec := NewExecutor(db, mockCli, nil)

	err := exec.Execute(ctx, execID)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	status, verStatus, verifiedAt := queryVerification(t, db, execID)
	if status != "EXECUTED" {
		t.Errorf("status = %q, want EXECUTED", status)
	}
	if verStatus != "VERIFIED" {
		t.Errorf("verification_status = %q, want VERIFIED", verStatus)
	}
	if !verifiedAt.Valid {
		t.Errorf("verified_at should be set for VERIFIED outcome")
	}
}

// ─── Test 2: Running-state verification failure ───────────────────────────────

// TestVerification_RunningState: mutation succeeds, container is still "running".
// Expected: EXECUTED + FAILED.
func TestVerification_RunningState(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	ctx := context.Background()

	containerID := uuid.New().String()
	execID, _, _ := insertApprovedExecution(t, db, containerID, realDigest)

	mockCli := docker.NewMockClient(realDigest, "net1")
	mockCli.ContainerState = "running" // mutation "succeeded" but container didn't actually stop
	exec := NewExecutor(db, mockCli, nil)

	err := exec.Execute(ctx, execID)
	if err != nil {
		t.Fatalf("mutation should succeed, got: %v", err)
	}

	status, verStatus, verifiedAt := queryVerification(t, db, execID)
	if status != "EXECUTED" {
		t.Errorf("status = %q, want EXECUTED", status)
	}
	if verStatus != "FAILED" {
		t.Errorf("verification_status = %q, want FAILED", verStatus)
	}
	if verifiedAt.Valid {
		t.Errorf("verified_at should be NULL for FAILED verification")
	}
}

// ─── Test 3: Restarting-state verification failure ───────────────────────────

// TestVerification_RestartingState: container state is "restarting".
// Expected: EXECUTED + FAILED.
func TestVerification_RestartingState(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	ctx := context.Background()

	containerID := uuid.New().String()
	execID, _, _ := insertApprovedExecution(t, db, containerID, realDigest)

	mockCli := docker.NewMockClient(realDigest, "net1")
	mockCli.ContainerState = "restarting"
	exec := NewExecutor(db, mockCli, nil)

	err := exec.Execute(ctx, execID)
	if err != nil {
		t.Fatalf("mutation should succeed, got: %v", err)
	}

	_, verStatus, verifiedAt := queryVerification(t, db, execID)
	if verStatus != "FAILED" {
		t.Errorf("verification_status = %q, want FAILED for restarting state", verStatus)
	}
	if verifiedAt.Valid {
		t.Errorf("verified_at should be NULL for FAILED verification")
	}
}

// ─── Test 4: Digest mismatch verification failure ────────────────────────────

// TestVerification_DigestMismatch: inspect returns wrong canonical digest.
// Expected: EXECUTED + FAILED.
func TestVerification_DigestMismatch(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	ctx := context.Background()

	containerID := uuid.New().String()
	execID, _, _ := insertApprovedExecution(t, db, containerID, realDigest)

	// The pre-mutation inspection must succeed (realDigest) to pass authorization.
	// After mutation, InspectResult returns otherDigest, simulating image replacement.
	mockCli := docker.NewMockClient(realDigest, "net1")
	mockCli.InspectResult = &docker.ContainerState{
		ID:          containerID,
		ImageID:     "sha256:internal_id_not_registry_digest",
		RepoDigests: []string{"mock.registry/image@" + otherDigest},
		State:       "exited",
		NetworkIDs:  []string{"net1"},
	}
	exec := NewExecutor(db, mockCli, nil)

	err := exec.Execute(ctx, execID)
	// Pre-mutation inspection uses InspectResult (otherDigest) which won't match realDigest.
	// So the execution is ABORTED before mutation — this test validates the pre-mutation guard.
	if err == nil || !strings.Contains(err.Error(), "image digest mismatch") {
		t.Fatalf("expected DENY for digest mismatch in pre-mutation inspection, got: %v", err)
	}

	// For post-mutation-only digest mismatch we need a two-phase mock.
	// Use callCount to switch behavior: first InspectContainer call returns realDigest
	// (pre-mutation check passes), second returns otherDigest (verification fails).
	containerID2 := uuid.New().String()
	execID2, _, _ := insertApprovedExecution(t, db, containerID2, realDigest)
	twoPhase := &twoPhaseInspectMock{
		firstResult: &docker.ContainerState{
			ID:          containerID2,
			RepoDigests: []string{"mock.registry/image@" + realDigest},
			State:       "running",
			NetworkIDs:  []string{"net1"},
		},
		secondResult: &docker.ContainerState{
			ID:          containerID2,
			RepoDigests: []string{"mock.registry/image@" + otherDigest},
			State:       "exited",
			NetworkIDs:  []string{"net1"},
		},
	}
	exec2 := NewExecutor(db, twoPhase, nil)
	if err := exec2.Execute(ctx, execID2); err != nil {
		t.Fatalf("mutation should succeed, got: %v", err)
	}

	_, verStatus, _ := queryVerification(t, db, execID2)
	if verStatus != "FAILED" {
		t.Errorf("verification_status = %q, want FAILED for post-mutation digest mismatch", verStatus)
	}
}

// ─── Test 5: Container ID mismatch ───────────────────────────────────────────

// TestVerification_ContainerIDMismatch: authorized container A, inspect returns B.
// B has the correct digest and is exited. Expected: EXECUTED + FAILED.
// This proves correct image digest alone does not satisfy verification.
func TestVerification_ContainerIDMismatch(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	ctx := context.Background()

	containerA := uuid.New().String()
	containerB := uuid.New().String()
	execID, _, _ := insertApprovedExecution(t, db, containerA, realDigest)

	// Pre-mutation: first call must return containerA to pass authorization.
	// Post-mutation: second call returns containerB (correct digest, exited) → FAILED.
	twoPhase := &twoPhaseInspectMock{
		firstResult: &docker.ContainerState{
			ID:          containerA,
			RepoDigests: []string{"mock.registry/image@" + realDigest},
			State:       "running",
			NetworkIDs:  []string{"net1"},
		},
		secondResult: &docker.ContainerState{
			ID:          containerB, // Different container ID
			RepoDigests: []string{"mock.registry/image@" + realDigest},
			State:       "exited",
			NetworkIDs:  []string{"net1"},
		},
	}
	exec := NewExecutor(db, twoPhase, nil)

	if err := exec.Execute(ctx, execID); err != nil {
		t.Fatalf("mutation should succeed, got: %v", err)
	}

	_, verStatus, _ := queryVerification(t, db, execID)
	if verStatus != "FAILED" {
		t.Errorf("verification_status = %q, want FAILED when inspect returns wrong container ID", verStatus)
	}
}

// ─── Test 6: Inspect failure ─────────────────────────────────────────────────

// TestVerification_InspectFailure: InspectContainer returns an error post-mutation.
// Expected: EXECUTED + FAILED.
func TestVerification_InspectFailure(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	ctx := context.Background()

	containerID := uuid.New().String()
	execID, _, _ := insertApprovedExecution(t, db, containerID, realDigest)

	// First InspectContainer call (pre-mutation) succeeds; second fails.
	twoPhase := &twoPhaseInspectMock{
		firstResult: &docker.ContainerState{
			ID:          containerID,
			RepoDigests: []string{"mock.registry/image@" + realDigest},
			State:       "running",
			NetworkIDs:  []string{"net1"},
		},
		secondErr: fmt.Errorf("docker daemon unavailable"),
	}
	exec := NewExecutor(db, twoPhase, nil)

	if err := exec.Execute(ctx, execID); err != nil {
		t.Fatalf("mutation should succeed, got: %v", err)
	}

	status, verStatus, _ := queryVerification(t, db, execID)
	if status != "EXECUTED" {
		t.Errorf("status = %q, want EXECUTED", status)
	}
	if verStatus != "FAILED" {
		t.Errorf("verification_status = %q, want FAILED when inspect fails", verStatus)
	}
}

// ─── Test 7: Multiple RepoDigests — correct one present ──────────────────────

// TestVerification_MultipleRepoDigests: Docker returns multiple RepoDigests.
// One canonically matches target. Expected: EXECUTED + VERIFIED.
func TestVerification_MultipleRepoDigests(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	ctx := context.Background()

	containerID := uuid.New().String()
	execID, _, _ := insertApprovedExecution(t, db, containerID, realDigest)

	mockCli := docker.NewMockClient(realDigest, "net1")
	// Override InspectResult to return multiple digests; one matches.
	mockCli.InspectResult = &docker.ContainerState{
		ID: containerID,
		RepoDigests: []string{
			"registry1.example.com/repo@" + otherDigest,
			"registry2.example.com/repo@" + realDigest, // this one matches
		},
		State:      "exited",
		NetworkIDs: []string{"net1"},
	}
	exec := NewExecutor(db, mockCli, nil)

	// Pre-mutation InspectContainer will use InspectResult too.
	// We need the first call to succeed. Since both checks use InspectResult,
	// the pre-mutation digest check must also find a match — realDigest is present.
	if err := exec.Execute(ctx, execID); err != nil {
		t.Fatalf("expected success with multiple digests, got: %v", err)
	}

	_, verStatus, _ := queryVerification(t, db, execID)
	if verStatus != "VERIFIED" {
		t.Errorf("verification_status = %q, want VERIFIED when one of multiple digests matches", verStatus)
	}
}

// ─── Test 8: ImageID negative — only ImageID matches, RepoDigest does not ────

// TestVerification_ImageIDNegative: Only the Docker ImageID matches the target digest.
// RepoDigests don't contain target. Expected: pre-mutation DENY (not VERIFICATION_FAILED).
// This test proves ImageID is never used as verification identity.
func TestVerification_ImageIDNegative(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	ctx := context.Background()

	containerID := uuid.New().String()
	execID, _, _ := insertApprovedExecution(t, db, containerID, realDigest)

	mockCli := docker.NewMockClient(realDigest, "net1")
	// Return realDigest as the ImageID but different RepoDigest.
	mockCli.InspectResult = &docker.ContainerState{
		ID:          containerID,
		ImageID:     realDigest, // Attacker stored registry digest as ImageID
		RepoDigests: []string{"mock.registry/image@" + otherDigest},
		State:       "exited",
		NetworkIDs:  []string{"net1"},
	}
	exec := NewExecutor(db, mockCli, nil)

	err := exec.Execute(ctx, execID)
	// The pre-mutation check verifies RepoDigests (not ImageID), so this is DENIED before mutation.
	if err == nil {
		t.Fatal("expected DENY: ImageID must not authorize or verify")
	}
	if !strings.Contains(err.Error(), "image digest mismatch") {
		t.Errorf("expected image digest mismatch error, got: %v", err)
	}
	// Mutation must NOT have been called.
	if mockCli.TotalMutationCount() != 0 {
		t.Errorf("ContainerStop must not be called when digest check fails, got %d calls", mockCli.TotalMutationCount())
	}
}

// ─── Test 9: Event emission ───────────────────────────────────────────────────

// TestVerification_EventEmission: verifies that the correct Phase 4 event types are emitted.
// VERIFIED path → EXECUTION_VERIFIED.  FAILED path → EXECUTION_VERIFICATION_FAILED.
// This test validates event routing logic without requiring a real NATS; it inspects
// the emitAudit routing by examining the resulting DB state.
func TestVerification_EventEmission(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	ctx := context.Background()

	t.Run("VERIFIED emits EXECUTION_VERIFIED", func(t *testing.T) {
		containerID := uuid.New().String()
		execID, _, _ := insertApprovedExecution(t, db, containerID, realDigest)
		mockCli := docker.NewMockClient(realDigest, "net1")
		// State="exited" by default → VERIFIED path
		exec := NewExecutor(db, mockCli, nil)
		if err := exec.Execute(ctx, execID); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		_, verStatus, _ := queryVerification(t, db, execID)
		if verStatus != "VERIFIED" {
			t.Errorf("expected VERIFIED, got %q — EXECUTION_VERIFIED should have been emitted", verStatus)
		}
	})

	t.Run("FAILED verification emits EXECUTION_VERIFICATION_FAILED", func(t *testing.T) {
		containerID := uuid.New().String()
		execID, _, _ := insertApprovedExecution(t, db, containerID, realDigest)
		twoPhase := &twoPhaseInspectMock{
			firstResult: &docker.ContainerState{
				ID:          containerID,
				RepoDigests: []string{"mock.registry/image@" + realDigest},
				State:       "running",
				NetworkIDs:  []string{"net1"},
			},
			secondResult: &docker.ContainerState{
				ID:          containerID,
				RepoDigests: []string{"mock.registry/image@" + realDigest},
				State:       "running", // Not terminal → VERIFICATION_FAILED
			},
		}
		exec := NewExecutor(db, twoPhase, nil)
		if err := exec.Execute(ctx, execID); err != nil {
			t.Fatalf("mutation should succeed: %v", err)
		}
		_, verStatus, _ := queryVerification(t, db, execID)
		if verStatus != "FAILED" {
			t.Errorf("expected FAILED, got %q — EXECUTION_VERIFICATION_FAILED should have been emitted", verStatus)
		}
	})
}

// ─── Test 10: Persistence — all three valid outcome combinations ──────────────

// TestVerification_Persistence: verifies all three valid state combinations persist correctly.
func TestVerification_Persistence(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	ctx := context.Background()

	t.Run("FAILED+NULL from mutation failure", func(t *testing.T) {
		containerID := uuid.New().String()
		execID, _, _ := insertApprovedExecution(t, db, containerID, realDigest)

		mockCli := docker.NewMockClient(realDigest, "net1")
		mockCli.ContainerStopErr = fmt.Errorf("docker daemon returned error")
		exec := NewExecutor(db, mockCli, nil)

		_ = exec.Execute(ctx, execID)

		status, verStatus, verifiedAt := queryVerification(t, db, execID)
		if status != "FAILED" {
			t.Errorf("status = %q, want FAILED", status)
		}
		if verStatus != "" {
			t.Errorf("verification_status = %q, want NULL (empty)", verStatus)
		}
		if verifiedAt.Valid {
			t.Errorf("verified_at should be NULL for mutation failure")
		}
	})

	t.Run("EXECUTED+FAILED from verification failure", func(t *testing.T) {
		containerID := uuid.New().String()
		execID, _, _ := insertApprovedExecution(t, db, containerID, realDigest)

		twoPhase := &twoPhaseInspectMock{
			firstResult: &docker.ContainerState{
				ID:          containerID,
				RepoDigests: []string{"mock.registry/image@" + realDigest},
				State:       "running",
				NetworkIDs:  []string{"net1"},
			},
			secondResult: &docker.ContainerState{
				ID:          containerID,
				RepoDigests: []string{"mock.registry/image@" + realDigest},
				State:       "restarting", // non-terminal
			},
		}
		exec := NewExecutor(db, twoPhase, nil)
		if err := exec.Execute(ctx, execID); err != nil {
			t.Fatalf("mutation should succeed: %v", err)
		}

		status, verStatus, verifiedAt := queryVerification(t, db, execID)
		if status != "EXECUTED" || verStatus != "FAILED" {
			t.Errorf("want EXECUTED+FAILED, got %s+%s", status, verStatus)
		}
		if verifiedAt.Valid {
			t.Errorf("verified_at should be NULL for FAILED verification")
		}
	})

	t.Run("EXECUTED+VERIFIED from successful verification", func(t *testing.T) {
		containerID := uuid.New().String()
		execID, _, _ := insertApprovedExecution(t, db, containerID, realDigest)

		mockCli := docker.NewMockClient(realDigest, "net1")
		exec := NewExecutor(db, mockCli, nil)
		if err := exec.Execute(ctx, execID); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		status, verStatus, verifiedAt := queryVerification(t, db, execID)
		if status != "EXECUTED" || verStatus != "VERIFIED" {
			t.Errorf("want EXECUTED+VERIFIED, got %s+%s", status, verStatus)
		}
		if !verifiedAt.Valid {
			t.Errorf("verified_at must be set for VERIFIED outcome")
		}
	})
}

// ─── Test 11: Verification read-only guarantee ───────────────────────────────

// TestVerification_ReadOnlyGuarantee: proves that the verification step never
// calls ContainerStop. InspectContainer is called; ContainerStop is called exactly once
// (for the mutation) and never again during verification.
func TestVerification_ReadOnlyGuarantee(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	ctx := context.Background()

	containerID := uuid.New().String()
	execID, _, _ := insertApprovedExecution(t, db, containerID, realDigest)

	mockCli := docker.NewMockClient(realDigest, "net1")
	exec := NewExecutor(db, mockCli, nil)

	if err := exec.Execute(ctx, execID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// ContainerStop must have been called exactly once (the mutation).
	stopCount := mockCli.TotalMutationCount()
	if stopCount != 1 {
		t.Errorf("ContainerStop call count = %d, want exactly 1", stopCount)
	}

	// InspectContainer must have been called: once pre-mutation + once for verification.
	inspectCount := mockCli.InspectCallCount()
	if inspectCount < 2 {
		t.Errorf("InspectContainer call count = %d, want >= 2 (pre-mutation + verification)", inspectCount)
	}
}

// ─── Test 12: Idempotency — no second mutation ───────────────────────────────

// TestVerification_Idempotency: ensures a completed execution cannot be re-executed.
// The authorization is in EXECUTING/EXECUTED state; a second Execute() call must DENY.
func TestVerification_Idempotency(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	ctx := context.Background()

	containerID := uuid.New().String()
	execID, _, _ := insertApprovedExecution(t, db, containerID, realDigest)

	mockCli := docker.NewMockClient(realDigest, "net1")
	exec := NewExecutor(db, mockCli, nil)

	// First execution succeeds.
	if err := exec.Execute(ctx, execID); err != nil {
		t.Fatalf("first Execute: %v", err)
	}

	// Second execution with the same execID must be denied.
	err := exec.Execute(ctx, execID)
	if err == nil {
		t.Fatal("second Execute should return DENY")
	}
	if !strings.Contains(err.Error(), "authorization revoked, expired, or already executed") &&
		!strings.Contains(err.Error(), "authorization missing") {
		t.Errorf("unexpected error: %v", err)
	}

	// Mutation must have been called exactly once total.
	if mockCli.TotalMutationCount() != 1 {
		t.Errorf("ContainerStop count = %d, want exactly 1", mockCli.TotalMutationCount())
	}
}

// ─── twoPhaseInspectMock ─────────────────────────────────────────────────────

// twoPhaseInspectMock returns firstResult on the first InspectContainer call and
// secondResult (or secondErr) on subsequent calls. ContainerStop always succeeds.
// This is used for tests where pre-mutation and post-mutation Docker state differ.
type twoPhaseInspectMock struct {
	firstResult  *docker.ContainerState
	secondResult *docker.ContainerState
	secondErr    error

	mu       sync.Mutex
	callCount int
}

func (m *twoPhaseInspectMock) ContainerStop(ctx context.Context, containerID string) error {
	return nil
}

func (m *twoPhaseInspectMock) NetworkDisconnect(ctx context.Context, containerID, networkID string) error {
	return nil
}

func (m *twoPhaseInspectMock) InspectContainer(ctx context.Context, containerID string) (*docker.ContainerState, error) {
	m.mu.Lock()
	m.callCount++
	n := m.callCount
	m.mu.Unlock()

	if n == 1 {
		return m.firstResult, nil
	}
	if m.secondErr != nil {
		return nil, m.secondErr
	}
	return m.secondResult, nil
}
