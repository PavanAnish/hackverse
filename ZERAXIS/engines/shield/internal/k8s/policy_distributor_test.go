package k8s_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/pavanvanish/zeraxis/engines/shield/internal/k8s"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func setupDB(t *testing.T) *sql.DB {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:5433/zeraxis?sslmode=disable"
	}
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		t.Fatalf("DB not available: %v", err)
	}

	db.Exec("DROP SCHEMA IF EXISTS public CASCADE")
	db.Exec("CREATE SCHEMA public")
	
	schema := `
	CREATE TABLE policy_definitions (policy_id UUID PRIMARY KEY);
	CREATE TABLE k8s_policy_snapshots (
		snapshot_id UUID PRIMARY KEY,
		policy_id UUID NOT NULL,
		policy_version INT NOT NULL,
		rego_text TEXT NOT NULL,
		checksum TEXT NOT NULL,
		rollout_status TEXT NOT NULL, 
		rollout_started_at TIMESTAMPTZ,
		rollout_completed_at TIMESTAMPTZ,
		created_at TIMESTAMPTZ DEFAULT NOW(),
		FOREIGN KEY (policy_id) REFERENCES policy_definitions(policy_id)
	);
	CREATE TABLE k8s_cluster_policy_state (
		state_id UUID PRIMARY KEY,
		cluster_id TEXT NOT NULL UNIQUE,
		current_snapshot_id UUID NOT NULL,
		previous_snapshot_id UUID,
		applied_at TIMESTAMPTZ DEFAULT NOW(),
		FOREIGN KEY (current_snapshot_id) REFERENCES k8s_policy_snapshots(snapshot_id)
	);
	CREATE TABLE k8s_webhook_registrations (
		registration_id UUID PRIMARY KEY,
		cluster_id TEXT NOT NULL,
		webhook_name TEXT NOT NULL,
		service_namespace TEXT NOT NULL,
		service_name TEXT NOT NULL,
		failure_policy TEXT NOT NULL,
		timeout_seconds INT NOT NULL DEFAULT 2,
		registered_at TIMESTAMPTZ DEFAULT NOW(),
		last_heartbeat_at TIMESTAMPTZ
	);
	CREATE TABLE policy_audit_trail (
		entry_id UUID PRIMARY KEY,
		event_type TEXT NOT NULL,
		actor_id TEXT NOT NULL,
		cluster_id TEXT NOT NULL,
		details JSONB,
		created_at TIMESTAMPTZ DEFAULT NOW()
	);
	`
	_, err = db.Exec(schema)
	if err != nil {
		t.Fatalf("Schema failed: %v", err)
	}
	
	for i := 1; i <= 10; i++ {
		db.Exec(`INSERT INTO k8s_webhook_registrations (registration_id, cluster_id, webhook_name, service_namespace, service_name, failure_policy)
				 VALUES ($1, $2, 'hook', 'ns', 'svc', 'Fail')`, uuid.New().String(), fmt.Sprintf("cluster-%d", i))
	}
	
	return db
}

func TestPushPolicy_Valid(t *testing.T) {
	db := setupDB(t)
	defer db.Close()
	fakeClient := fake.NewSimpleClientset()
	rm := k8s.NewRolloutManager(db, fakeClient)
	pd := k8s.NewPolicyDistributor(db, rm)

	policyID := uuid.New().String()
	snapID := uuid.New().String()
	db.Exec(`INSERT INTO policy_definitions (policy_id) VALUES ($1)`, policyID)
	db.Exec(`INSERT INTO k8s_policy_snapshots (snapshot_id, policy_id, policy_version, rego_text, checksum, rollout_status) 
			 VALUES ($1, $2, 1, 'package shield', 'csum', 'PENDING')`, snapID, policyID)

	err := pd.PushPolicy(context.Background(), snapID, 1)
	if err != nil {
		t.Fatalf("PushPolicy failed: %v", err)
	}

	for i := 1; i <= 10; i++ {
		cm, err := fakeClient.CoreV1().ConfigMaps("zeraxis").Get(context.Background(), fmt.Sprintf("shield-policy-cluster-%d", i), metav1.GetOptions{})
		if err != nil || cm == nil {
			t.Errorf("ConfigMap missing for cluster-%d", i)
		} else if cm.Data["policy.rego"] != "package shield" {
			t.Errorf("Invalid configmap data")
		} else if cm.Immutable == nil || !*cm.Immutable {
			t.Errorf("Configmap not immutable")
		}
	}
}

func TestCanaryRollout_Progression(t *testing.T) {
	db := setupDB(t)
	defer db.Close()
	fakeClient := fake.NewSimpleClientset()
	rm := k8s.NewRolloutManager(db, fakeClient)

	policyID := uuid.New().String()
	snapID := uuid.New().String()
	db.Exec(`INSERT INTO policy_definitions (policy_id) VALUES ($1)`, policyID)
	db.Exec(`INSERT INTO k8s_policy_snapshots (snapshot_id, policy_id, policy_version, rego_text, checksum, rollout_status) 
			 VALUES ($1, $2, 1, 'package shield', 'csum', 'PENDING')`, snapID, policyID)

	err := rm.ApplyCanaryStage(context.Background(), snapID, 10)
	if err != nil { t.Fatalf("Stage 10 failed: %v", err) }
	
	cms, _ := fakeClient.CoreV1().ConfigMaps("zeraxis").List(context.Background(), metav1.ListOptions{})
	if len(cms.Items) != 1 {
		t.Errorf("Expected 1 cluster configmap for 10%%, got %d", len(cms.Items))
	}

	rm.ApplyCanaryStage(context.Background(), snapID, 50)
	cms, _ = fakeClient.CoreV1().ConfigMaps("zeraxis").List(context.Background(), metav1.ListOptions{})
	if len(cms.Items) != 5 {
		t.Errorf("Expected 5 cluster configmaps for 50%%, got %d", len(cms.Items))
	}
	
	rm.ApplyCanaryStage(context.Background(), snapID, 100)
	cms, _ = fakeClient.CoreV1().ConfigMaps("zeraxis").List(context.Background(), metav1.ListOptions{})
	if len(cms.Items) != 10 {
		t.Errorf("Expected 10 cluster configmaps for 100%%, got %d", len(cms.Items))
	}
}

func TestCanaryRollout_Rollback(t *testing.T) {
	db := setupDB(t)
	defer db.Close()
	fakeClient := fake.NewSimpleClientset()
	rm := k8s.NewRolloutManager(db, fakeClient)
	pd := k8s.NewPolicyDistributor(db, rm)

	policyID := uuid.New().String()
	snap1 := uuid.New().String()
	snap2 := uuid.New().String()
	db.Exec(`INSERT INTO policy_definitions (policy_id) VALUES ($1)`, policyID)
	db.Exec(`INSERT INTO k8s_policy_snapshots (snapshot_id, policy_id, policy_version, rego_text, checksum, rollout_status) 
			 VALUES ($1, $2, 1, 'v1', 'csum1', 'GENERAL_AVAILABILITY')`, snap1, policyID)
	db.Exec(`INSERT INTO k8s_policy_snapshots (snapshot_id, policy_id, policy_version, rego_text, checksum, rollout_status) 
			 VALUES ($1, $2, 2, 'v2', 'csum2', 'PENDING')`, snap2, policyID)

	pd.PushPolicy(context.Background(), snap1, 1)
	pd.PushPolicy(context.Background(), snap2, 2)
	
	cur, prev, _ := pd.GetPolicyState(context.Background(), "cluster-1")
	if cur != snap2 || prev != snap1 {
		t.Errorf("Expected v2/v1, got %s/%s", cur, prev)
	}

	err := pd.RollbackPolicy(context.Background(), snap2, snap1)
	if err != nil { t.Fatalf("Rollback failed: %v", err) }

	cur, prev, _ = pd.GetPolicyState(context.Background(), "cluster-1")
	if cur != snap1 || prev != snap2 {
		t.Errorf("Expected v1/v2 after rollback, got %s/%s", cur, prev)
	}

	var status string
	db.QueryRow("SELECT rollout_status FROM k8s_policy_snapshots WHERE snapshot_id = $1", snap2).Scan(&status)
	if status != "ROLLED_BACK" {
		t.Errorf("Expected ROLLED_BACK status, got %s", status)
	}
}

func TestRollout_RapidFire(t *testing.T) {
	db := setupDB(t)
	defer db.Close()
	fakeClient := fake.NewSimpleClientset()
	rm := k8s.NewRolloutManager(db, fakeClient)
	pd := k8s.NewPolicyDistributor(db, rm)

	policyID := uuid.New().String()
	db.Exec(`INSERT INTO policy_definitions (policy_id) VALUES ($1)`, policyID)

	for i := 1; i <= 5; i++ {
		snapID := uuid.New().String()
		db.Exec(`INSERT INTO k8s_policy_snapshots (snapshot_id, policy_id, policy_version, rego_text, checksum, rollout_status) 
				 VALUES ($1, $2, $3, 'v', 'csum', 'PENDING')`, snapID, policyID, i)
		err := pd.PushPolicy(context.Background(), snapID, i)
		if err != nil { t.Fatalf("Rapid fire %d failed: %v", i, err) }
	}
}

func TestCanaryRollout_Concurrent(t *testing.T) {
	db := setupDB(t)
	defer db.Close()
	fakeClient := fake.NewSimpleClientset()
	rm := k8s.NewRolloutManager(db, fakeClient)
	pd := k8s.NewPolicyDistributor(db, rm)

	policyID := uuid.New().String()
	db.Exec(`INSERT INTO policy_definitions (policy_id) VALUES ($1)`, policyID)

	var wg sync.WaitGroup
	for i := 1; i <= 5; i++ {
		wg.Add(1)
		go func(version int) {
			defer wg.Done()
			snapID := uuid.New().String()
			db.Exec(`INSERT INTO k8s_policy_snapshots (snapshot_id, policy_id, policy_version, rego_text, checksum, rollout_status) 
					 VALUES ($1, $2, $3, 'v', 'csum', 'PENDING')`, snapID, policyID, version)
			pd.PushPolicy(context.Background(), snapID, version)
		}(i)
	}
	wg.Wait()
}
