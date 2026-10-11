package evaluator_test

import (
	"context"
	"database/sql"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/pavanvanish/zeraxis/engines/shield/internal/cache"
	"github.com/pavanvanish/zeraxis/engines/shield/internal/evaluator"
)

var samplePolicy = `
package shield.admission

default allowed = false
default block_reason = "Denied by default"

allowed {
	not blocked
}

blocked {
	input.fingerprint == "KNOWN_CRITICAL"
}
block_reason = "CRITICAL_INCIDENT_MATCH" {
	input.fingerprint == "KNOWN_CRITICAL"
}

blocked {
	input.registry == "untrusted.io"
}
block_reason = "UNTRUSTED_REGISTRY" {
	input.registry == "untrusted.io"
}
`

var v2Policy = `
package shield.admission

default allowed = false
default block_reason = "Denied by default"

allowed {
	not blocked
}

blocked {
	input.fingerprint == "KNOWN_CRITICAL"
}
block_reason = "CRITICAL_INCIDENT_MATCH" {
	input.fingerprint == "KNOWN_CRITICAL"
}

blocked {
	input.registry == "untrusted.io"
}
block_reason = "UNTRUSTED_REGISTRY" {
	input.registry == "untrusted.io"
}

blocked {
	input.registry == "very-untrusted.io"
}
block_reason = "VERY_UNTRUSTED_REGISTRY" {
	input.registry == "very-untrusted.io"
}
`

func setupTestDB(t *testing.T) *sql.DB {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:5433/zeraxis?sslmode=disable"
	}
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		t.Skipf("DB not available: %v", err)
	}
	db.Exec("DROP SCHEMA IF EXISTS public CASCADE")
	db.Exec("CREATE SCHEMA public")

	schema := `
	CREATE TABLE policy_definitions (
		policy_id UUID PRIMARY KEY,
		rego_text TEXT NOT NULL,
		checksum TEXT NOT NULL UNIQUE,
		policy_status TEXT NOT NULL,
		version_number INT NOT NULL,
		created_at TIMESTAMPTZ DEFAULT NOW(),
		activated_at TIMESTAMPTZ
	);
	CREATE TABLE shield_cache_state (
		fingerprint TEXT PRIMARY KEY,
		block_reason TEXT,
		cached_at TIMESTAMPTZ DEFAULT NOW(),
		ttl_expires_at TIMESTAMPTZ,
		incident_id UUID,
		last_event_id TEXT
	);
	`
	_, err = db.Exec(schema)
	if err != nil {
		t.Fatalf("Failed to create schema: %v", err)
	}
	return db
}

func setupEvaluator(t *testing.T, db *sql.DB) *evaluator.OPAEvaluator {
	c := cache.NewMaterializedView(db)
	
	policyID := uuid.New().String()
	_, err := db.Exec("INSERT INTO policy_definitions (policy_id, rego_text, checksum, policy_status, version_number) VALUES ($1, $2, $3, 'ACTIVE', 1)", policyID, samplePolicy, "hash1")
	if err != nil {
		t.Fatalf("Failed to insert policy: %v", err)
	}

	eval := evaluator.NewOPAEvaluator(db, c)
	if err := eval.LoadPolicy(context.Background(), ""); err != nil {
		t.Fatalf("Failed to load policy: %v", err)
	}
	return eval
}

func TestPolicyEvaluation_BlockCritical(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	eval := setupEvaluator(t, db)

	req := evaluator.AdmissionRequest{
		Fingerprint: "KNOWN_CRITICAL",
		Registry:    "trusted.io",
	}

	start := time.Now()
	dec, err := eval.Evaluate(context.Background(), req)
	dur := time.Since(start)

	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	if dec.Allowed || dec.Reason != "CRITICAL_INCIDENT_MATCH" {
		t.Errorf("Expected block CRITICAL_INCIDENT_MATCH, got allowed=%v reason=%s", dec.Allowed, dec.Reason)
	}
	t.Logf("Eval latency (BlockCritical): %v (SLA < 50ms)", dur)
}

func TestPolicyEvaluation_AllowClean(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	eval := setupEvaluator(t, db)

	req := evaluator.AdmissionRequest{
		Fingerprint: "CLEAN_FINGERPRINT",
		Registry:    "trusted.io",
	}

	start := time.Now()
	dec, err := eval.Evaluate(context.Background(), req)
	dur := time.Since(start)

	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	if !dec.Allowed {
		t.Errorf("Expected allow, got blocked reason=%s", dec.Reason)
	}
	t.Logf("Eval latency (AllowClean): %v (SLA < 50ms)", dur)
}

func TestPolicyEvaluation_CacheBypass(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	
	c := cache.NewMaterializedView(db)
	
	policyID := uuid.New().String()
	db.Exec("INSERT INTO policy_definitions (policy_id, rego_text, checksum, policy_status, version_number) VALUES ($1, $2, $3, 'ACTIVE', 1)", policyID, samplePolicy, "hash1")
	
	eval := evaluator.NewOPAEvaluator(db, c)
	eval.LoadPolicy(context.Background(), "")

	// Set cache to block even though policy allows it
	c.Set(context.Background(), "CLEAN_FINGERPRINT", "CACHE_QUARANTINE", 5*time.Minute)

	req := evaluator.AdmissionRequest{
		Fingerprint: "CLEAN_FINGERPRINT", // Policy says allowed
		Registry:    "trusted.io",
	}

	start := time.Now()
	dec, err := eval.Evaluate(context.Background(), req)
	dur := time.Since(start)

	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	if dec.Allowed || dec.Reason != "CACHE_QUARANTINE" {
		t.Errorf("Expected cache block CACHE_QUARANTINE to win, got allowed=%v reason=%s", dec.Allowed, dec.Reason)
	}
	t.Logf("Eval latency (CacheBypass fast-path): %v (SLA < 50ms)", dur)
}

func TestPolicyCompilation_Invalid(t *testing.T) {
	_, err := evaluator.CompilePolicy("invalid rego text {")
	if err == nil {
		t.Errorf("Expected compile error for invalid rego")
	}
}

func TestPolicyVersioning(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	policy1 := uuid.New().String()
	db.Exec("INSERT INTO policy_definitions (policy_id, rego_text, checksum, policy_status, version_number) VALUES ($1, $2, $3, 'STAGING', 1)", policy1, samplePolicy, "hash1")
	
	policy2 := uuid.New().String()
	db.Exec("INSERT INTO policy_definitions (policy_id, rego_text, checksum, policy_status, version_number) VALUES ($1, $2, $3, 'ACTIVE', 2)", policy2, v2Policy, "hash2")

	c := cache.NewMaterializedView(db)
	eval := evaluator.NewOPAEvaluator(db, c)
	
	// Should load policy2 (ACTIVE)
	err := eval.LoadPolicy(context.Background(), "")
	if err != nil {
		t.Fatalf("Failed to load policy: %v", err)
	}

	req := evaluator.AdmissionRequest{
		Fingerprint: "CLEAN_FINGERPRINT",
		Registry:    "very-untrusted.io",
	}

	dec, _ := eval.Evaluate(context.Background(), req)
	if dec.Allowed || dec.Reason != "VERY_UNTRUSTED_REGISTRY" {
		t.Errorf("Expected block VERY_UNTRUSTED_REGISTRY from v2 policy, got allowed=%v reason=%s", dec.Allowed, dec.Reason)
	}
}

func TestConcurrentEvaluation(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	eval := setupEvaluator(t, db)

	req := evaluator.AdmissionRequest{
		Fingerprint: "CLEAN_FINGERPRINT",
		Registry:    "trusted.io",
	}

	var wg sync.WaitGroup
	workers := 100
	wg.Add(workers)

	start := time.Now()
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			dec, err := eval.Evaluate(context.Background(), req)
			if err != nil || !dec.Allowed {
				t.Errorf("Concurrent eval failed: err=%v, dec=%+v", err, dec)
			}
		}()
	}
	wg.Wait()
	t.Logf("100 concurrent evals latency: %v (SLA < 50ms total)", time.Since(start))
}
