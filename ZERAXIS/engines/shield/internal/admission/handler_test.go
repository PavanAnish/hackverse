package admission_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/pavanvanish/zeraxis/engines/shield/internal/admission"
	"github.com/pavanvanish/zeraxis/engines/shield/internal/cache"
	"github.com/pavanvanish/zeraxis/engines/shield/internal/evaluator"
)

var samplePolicy = `
package shield.admission
default allowed = false
default block_reason = ""
allowed {
	not blocked
}
blocked {
	input.fingerprint == "KNOWN_CRITICAL"
}
block_reason = "CRITICAL_INCIDENT_MATCH" {
	input.fingerprint == "KNOWN_CRITICAL"
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

func setupServer(t *testing.T, db *sql.DB) (*admission.Server, *cache.MaterializedView) {
	c := cache.NewMaterializedView(db)
	policyID := uuid.New().String()
	db.Exec("INSERT INTO policy_definitions (policy_id, rego_text, checksum, policy_status, version_number) VALUES ($1, $2, $3, 'ACTIVE', 1)", policyID, samplePolicy, "hash1")

	eval := evaluator.NewOPAEvaluator(db, c)
	if err := eval.LoadPolicy(context.Background(), ""); err != nil {
		t.Fatalf("Failed to load policy: %v", err)
	}
	srv := admission.NewServer(":8094", eval)
	return srv, c
}

func performRequest(srv *admission.Server, method, path string, body []byte) *httptest.ResponseRecorder {
	req, _ := http.NewRequest(method, path, bytes.NewBuffer(body))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	return rr
}

func TestEvaluateHandler_Allowed(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	srv, _ := setupServer(t, db)

	req := evaluator.AdmissionRequest{Fingerprint: "CLEAN_FP"}
	b, _ := json.Marshal(req)

	rr := performRequest(srv, "POST", "/v1/admission/evaluate", b)
	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200 OK, got %d", rr.Code)
	}

	var resp admission.EvaluateResponse
	json.NewDecoder(rr.Body).Decode(&resp)
	if !resp.Allowed || resp.Reason != "" {
		t.Errorf("Expected allowed=true, reason='', got allowed=%v, reason=%s", resp.Allowed, resp.Reason)
	}
	if resp.DecisionID == "" {
		t.Errorf("Missing DecisionID")
	}
}

func TestEvaluateHandler_Blocked(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	srv, _ := setupServer(t, db)

	req := evaluator.AdmissionRequest{Fingerprint: "KNOWN_CRITICAL"}
	b, _ := json.Marshal(req)

	rr := performRequest(srv, "POST", "/v1/admission/evaluate", b)
	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200 OK, got %d", rr.Code)
	}

	var resp admission.EvaluateResponse
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp.Allowed || resp.Reason != "CRITICAL_INCIDENT_MATCH" {
		t.Errorf("Expected allowed=false, reason='CRITICAL_INCIDENT_MATCH', got allowed=%v, reason=%s", resp.Allowed, resp.Reason)
	}
}

func TestEvaluateHandler_MalformedRequest(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	srv, _ := setupServer(t, db)

	rr := performRequest(srv, "POST", "/v1/admission/evaluate", []byte(`{invalid_json`))
	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 Bad Request, got %d", rr.Code)
	}

	var resp admission.EvaluateResponse
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp.Allowed || resp.Reason != "INVALID_REQUEST" {
		t.Errorf("Expected allowed=false, reason='INVALID_REQUEST', got allowed=%v, reason=%s", resp.Allowed, resp.Reason)
	}
}

func TestEvaluateHandler_CacheMiss(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	srv, c := setupServer(t, db)

	// Fingerprint not in DB or cache, but allowed by policy
	req := evaluator.AdmissionRequest{Fingerprint: "UNKNOWN_FP"}
	b, _ := json.Marshal(req)

	rr := performRequest(srv, "POST", "/v1/admission/evaluate", b)
	var resp admission.EvaluateResponse
	json.NewDecoder(rr.Body).Decode(&resp)

	if !resp.Allowed {
		t.Errorf("Expected cache miss to be allowed by default policy, got blocked")
	}

	// Verify cache was populated via miss
	reason, allowed, _, _ := c.Get(context.Background(), "UNKNOWN_FP")
	if !allowed || reason != "" {
		t.Errorf("Cache was not populated correctly on miss")
	}
}

func TestHealthCheck(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	srv, _ := setupServer(t, db)

	rr := performRequest(srv, "GET", "/health", nil)
	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200 OK, got %d", rr.Code)
	}
	if rr.Body.String() != `{"status":"OK"}` {
		t.Errorf("Unexpected body: %s", rr.Body.String())
	}
}
