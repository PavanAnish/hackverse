package integration_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
	"sync"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/nats-io/nats.go"

	"github.com/pavanvanish/zeraxis/engines/shield/internal/breakglass"
	shieldk8s "github.com/pavanvanish/zeraxis/engines/shield/internal/k8s"
	"github.com/pavanvanish/zeraxis/engines/shield/internal/multicluster"
	"github.com/pavanvanish/zeraxis/pkg/events"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

// MOCKS
type MockEvaluator struct {
	blockedImages map[string]string
}
func (m *MockEvaluator) Evaluate(ctx context.Context, fingerprint string) (bool, string, error) {
	if reason, ok := m.blockedImages[fingerprint]; ok {
		return false, reason, nil
	}
	return true, "", nil
}

type MockCache struct {
	mu            sync.Mutex
	invalidations map[string]int
}
func (m *MockCache) Invalidate(fingerprint string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.invalidations[fingerprint]++
	return nil
}
func (m *MockCache) GetCount(fingerprint string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.invalidations[fingerprint]
}

func setupE2EEnvironment(t *testing.T) (*sql.DB, *nats.Conn) {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:5433/zeraxis?sslmode=disable"
	}
	db, err := sql.Open("postgres", dbURL)
	if err != nil { t.Fatalf("DB failed: %v", err) }

	db.Exec("DROP SCHEMA IF EXISTS public CASCADE; CREATE SCHEMA public;")
	
	schema := `
	CREATE TABLE override_requests (
		request_id TEXT PRIMARY KEY, fingerprint TEXT, requested_by TEXT, reason TEXT,
		cluster_id TEXT, status TEXT, approval_count INT DEFAULT 0, approval_threshold INT,
		expires_at TIMESTAMPTZ DEFAULT NOW() + INTERVAL '1 hour', created_at TIMESTAMPTZ DEFAULT NOW(),
		executed_at TIMESTAMPTZ
	);
	CREATE TABLE override_approvals (
		approval_id TEXT PRIMARY KEY, override_request_id TEXT, approver_id TEXT,
		signature_hmac TEXT, created_at TIMESTAMPTZ DEFAULT NOW(),
		revoked_at TIMESTAMPTZ, can_revoke BOOLEAN DEFAULT TRUE,
		FOREIGN KEY (override_request_id) REFERENCES override_requests(request_id)
	);
	CREATE TABLE policy_audit_trail (
		entry_id TEXT PRIMARY KEY, event_type TEXT, actor_id TEXT, cluster_id TEXT,
		details JSONB, created_at TIMESTAMPTZ DEFAULT NOW()
	);
	CREATE TABLE shield_blocked_users (
		user_id TEXT PRIMARY KEY, blocked_at TIMESTAMPTZ DEFAULT NOW(), reason TEXT
	);
	CREATE TABLE incidents (
		incident_id TEXT PRIMARY KEY, type TEXT, user_id TEXT, severity TEXT, status TEXT
	);
	CREATE TABLE policy_definitions (policy_id TEXT PRIMARY KEY);
	CREATE TABLE k8s_policy_snapshots (
		snapshot_id TEXT PRIMARY KEY, policy_id TEXT, policy_version INT,
		rego_text TEXT, checksum TEXT, rollout_status TEXT, rollout_started_at TIMESTAMPTZ,
		rollout_completed_at TIMESTAMPTZ, created_at TIMESTAMPTZ DEFAULT NOW()
	);
	CREATE TABLE k8s_cluster_policy_state (
		state_id TEXT PRIMARY KEY, cluster_id TEXT UNIQUE, current_snapshot_id TEXT,
		previous_snapshot_id TEXT, applied_at TIMESTAMPTZ DEFAULT NOW()
	);
	CREATE TABLE k8s_webhook_registrations (
		registration_id TEXT PRIMARY KEY, cluster_id TEXT, webhook_name TEXT,
		service_namespace TEXT, service_name TEXT, failure_policy TEXT,
		timeout_seconds INT DEFAULT 2, registered_at TIMESTAMPTZ DEFAULT NOW(),
		last_heartbeat_at TIMESTAMPTZ
	);
	`
	_, err = db.Exec(schema)
	if err != nil { t.Fatalf("Schema failed: %v", err) }

	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" { natsURL = "nats://localhost:4222" }
	nc, err := nats.Connect(natsURL)
	if err != nil { t.Fatalf("NATS failed: %v", err) }

	// Mock Correlation Engine Subscriber for Incident Creation
	nc.Subscribe("zeraxis.events.correlation.incident.policy_abuse", func(m *nats.Msg) {
		var evt events.PolicyAbuseIncidentEvent
		json.Unmarshal(m.Data, &evt)
		db.Exec(`INSERT INTO incidents (incident_id, type, user_id, severity, status) VALUES ($1, 'POLICY_ABUSE', $2, 'CRITICAL', 'OPEN')`, uuid.New().String(), evt.UserID)
	})
	nc.Flush()

	return db, nc
}

func buildReview(image string) []byte {
	pod := corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{Image: image}}}}
	podBytes, _ := json.Marshal(pod)
	review := admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{Kind: "AdmissionReview", APIVersion: "admission.k8s.io/v1"},
		Request: &admissionv1.AdmissionRequest{UID: "e2e-uid", Object: runtime.RawExtension{Raw: podBytes}},
	}
	b, _ := json.Marshal(review)
	return b
}

func TestPhase9_EndToEnd_BreakGlassToMultiCluster(t *testing.T) {
	ctx := context.Background()
	db, nc := setupE2EEnvironment(t)
	defer db.Close()
	defer nc.Close()

	// Initialize Break-Glass
	det := breakglass.NewElevationDetector()
	det.Start(db, nc)
	val := breakglass.NewHMACValidator()
	overrideSvc := breakglass.NewOverrideService(db, val, "secret123", det)

	// Initialize K8s components
	fakeClient := fake.NewSimpleClientset()
	rm := shieldk8s.NewRolloutManager(db, fakeClient)
	
	
	evaluator := &MockEvaluator{blockedImages: map[string]string{"malicious:latest": "CRITICAL_BLOCK"}}
	webhookHandler := shieldk8s.NewWebhookHandler(evaluator)

	// Initialize NATS Coherency for 3 clusters
	cacheA := &MockCache{invalidations: make(map[string]int)}
	cacheB := &MockCache{invalidations: make(map[string]int)}
	cacheC := &MockCache{invalidations: make(map[string]int)}
	
	cohA := multicluster.NewNATSCoherencyManager(); cohA.Start(nc, cacheA, "cluster-A")
	cohB := multicluster.NewNATSCoherencyManager(); cohB.Start(nc, cacheB, "cluster-B")
	cohC := multicluster.NewNATSCoherencyManager(); cohC.Start(nc, cacheC, "cluster-C")
	defer cohA.Stop(); defer cohB.Stop(); defer cohC.Stop()

	// Seed 10 clusters for rollout
	for i := 1; i <= 10; i++ {
		db.Exec(`INSERT INTO k8s_webhook_registrations (registration_id, cluster_id, webhook_name, service_namespace, service_name, failure_policy) VALUES ($1, $2, 'hook', 'ns', 'svc', 'Fail')`, uuid.New().String(), fmt.Sprintf("cluster-%d", i))
	}

	t.Log("=== STEP 1: BREAK-GLASS WORKFLOW ===")
	reqID, err := overrideSvc.RequestOverride(ctx, "fp-critical", "user-1", "emergency", "cluster-A")
	if err != nil { t.Fatalf("RequestOverride failed: %v", err) }

	sig1 := val.GenerateSignature(reqID, "approver-1", "secret123")
	sig2 := val.GenerateSignature(reqID, "approver-2", "secret123")
	
	_, thresholdMet, _ := overrideSvc.ApproveOverride(ctx, reqID, "approver-1", sig1)
	if thresholdMet { t.Fatalf("Threshold met too early") }
	_, thresholdMet, _ = overrideSvc.ApproveOverride(ctx, reqID, "approver-2", sig2)
	if !thresholdMet { t.Fatalf("Threshold not met after 2 approvals") }

	status, _, _ := overrideSvc.GetOverrideStatus(ctx, reqID)
	if status != "APPROVED" { t.Fatalf("Expected APPROVED, got %s", status) }
	
	err = overrideSvc.ExecuteOverride(ctx, reqID)
	if err != nil { t.Fatalf("ExecuteOverride failed: %v", err) }
	evaluator.blockedImages["fp-critical"] = "" // Simulating cache allow update

	t.Log("=== STEP 2: POLICY ABUSE DETECTION ===")
	overrideSvc.RequestOverride(ctx, "fp-1", "user-1", "reason", "cluster-A")
	overrideSvc.RequestOverride(ctx, "fp-2", "user-1", "reason", "cluster-A") // 3 total within 1h
	time.Sleep(150 * time.Millisecond) // Allow NATS correlation to process

	_, err = overrideSvc.RequestOverride(ctx, "fp-4", "user-1", "reason", "cluster-A")
	if err != breakglass.ErrUserBlocked { t.Fatalf("Expected user to be blocked, got %v", err) }
	
	var incCount int
	db.QueryRow("SELECT COUNT(*) FROM incidents WHERE user_id = 'user-1' AND type = 'POLICY_ABUSE' AND severity = 'CRITICAL'").Scan(&incCount)
	if incCount != 1 { t.Fatalf("Expected exactly 1 POLICY_ABUSE incident") }

	t.Log("=== STEP 3: K8S WEBHOOK ENFORCEMENT ===")
	req := httptest.NewRequest("POST", "/v1/k8s/validate", bytes.NewReader(buildReview("malicious:latest")))
	rr := httptest.NewRecorder()
	
	start := time.Now()
	webhookHandler.ServeHTTP(rr, req)
	latency := time.Since(start)

	var resp admissionv1.AdmissionReview
	json.Unmarshal(rr.Body.Bytes(), &resp)

	if resp.Response == nil || resp.Response.Allowed { t.Fatalf("Expected pod to be blocked") }
	if !strings.Contains(resp.Response.Result.Message, "CRITICAL_BLOCK") { t.Fatalf("Wrong block reason") }
	if latency > time.Second { t.Fatalf("Webhook exceeded latency SLA: %v", latency) }

	t.Log("=== STEP 4: CANARY POLICY ROLLOUT ===")
	policyID := uuid.New().String()
	snapID := uuid.New().String()
	db.Exec(`INSERT INTO policy_definitions (policy_id) VALUES ($1)`, policyID)
	db.Exec(`INSERT INTO k8s_policy_snapshots (snapshot_id, policy_id, policy_version, rego_text, checksum, rollout_status) VALUES ($1, $2, 2, 'package v2', 'csum', 'PENDING')`, snapID, policyID)

	// Simulate Step-by-Step Canary Rollout
	rm.ApplyCanaryStage(ctx, snapID, 10)
	cms, _ := fakeClient.CoreV1().ConfigMaps("zeraxis").List(ctx, metav1.ListOptions{})
	if len(cms.Items) != 1 { t.Fatalf("10%% canary failed, got %d maps", len(cms.Items)) }

	rm.ApplyCanaryStage(ctx, snapID, 50)
	cms, _ = fakeClient.CoreV1().ConfigMaps("zeraxis").List(ctx, metav1.ListOptions{})
	if len(cms.Items) != 5 { t.Fatalf("50%% canary failed, got %d maps", len(cms.Items)) }

	rm.ApplyCanaryStage(ctx, snapID, 100)
	cms, _ = fakeClient.CoreV1().ConfigMaps("zeraxis").List(ctx, metav1.ListOptions{})
	if len(cms.Items) != 10 { t.Fatalf("100%% canary failed, got %d maps", len(cms.Items)) }

	t.Log("=== STEP 5: MULTI-CLUSTER COHERENCY ===")
	startInvalidation := time.Now()
	cohA.PublishResolved(ctx, "fp-resolved")
	time.Sleep(100 * time.Millisecond)
	cohLatency := time.Since(startInvalidation)

	if cacheB.GetCount("fp-resolved") != 1 || cacheC.GetCount("fp-resolved") != 1 {
		t.Fatalf("Cross-cluster cache coherency failed. B: %d, C: %d", cacheB.GetCount("fp-resolved"), cacheC.GetCount("fp-resolved"))
	}
	if cacheA.GetCount("fp-resolved") != 0 { t.Fatalf("Cluster A should not invalidate its own cache via network") }
	if cohLatency > 500*time.Millisecond { t.Fatalf("Cache invalidation SLA exceeded: %v", cohLatency) }

	t.Log("=== STEP 6: AUDIT TRAIL VERIFICATION ===")
	var auditCount int
	db.QueryRow("SELECT COUNT(*) FROM policy_audit_trail").Scan(&auditCount)
	if auditCount < 7 { t.Fatalf("Expected at least 7 audit events across all workflows, got %d", auditCount) }
}
