package breakglass_test

import (
	"context"
	"database/sql"
	"os"
	"testing"
	

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/pavanvanish/zeraxis/engines/shield/internal/breakglass"
)

func setupTestDB(t *testing.T) *sql.DB {
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
	CREATE TABLE override_requests (
		request_id UUID PRIMARY KEY,
		fingerprint TEXT NOT NULL,
		requested_by UUID NOT NULL,
		reason TEXT NOT NULL,
		cluster_id TEXT NOT NULL,
		status TEXT NOT NULL,
		approval_threshold INT NOT NULL,
		approval_count INT DEFAULT 0,
		created_at TIMESTAMPTZ DEFAULT NOW(),
		expires_at TIMESTAMPTZ DEFAULT NOW() + INTERVAL '1 hour',
		executed_at TIMESTAMPTZ
	);
	CREATE TABLE override_approvals (
		approval_id UUID PRIMARY KEY,
		override_request_id UUID NOT NULL,
		approver_id UUID NOT NULL,
		signature_hmac TEXT NOT NULL,
		signature_timestamp TIMESTAMPTZ DEFAULT NOW(),
		can_revoke BOOLEAN DEFAULT TRUE,
		revoked_at TIMESTAMPTZ,
		UNIQUE(override_request_id, approver_id)
	);
	CREATE TABLE policy_audit_trail (
		entry_id UUID PRIMARY KEY,
		event_type TEXT NOT NULL,
		actor_id TEXT NOT NULL,
		cluster_id TEXT NOT NULL,
		details JSONB,
		created_at TIMESTAMPTZ DEFAULT NOW()
	);
	CREATE OR REPLACE FUNCTION raise_immutable_error() RETURNS trigger AS $$
	BEGIN
		IF TG_OP = 'UPDATE' THEN
			IF NEW.revoked_at IS NOT NULL AND OLD.revoked_at IS NULL THEN
				RETURN NEW; -- allow specific revocation update
			END IF;
			RAISE EXCEPTION 'Table is immutable';
		END IF;
		IF TG_OP = 'DELETE' THEN
			RAISE EXCEPTION 'Table is immutable';
		END IF;
		RETURN NEW;
	END;
	$$ LANGUAGE plpgsql;
	CREATE TRIGGER prevent_override_approvals_modification
	BEFORE UPDATE OR DELETE ON override_approvals
	FOR EACH ROW EXECUTE FUNCTION raise_immutable_error();
	`
	_, err = db.Exec(schema)
	if err != nil {
		t.Fatalf("Schema failed: %v", err)
	}
	return db
}

func TestRequestOverride_Valid(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	val := breakglass.NewHMACValidator()
	svc := breakglass.NewOverrideService(db, val, "test_secret", nil)

	reqID, err := svc.RequestOverride(context.Background(), "fp_123", uuid.New().String(), "Emergency fix", "cluster-A")
	if err != nil || reqID == "" {
		t.Fatalf("RequestOverride failed: %v", err)
	}

	status, needed, err := svc.GetOverrideStatus(context.Background(), reqID)
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}
	if status != "PENDING" || needed != 2 {
		t.Errorf("Expected PENDING and 2 needed, got %s and %d", status, needed)
	}
}

func TestApprovalWorkflow_2Of2_Approved(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	val := breakglass.NewHMACValidator()
	svc := breakglass.NewOverrideService(db, val, "test_secret", nil)

	reqID, _ := svc.RequestOverride(context.Background(), "fp", uuid.New().String(), "res", "cluster-A")

	app1 := uuid.New().String()
	sig1 := val.GenerateSignature(reqID, app1, "test_secret")
	c1, met1, err := svc.ApproveOverride(context.Background(), reqID, app1, sig1)
	if err != nil || met1 || c1 != 1 {
		t.Fatalf("Approve 1 failed: err=%v, met=%v, c=%d", err, met1, c1)
	}

	app2 := uuid.New().String()
	sig2 := val.GenerateSignature(reqID, app2, "test_secret")
	c2, met2, err := svc.ApproveOverride(context.Background(), reqID, app2, sig2)
	if err != nil || !met2 || c2 != 2 {
		t.Fatalf("Approve 2 failed: err=%v, met=%v, c=%d", err, met2, c2)
	}

	status, _, _ := svc.GetOverrideStatus(context.Background(), reqID)
	if status != "APPROVED" {
		t.Errorf("Expected APPROVED status, got %s", status)
	}

	// Execution
	err = svc.ExecuteOverride(context.Background(), reqID)
	if err != nil {
		t.Errorf("Execution failed: %v", err)
	}

	status, _, _ = svc.GetOverrideStatus(context.Background(), reqID)
	if status != "EXECUTED" {
		t.Errorf("Expected EXECUTED status, got %s", status)
	}
}

func TestHMACValidation_Tampering(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	val := breakglass.NewHMACValidator()
	svc := breakglass.NewOverrideService(db, val, "test_secret", nil)

	reqID, _ := svc.RequestOverride(context.Background(), "fp", uuid.New().String(), "res", "cluster-A")
	app1 := uuid.New().String()
	
	// Tampered sig
	_, _, err := svc.ApproveOverride(context.Background(), reqID, app1, "invalid_sig")
	if err != breakglass.ErrInvalidSig {
		t.Fatalf("Expected ErrInvalidSig, got %v", err)
	}
}

func TestRevokeApproval_BeforeThreshold(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	val := breakglass.NewHMACValidator()
	svc := breakglass.NewOverrideService(db, val, "test_secret", nil)

	reqID, _ := svc.RequestOverride(context.Background(), "fp", uuid.New().String(), "res", "cluster-A")

	app1 := uuid.New().String()
	sig1 := val.GenerateSignature(reqID, app1, "test_secret")
	svc.ApproveOverride(context.Background(), reqID, app1, sig1)

	// Fetch approval ID directly for testing
	var approvalID string
	db.QueryRow("SELECT approval_id FROM override_approvals WHERE override_request_id = $1", reqID).Scan(&approvalID)

	err := svc.RevokeApproval(context.Background(), approvalID, app1)
	if err != nil {
		t.Fatalf("Revoke failed: %v", err)
	}

	status, needed, _ := svc.GetOverrideStatus(context.Background(), reqID)
	if status != "PENDING" || needed != 2 {
		t.Errorf("Expected PENDING and 2 needed after revoke, got %s and %d", status, needed)
	}
}

func TestRevokeApproval_AfterThreshold(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	val := breakglass.NewHMACValidator()
	svc := breakglass.NewOverrideService(db, val, "test_secret", nil)

	reqID, _ := svc.RequestOverride(context.Background(), "fp", uuid.New().String(), "res", "cluster-A")

	app1, app2 := uuid.New().String(), uuid.New().String()
	sig1 := val.GenerateSignature(reqID, app1, "test_secret")
	sig2 := val.GenerateSignature(reqID, app2, "test_secret")
	
	svc.ApproveOverride(context.Background(), reqID, app1, sig1)
	svc.ApproveOverride(context.Background(), reqID, app2, sig2) // Threshold met

	var approvalID string
	db.QueryRow("SELECT approval_id FROM override_approvals WHERE override_request_id = $1 AND approver_id = $2", reqID, app1).Scan(&approvalID)

	err := svc.RevokeApproval(context.Background(), approvalID, app1)
	if err != breakglass.ErrCannotRevoke {
		t.Fatalf("Expected ErrCannotRevoke, got %v", err)
	}
}
