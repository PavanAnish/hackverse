sed -i '' 's/RepoDigests: inspect.Image,/RepoDigests: nil,/' engines/executor/internal/docker/client.go

cat << 'TEST' > engines/executor/internal/engine/executor_integration_test.go
package engine

import (
	"context"
	"database/sql"
	"testing"
	"os"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/google/uuid"

	"github.com/pavanvanish/zeraxis/engines/executor/internal/docker"
	_ "github.com/lib/pq"
)

func TestIntegration_DockerExecution(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping DB and Docker integration tests in short mode")
	}

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Skipf("Docker not available: %v", err)
	}
	ctx := context.Background()

	db, err := sql.Open("postgres", "postgres://postgres:postgres@localhost:5433/zeraxis?sslmode=disable")
	if err != nil || db.Ping() != nil {
		t.Skipf("DB not available: %v", err)
	}
	defer db.Close()

	// 1. Setup disposable container
	netResp, err := cli.NetworkCreate(ctx, "zeraxis-test-net", types.NetworkCreate{})
	if err != nil {
		t.Fatalf("Failed to create network: %v", err)
	}
	defer cli.NetworkRemove(ctx, netResp.ID)
	
	os.Setenv("AUTHORIZED_QUARANTINE_NETWORK", "zeraxis-test-net")
	defer os.Unsetenv("AUTHORIZED_QUARANTINE_NETWORK")

	contResp, err := cli.ContainerCreate(ctx, &container.Config{
		Image: "alpine:latest",
		Cmd:   []string{"sleep", "300"},
	}, &container.HostConfig{}, &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			"zeraxis-test-net": {NetworkID: netResp.ID},
		},
	}, nil, "zeraxis-test-target")
	if err != nil {
		t.Fatalf("Failed to create container: %v", err)
	}
	defer cli.ContainerRemove(ctx, contResp.ID, types.ContainerRemoveOptions{Force: true})

	if err := cli.ContainerStart(ctx, contResp.ID, types.ContainerStartOptions{}); err != nil {
		t.Fatalf("Failed to start container: %v", err)
	}

	inspect, _ := cli.ContainerInspect(ctx, contResp.ID)
	imageID := inspect.Image

	// 2. Setup Database State
	incID := uuid.New().String()
	_, _ = db.Exec(`INSERT INTO incidents (incident_id, fingerprint, status, risk_level, finding_count, severity) VALUES ($1, $2, 'OPEN', 'CRITICAL', 1, 'CRITICAL')`, incID, incID)

	recID := uuid.New().String()
	_, _ = db.Exec(`INSERT INTO incident_recommendations (recommendation_id, incident_id, fingerprint, action, priority, confidence, reason, evidence, model_version, status, target_asset_id, target_container_id, target_image_digest) VALUES ($1, $2, $3, 'STOP_CONTAINER', 'CRITICAL', 95, 'Test', '{}', '1.0', 'APPROVED', 'A-TEST', $4, $5)`, recID, incID, recID, contResp.ID, imageID)

	_, _ = db.Exec(`INSERT INTO discovery_assets (asset_id, asset_type, name, container_id, image, status) VALUES ('A-TEST', 'container', 'zeraxis-test-target', $1, 'alpine:latest', 'running')`, contResp.ID)

	execID := uuid.New().String()
	_, _ = db.Exec(`INSERT INTO execution_authorizations (execution_id, recommendation_id, approver_id, status, approved_at) VALUES ($1, $2, 'user-admin', 'APPROVED', NOW())`, execID, recID)

	// 3. Execute
	dockerWrapper, _ := docker.NewRealClient()
	executor := NewExecutor(db, dockerWrapper, nil)

	err = executor.Execute(ctx, execID)
	if err != nil {
		t.Fatalf("Execution failed: %v", err)
	}

	// 4. Verify Docker State
	postInspect, _ := cli.ContainerInspect(ctx, contResp.ID)
	if postInspect.State.Running {
		t.Fatalf("Container should be stopped, but is still running")
	}

	// 5. Verify DB Results
	var finalStatus string
	_ = db.QueryRow(`SELECT status FROM execution_results WHERE execution_id = $1`, execID).Scan(&finalStatus)
	if finalStatus != "EXECUTED" {
		t.Fatalf("Expected EXECUTED status, got %s", finalStatus)
	}
}
TEST
