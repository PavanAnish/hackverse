package engine

import (
	"context"
		"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/google/uuid"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"

	"github.com/pavanvanish/zeraxis/engines/correlation/pkg/approvals"
	"github.com/pavanvanish/zeraxis/engines/executor/internal/docker"
	"github.com/pavanvanish/zeraxis/pkg/events"
	_ "github.com/lib/pq"
)

func RunNATSServer() (*server.Server, error) {
	opts := &server.Options{Port: 4222}
	s, err := server.NewServer(opts)
	if err != nil {
		return nil, err
	}
	go s.Start()
	if !s.ReadyForConnections(5 * time.Second) {
		return nil, os.ErrDeadlineExceeded
	}
	return s, nil
}

// getPhase3RegistryDigest fetches the authoritative OCI manifest digest from the Phase 3
// local registry using a HEAD request with the appropriate Accept headers.
// Returns t.Skip if the registry is unreachable.
func getPhase3RegistryDigest(t *testing.T, imageRepo string) string {
	t.Helper()
	req, err := http.NewRequest("HEAD", "http://localhost:5001/v2/"+imageRepo+"/manifests/latest", nil)
	if err != nil {
		t.Fatalf("failed to construct registry request: %v", err)
	}
	// Full OCI + Docker manifest Accept header set required to get Docker-Content-Digest.
	req.Header.Set("Accept", "application/vnd.oci.image.manifest.v1+json,application/vnd.docker.distribution.manifest.v2+json,application/vnd.docker.distribution.manifest.list.v2+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Skipf("Phase 3 local OCI registry not reachable at localhost:5001: %v — SKIPPED", err)
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Skipf("Phase 3 registry returned HTTP %d for %s — SKIPPED", resp.StatusCode, imageRepo)
		return ""
	}
	digest := resp.Header.Get("Docker-Content-Digest")
	if digest == "" {
		t.Fatalf("Phase 3 registry response missing Docker-Content-Digest header")
	}
	if err := ValidateTargetDigest(digest); err != nil {
		t.Fatalf("Phase 3 registry returned malformed Docker-Content-Digest %q: %v", digest, err)
	}
	return digest
}

// connectNATS connects to the external NATS daemon if running, or starts an in-process server.
// External NATS takes precedence (port 4222 may be occupied by the compose stack).
func connectNATS(t *testing.T) (*nats.Conn, func()) {
	t.Helper()
	nc, err := nats.Connect("nats://localhost:4222")
	if err == nil {
		t.Logf("Using external NATS at localhost:4222")
		return nc, func() { nc.Close() }
	}
	// Fall back to in-process server on a random port.
	opts := &server.Options{Port: -1} // -1 = random available port
	s, sErr := server.NewServer(opts)
	if sErr != nil {
		t.Skipf("No NATS available (external failed: %v; in-process failed: %v) — SKIPPED", err, sErr)
		return nil, nil
	}
	go s.Start()
	if !s.ReadyForConnections(5 * time.Second) {
		t.Skipf("In-process NATS failed to start — SKIPPED")
		return nil, nil
	}
	nc2, err2 := nats.Connect(s.ClientURL())
	if err2 != nil {
		t.Fatalf("failed to connect to in-process NATS: %v", err2)
	}
	t.Logf("Using in-process NATS at %s", s.ClientURL())
	return nc2, func() { nc2.Close(); s.Shutdown() }
}

// TestIntegration_DockerNATSExecution is the definitive Phase 7.2 E2E test.
//
// Flow:
//
//	Phase 3 registry manifest digest
//	  → Docker pull same registry image
//	  → verify container RepoDigest canonical == registry digest
//	  → insert PROPOSED recommendation using registry digest
//	  → approvals.Service.ApproveAndPublish (real production path)
//	  → NATS zeraxis.events.execution.authorized
//	  → Executor.HandleNatsWakeup
//	  → Execute (canonical RepoDigest comparison)
//	  → Docker ContainerStop mutation
//	  → execution_results = EXECUTED
//	  → pkg/events Phase 4 unified audit event EXECUTION_SUCCEEDED
//
// Infrastructure requirements: PostgreSQL, Docker, Phase 3 local OCI registry at localhost:5000.
// Any missing dependency causes SKIP, not FAIL.
func TestIntegration_DockerNATSExecution(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in -short mode")
	}

	// ── Infrastructure checks ──────────────────────────────────────────────
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Skipf("Docker not available: %v — SKIPPED", err)
	}
	ctx := context.Background()
	if _, err := cli.Ping(ctx); err != nil {
		t.Skipf("Docker daemon not reachable: %v — SKIPPED", err)
	}

	db := setupTestDB(t)
	var dbErr error
	if dbErr != nil {
		t.Skipf("PostgreSQL open failed: %v — SKIPPED", dbErr)
	}
	defer db.Close()
	if pingErr := db.Ping(); pingErr != nil {
		t.Skipf("PostgreSQL not reachable: %v — SKIPPED", pingErr)
	}

	// ── Step 1: Fetch authoritative Phase 3 registry digest ───────────────
	registryImage := "zeraxis-test-target"
	authoritativeDigest := getPhase3RegistryDigest(t, registryImage)
	// getPhase3RegistryDigest calls t.Skip if registry unreachable.
	// If we reach here, authoritativeDigest is a valid sha256:<64hex> string.

	// ── Step 2: Docker pull the Phase 3 registry image ───────────────────
	// fullImageRef uses port 5001 — the host-mapped port for the Phase 3 local registry.
	fullImageRef := "localhost:5001/" + registryImage + ":latest"
	pullReader, err := cli.ImagePull(ctx, fullImageRef, types.ImagePullOptions{})
	if err != nil {
		t.Fatalf("Docker ImagePull %s failed: %v", fullImageRef, err)
	}
	// Fully consume and close so the pull completes and resources release.
	if _, err := io.Copy(io.Discard, pullReader); err != nil {
		t.Fatalf("consuming ImagePull response: %v", err)
	}
	pullReader.Close()

	// ── Step 3: Verify Docker image RepoDigests match Phase 3 digest ──────
	imgInspect, _, err := cli.ImageInspectWithRaw(ctx, fullImageRef)
	if err != nil {
		t.Fatalf("ImageInspect after pull failed: %v", err)
	}
	if !RepoDigestsContain(imgInspect.RepoDigests, authoritativeDigest) {
		t.Fatalf("PRECONDITION FAILED: pulled image RepoDigests %v do not contain Phase 3 authoritative digest %s — cannot continue", imgInspect.RepoDigests, authoritativeDigest)
	}

	// ── Step 4: Connect to NATS (external if available, else in-process) ──
	nc, natsCleanup := connectNATS(t)
	if nc == nil {
		return // already called t.Skip inside connectNATS
	}
	defer natsCleanup()

	// ── Step 5: Create test network and container ─────────────────────────
	netResp, err := cli.NetworkCreate(ctx, "zeraxis-test-net", types.NetworkCreate{})
	if err != nil {
		t.Fatalf("NetworkCreate: %v", err)
	}
	defer cli.NetworkRemove(ctx, netResp.ID)

	os.Setenv("AUTHORIZED_QUARANTINE_NETWORK", "zeraxis-test-net")
	defer os.Unsetenv("AUTHORIZED_QUARANTINE_NETWORK")

	contResp, err := cli.ContainerCreate(ctx, &container.Config{
		Image: fullImageRef,
		Cmd:   []string{"sleep", "300"},
	}, &container.HostConfig{}, &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			"zeraxis-test-net": {NetworkID: netResp.ID},
		},
	}, nil, "zeraxis-e2e-target")
	if err != nil {
		t.Fatalf("ContainerCreate: %v", err)
	}
	defer cli.ContainerRemove(ctx, contResp.ID, types.ContainerRemoveOptions{Force: true})

	if err := cli.ContainerStart(ctx, contResp.ID, types.ContainerStartOptions{}); err != nil {
		t.Fatalf("ContainerStart: %v", err)
	}

	// ── Step 6: Database setup ─────────────────────────────────────────────
	incID := uuid.New().String()
	if _, err := db.Exec(`
		INSERT INTO incidents (incident_id, fingerprint, status, risk_level, finding_count, severity, first_seen, last_seen)
		VALUES ($1, $2, 'OPEN', 'CRITICAL', 1, 'CRITICAL', NOW(), NOW())
	`, incID, incID); err != nil {
		t.Fatalf("insert incident: %v", err)
	}

	assetID := uuid.New().String()
	recID := uuid.New().String()
	if _, err := db.Exec(`
		INSERT INTO incident_recommendations
		  (recommendation_id, incident_id, fingerprint, action, priority, confidence,
		   reason, evidence, model_version, status,
		   target_asset_id, target_container_id, target_image_digest)
		VALUES ($1, $2, $3, 'STOP_CONTAINER', 'CRITICAL', 95, 'E2E Test', '{}', '1.0', 'PROPOSED',
		        $4, $5, $6)
	`, recID, incID, recID, assetID, contResp.ID, authoritativeDigest); err != nil {
		t.Fatalf("insert recommendation: %v", err)
	}

	if _, err := db.Exec(`
		INSERT INTO discovery_assets (asset_id, asset_type, name, container_id, image, status)
		VALUES ($1, 'container', $1, $2, $3, 'running')
	`, assetID, contResp.ID, fullImageRef); err != nil {
		t.Fatalf("insert discovery_asset: %v", err)
	}

	// ── Step 7: Wire Executor subscriber ──────────────────────────────────
	dockerWrapper, err := docker.NewRealClient()
	if err != nil {
		t.Fatalf("NewRealClient: %v", err)
	}
	executor := NewExecutor(db, dockerWrapper, nc)

	subExec, err := nc.Subscribe("zeraxis.events.execution.authorized", func(msg *nats.Msg) {
		executor.HandleNatsWakeup(ctx, msg.Data)
	})
	if err != nil {
		t.Fatalf("subscribe executor: %v", err)
	}
	defer subExec.Unsubscribe()

	auditCh := make(chan *events.Event, 10)
	subAudit, err := nc.Subscribe("zeraxis.events.execution.audit", func(msg *nats.Msg) {
		var ev events.Event
		if jsonErr := json.Unmarshal(msg.Data, &ev); jsonErr == nil && ev.Source == "executor" {
			auditCh <- &ev
		}
	})
	if err != nil {
		t.Fatalf("subscribe audit: %v", err)
	}
	defer subAudit.Unsubscribe()

	// ── Step 8: Real production approval workflow ─────────────────────────
	// approvals.Service.ApproveAndPublish is the Phase 7.2 production NATS publisher.
	// Phase 7.1 defined the schema; this service is new in Phase 7.2.
	approvalSvc := approvals.NewService(db, nc)
	execID, approvalErr := approvalSvc.ApproveAndPublish(ctx, recID, "user-admin")
	if approvalErr != nil && !errors.Is(approvalErr, approvals.ErrDispatchFailed) && !errors.Is(approvalErr, approvals.ErrDispatchUnavailable) {
		t.Fatalf("ApproveAndPublish failed before DB commit: %v", approvalErr)
	}
	if approvalErr != nil {
		t.Fatalf("ApproveAndPublish DB committed but NATS dispatch failed: %v", approvalErr)
	}

	// Verify recommendation transitioned to APPROVED in DB.
	var recStatus string
	if err := db.QueryRow(`SELECT status FROM incident_recommendations WHERE recommendation_id = $1`, recID).Scan(&recStatus); err != nil {
		t.Fatalf("query recommendation status: %v", err)
	}
	if recStatus != "APPROVED" {
		t.Fatalf("expected recommendation status APPROVED, got %s", recStatus)
	}

	// Verify execution_authorizations row exists with correct approver.
	var authApprover string
	if err := db.QueryRow(`SELECT approver_id FROM execution_authorizations WHERE execution_id = $1`, execID).Scan(&authApprover); err != nil {
		t.Fatalf("query execution_authorizations: %v", err)
	}
	if authApprover != "user-admin" {
		t.Fatalf("expected approver_id 'user-admin', got %q", authApprover)
	}

	// ── Step 9: Wait for Executor EXECUTION_SUCCEEDED audit event ─────────
	timeout := time.After(15 * time.Second)
	for {
		select {
		case ev := <-auditCh:
			switch ev.EventType {
			case "EXECUTION_SUCCEEDED":
				// Validate all required Phase 4 unified envelope fields.
				if ev.EventID == "" {
					t.Fatalf("audit event missing event_id")
				}
				if ev.EventVersion == 0 {
					t.Fatalf("audit event missing event_version")
				}
				if ev.Timestamp.IsZero() {
					t.Fatalf("audit event missing timestamp")
				}
				if ev.Source != "executor" {
					t.Fatalf("audit event source = %q, want executor", ev.Source)
				}
				if ev.AssetID != assetID {
					t.Fatalf("audit event asset_id = %q, want %q", ev.AssetID, assetID)
				}
				if ev.ImageDigest != authoritativeDigest {
					t.Fatalf("audit event image_digest = %q, want %q", ev.ImageDigest, authoritativeDigest)
				}
				goto VERIFY
			case "EXECUTION_FAILED", "EXECUTION_ABORTED":
				t.Fatalf("Execution failed/aborted: %s", string(ev.Metadata))
			}
		case <-timeout:
			t.Fatalf("timed out after 15s waiting for EXECUTION_SUCCEEDED audit event")
		}
	}

VERIFY:
	// ── Step 10: Verify Docker mutation ───────────────────────────────────
	postInspect, err := cli.ContainerInspect(ctx, contResp.ID)
	if err != nil {
		t.Fatalf("ContainerInspect post-execution: %v", err)
	}
	if postInspect.State.Running {
		t.Fatalf("container should be stopped after execution but is still running")
	}

	// Confirm the container's image still carries the authoritative registry digest.
	postImg, _, err := cli.ImageInspectWithRaw(ctx, postInspect.Image)
	if err != nil {
		t.Fatalf("ImageInspect post-execution: %v", err)
	}
	if !RepoDigestsContain(postImg.RepoDigests, authoritativeDigest) {
		t.Fatalf("post-execution container image RepoDigests %v missing authoritative digest %s", postImg.RepoDigests, authoritativeDigest)
	}

	// ── Step 11: Verify execution_results ─────────────────────────────────
	var finalStatus string
	if err := db.QueryRow(`SELECT status FROM execution_results WHERE execution_id = $1`, execID).Scan(&finalStatus); err != nil {
		t.Fatalf("query execution_results: %v", err)
	}
	if finalStatus != "EXECUTED" {
		t.Fatalf("execution_results.status = %q, want EXECUTED", finalStatus)
	}
}
