package api_test

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/pavanvanish/zeraxis/engines/correlation/internal/api"
	"github.com/pavanvanish/zeraxis/engines/correlation/pkg/approvals"
)

func setupTestDB(db *sql.DB) error {
	_, err := db.Exec(`
		DROP SCHEMA public CASCADE;
		CREATE SCHEMA public;
		
		CREATE TABLE incidents (
			incident_id UUID PRIMARY KEY,
			fingerprint TEXT NOT NULL,
			status TEXT NOT NULL,
			risk_level TEXT NOT NULL,
			first_seen TIMESTAMPTZ NOT NULL,
			last_seen TIMESTAMPTZ NOT NULL,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		);
		
		CREATE TABLE incident_recommendations (
			recommendation_id UUID PRIMARY KEY,
			incident_id UUID NOT NULL REFERENCES incidents(incident_id),
			fingerprint TEXT NOT NULL,
			action TEXT NOT NULL,
			model_version TEXT NOT NULL,
			status TEXT NOT NULL,
			priority TEXT NOT NULL,
			reason TEXT NOT NULL,
			evidence JSONB NOT NULL,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		);
		
		CREATE TABLE execution_authorizations (
			execution_id UUID PRIMARY KEY,
			recommendation_id UUID NOT NULL REFERENCES incident_recommendations(recommendation_id) ON DELETE CASCADE,
			approver_id TEXT NOT NULL,
			status TEXT NOT NULL,
			approved_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
		
		CREATE TABLE execution_state_transitions (
			transition_id UUID PRIMARY KEY,
			execution_id UUID NOT NULL REFERENCES execution_authorizations(execution_id) ON DELETE CASCADE,
			from_status TEXT,
			to_status TEXT NOT NULL,
			actor_type TEXT NOT NULL,
			actor_id TEXT NOT NULL,
			timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			reason TEXT,
			correlation_id TEXT,
			trace_id TEXT
		);
	`)
	return err
}

func getDB(t *testing.T) *sql.DB {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:5433/zeraxis?sslmode=disable"
	}
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		t.Fatalf("Failed to connect to DB: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Skipf("DB not available: %v", err)
	}
	
	if err := setupTestDB(db); err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	
	return db
}

func setupRec(t *testing.T, db *sql.DB, status string) string {
	incID := uuid.New().String()
	_, _ = db.Exec(`INSERT INTO incidents (incident_id, fingerprint, status, risk_level, first_seen, last_seen) VALUES ($1, $2, 'OPEN', 'CRITICAL', NOW(), NOW())`, incID, uuid.New().String())

	recID := uuid.New().String()
	_, _ = db.Exec(`INSERT INTO incident_recommendations (recommendation_id, incident_id, fingerprint, action, model_version, status, priority, reason, evidence) VALUES ($1, $2, $3, 'STOP_CONTAINER', 'v1', $4, 'HIGH', 'test', '{}')`, recID, incID, uuid.New().String(), status)
	return recID
}

func setupExec(t *testing.T, db *sql.DB, recID, status string) string {
	execID := uuid.New().String()
	_, err := db.Exec(`INSERT INTO execution_authorizations (execution_id, recommendation_id, approver_id, status) VALUES ($1, $2, 'sys', $3)`, execID, recID, status)
	if err != nil {
		t.Fatalf("setup exec: %v", err)
	}
	return execID
}

func TestAPI_UnauthorizedApprove(t *testing.T) {
	os.Setenv("API_INTERNAL_SECRET", "secret123")
	defer os.Unsetenv("API_INTERNAL_SECRET")
	db := getDB(t)
	defer db.Close()
	svc := approvals.NewService(db, nil)
	srv := api.New(nil, nil, svc, "8080")

	recID := setupRec(t, db, "PROPOSED")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/recommendations/"+recID+"/approve", nil)
	req.Header.Set("X-Internal-Secret", "secret123")
	// NO X-Forwarded-User Header

	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("Expected 401 Unauthorized, got %d", rr.Code)
	}
}

func TestAPI_ValidApprove(t *testing.T) {
	os.Setenv("API_INTERNAL_SECRET", "secret123")
	defer os.Unsetenv("API_INTERNAL_SECRET")
	db := getDB(t)
	defer db.Close()
	svc := approvals.NewService(db, nil)
	srv := api.New(nil, nil, svc, "8080")

	recID := setupRec(t, db, "PROPOSED")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/recommendations/"+recID+"/approve", nil)
	req.Header.Set("X-Forwarded-User", "admin@trusted.gateway")
	req.Header.Set("X-Internal-Secret", "secret123") // empty env allows it, but let's just set it.

	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestAPI_ApproveInvalidState(t *testing.T) {
	os.Setenv("API_INTERNAL_SECRET", "secret123")
	defer os.Unsetenv("API_INTERNAL_SECRET")
	db := getDB(t)
	defer db.Close()
	svc := approvals.NewService(db, nil)
	srv := api.New(nil, nil, svc, "8080")

	recID := setupRec(t, db, "APPROVED")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/recommendations/"+recID+"/approve", nil)
	req.Header.Set("X-Forwarded-User", "admin")
	req.Header.Set("X-Internal-Secret", "secret123")

	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusConflict { // 409 Conflict
		t.Fatalf("Expected 409 Conflict, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestAPI_RevokeApproved(t *testing.T) {
	os.Setenv("API_INTERNAL_SECRET", "secret123")
	defer os.Unsetenv("API_INTERNAL_SECRET")
	db := getDB(t)
	defer db.Close()
	svc := approvals.NewService(db, nil)
	srv := api.New(nil, nil, svc, "8080")

	recID := setupRec(t, db, "APPROVED")
	_ = setupExec(t, db, recID, "APPROVED")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/recommendations/"+recID+"/revoke", nil)
	req.Header.Set("X-Forwarded-User", "admin")
	req.Header.Set("X-Internal-Secret", "secret123")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestAPI_RevokeExecutingRejection(t *testing.T) {
	os.Setenv("API_INTERNAL_SECRET", "secret123")
	defer os.Unsetenv("API_INTERNAL_SECRET")
	db := getDB(t)
	defer db.Close()
	svc := approvals.NewService(db, nil)
	srv := api.New(nil, nil, svc, "8080")

	recID := setupRec(t, db, "APPROVED")
	_ = setupExec(t, db, recID, "EXECUTING") // Auth is executing

	req := httptest.NewRequest(http.MethodPost, "/api/v1/recommendations/"+recID+"/revoke", nil)
	req.Header.Set("X-Forwarded-User", "admin")
	req.Header.Set("X-Internal-Secret", "secret123")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusConflict {
		t.Fatalf("Expected 409 Conflict, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestAPI_AbandonExecuting(t *testing.T) {
	os.Setenv("API_INTERNAL_SECRET", "secret123")
	defer os.Unsetenv("API_INTERNAL_SECRET")
	db := getDB(t)
	defer db.Close()
	svc := approvals.NewService(db, nil)
	srv := api.New(nil, nil, svc, "8080")

	recID := setupRec(t, db, "APPROVED")
	_ = setupExec(t, db, recID, "EXECUTING") // Auth is executing

	req := httptest.NewRequest(http.MethodPost, "/api/v1/recommendations/"+recID+"/abandon", nil)
	req.Header.Set("X-Forwarded-User", "admin")
	req.Header.Set("X-Internal-Secret", "secret123")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d: %s", rr.Code, rr.Body.String())
	}
	
	// Re-approval Safety Proof:
	// Try to approve the abandoned recommendation. It's now MANUAL_INVESTIGATION_REQUIRED.
	reqApprove := httptest.NewRequest(http.MethodPost, "/api/v1/recommendations/"+recID+"/approve", nil)
	reqApprove.Header.Set("X-Forwarded-User", "admin")
	reqApprove.Header.Set("X-Internal-Secret", "secret123")
	rrApprove := httptest.NewRecorder()
	srv.ServeHTTP(rrApprove, reqApprove)
	
	if rrApprove.Code != http.StatusConflict {
		t.Fatalf("Expected 409 Conflict when re-approving MANUAL_INVESTIGATION_REQUIRED, got %d", rrApprove.Code)
	}
}

func TestAPI_AbandonNonExecutingRejection(t *testing.T) {
	os.Setenv("API_INTERNAL_SECRET", "secret123")
	defer os.Unsetenv("API_INTERNAL_SECRET")
	db := getDB(t)
	defer db.Close()
	svc := approvals.NewService(db, nil)
	srv := api.New(nil, nil, svc, "8080")

	recID := setupRec(t, db, "APPROVED")
	_ = setupExec(t, db, recID, "APPROVED")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/recommendations/"+recID+"/abandon", nil)
	req.Header.Set("X-Forwarded-User", "admin")
	req.Header.Set("X-Internal-Secret", "secret123")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusConflict {
		t.Fatalf("Expected 409 Conflict, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestAPI_SpoofedIdentityRejected(t *testing.T) {
	os.Setenv("API_INTERNAL_SECRET", "secret123")
	defer os.Unsetenv("API_INTERNAL_SECRET")

	db := getDB(t)
	defer db.Close()
	svc := approvals.NewService(db, nil)
	srv := api.New(nil, nil, svc, "8080")

	recID := setupRec(t, db, "PROPOSED")
	
	// Malicious container trying to spoof the header directly
	req := httptest.NewRequest(http.MethodPost, "/api/v1/recommendations/"+recID+"/approve", nil)
	req.Header.Set("X-Forwarded-User", "attacker")
	// Misses X-Internal-Secret

	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("Expected 403 Forbidden for untrusted caller, got %d", rr.Code)
	}
}
