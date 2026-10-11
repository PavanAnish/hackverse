package breakglass_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/nats-io/nats.go"
	"github.com/pavanvanish/zeraxis/engines/shield/internal/breakglass"
	"github.com/pavanvanish/zeraxis/pkg/events"
)

func setupDetectorTest(t *testing.T) (*sql.DB, *breakglass.OverrideService, *breakglass.ElevationDetector, *nats.Conn) {
	db := setupTestDB(t) 
	
	// Ensure extra tables needed for detector tests exist
	db.Exec(`CREATE TABLE IF NOT EXISTS shield_blocked_users (user_id TEXT PRIMARY KEY, blocked_at TIMESTAMPTZ DEFAULT NOW(), reason TEXT)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS incidents (incident_id UUID PRIMARY KEY, type TEXT, user_id TEXT, severity TEXT, status TEXT)`)

	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		natsURL = "nats://localhost:4222"
	}
	nc, err := nats.Connect(natsURL)
	if err != nil {
		t.Fatalf("NATS connect failed: %v", err)
	}

	// Mock correlation engine subscriber
	nc.Subscribe("zeraxis.events.correlation.incident.policy_abuse", func(m *nats.Msg) {
		var evt events.PolicyAbuseIncidentEvent
		json.Unmarshal(m.Data, &evt)
		db.Exec(`INSERT INTO incidents (incident_id, type, user_id, severity, status) VALUES ($1, 'POLICY_ABUSE', $2, 'CRITICAL', 'OPEN')`, uuid.New().String(), evt.UserID)
	})

	det := breakglass.NewElevationDetector()
	det.Start(db, nc)

	val := breakglass.NewHMACValidator()
	svc := breakglass.NewOverrideService(db, val, "secret", det)

	return db, svc, det, nc
}

func TestElevationDetector_Below3_NoIncident(t *testing.T) {
	db, svc, _, nc := setupDetectorTest(t)
	defer db.Close()
	defer nc.Close()

	userID := uuid.New().String()
	svc.RequestOverride(context.Background(), "fp", userID, "res", "cluster-A")
	svc.RequestOverride(context.Background(), "fp", userID, "res", "cluster-A")

	time.Sleep(100 * time.Millisecond) // allow async processing
	var count int
	db.QueryRow("SELECT COUNT(*) FROM incidents WHERE user_id = $1", userID).Scan(&count)
	if count != 0 {
		t.Errorf("Expected 0 incidents, got %d", count)
	}
}

func TestElevationDetector_At3_IncidentCreated(t *testing.T) {
	db, svc, _, nc := setupDetectorTest(t)
	defer db.Close()
	defer nc.Close()

	userID := uuid.New().String()
	svc.RequestOverride(context.Background(), "fp", userID, "res", "cluster-A")
	svc.RequestOverride(context.Background(), "fp", userID, "res", "cluster-A")
	svc.RequestOverride(context.Background(), "fp", userID, "res", "cluster-A")

	time.Sleep(150 * time.Millisecond) 
	var count int
	db.QueryRow("SELECT COUNT(*) FROM incidents WHERE user_id = $1 AND type = 'POLICY_ABUSE'", userID).Scan(&count)
	if count != 1 {
		t.Errorf("Expected 1 incident, got %d", count)
	}
}

func TestElevationDetector_IncidentBlocksOverrides(t *testing.T) {
	db, svc, _, nc := setupDetectorTest(t)
	defer db.Close()
	defer nc.Close()

	userID := uuid.New().String()
	for i := 0; i < 3; i++ {
		svc.RequestOverride(context.Background(), "fp", userID, "res", "cluster-A")
	}

	time.Sleep(100 * time.Millisecond)
	
	_, err := svc.RequestOverride(context.Background(), "fp", userID, "res", "cluster-A")
	if err != breakglass.ErrUserBlocked {
		t.Errorf("Expected ErrUserBlocked, got %v", err)
	}
}

func TestElevationDetector_WindowExpiry(t *testing.T) {
	db, svc, _, nc := setupDetectorTest(t)
	defer db.Close()
	defer nc.Close()

	userID := uuid.New().String()
	
	// Manually insert requests older than 1h
	reqID1 := uuid.New().String()
	reqID2 := uuid.New().String()
	db.Exec(`INSERT INTO override_requests (request_id, fingerprint, requested_by, reason, cluster_id, status, approval_threshold, created_at) 
			 VALUES ($1, 'fp', $2, 'res', 'cluster-A', 'PENDING', 2, NOW() - INTERVAL '2 hours')`, reqID1, userID)
	db.Exec(`INSERT INTO override_requests (request_id, fingerprint, requested_by, reason, cluster_id, status, approval_threshold, created_at) 
			 VALUES ($1, 'fp', $2, 'res', 'cluster-A', 'PENDING', 2, NOW() - INTERVAL '2 hours')`, reqID2, userID)

	// Make 2 new requests (total 4, but only 2 in window)
	svc.RequestOverride(context.Background(), "fp", userID, "res", "cluster-A")
	svc.RequestOverride(context.Background(), "fp", userID, "res", "cluster-A")

	time.Sleep(100 * time.Millisecond)
	var count int
	db.QueryRow("SELECT COUNT(*) FROM incidents WHERE user_id = $1", userID).Scan(&count)
	if count != 0 {
		t.Errorf("Expected 0 incidents due to window expiry, got %d", count)
	}
}

func TestElevationDetector_ConcurrentRequests(t *testing.T) {
	db, svc, _, nc := setupDetectorTest(t)
	defer db.Close()
	defer nc.Close()

	userID := uuid.New().String()
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			svc.RequestOverride(context.Background(), "fp", userID, "res", "cluster-A")
		}()
	}
	wg.Wait()

	time.Sleep(150 * time.Millisecond)
	
	// Should only be 1 incident created
	var incidentCount int
	db.QueryRow("SELECT COUNT(*) FROM incidents WHERE user_id = $1", userID).Scan(&incidentCount)
	if incidentCount != 1 {
		t.Errorf("Expected exactly 1 incident, got %d", incidentCount)
	}

	// User should be blocked
	var blocked bool
	db.QueryRow("SELECT EXISTS(SELECT 1 FROM shield_blocked_users WHERE user_id = $1)", userID).Scan(&blocked)
	if !blocked {
		t.Errorf("Expected user to be blocked")
	}
}

func TestElevationDetector_AuditTrail(t *testing.T) {
	db, svc, _, nc := setupDetectorTest(t)
	defer db.Close()
	defer nc.Close()

	userID := uuid.New().String()
	for i := 0; i < 3; i++ {
		svc.RequestOverride(context.Background(), "fp", userID, "res", "cluster-A")
	}

	time.Sleep(100 * time.Millisecond)
	var count int
	db.QueryRow("SELECT COUNT(*) FROM policy_audit_trail WHERE event_type = 'ELEVATION_TRIGGERED' AND actor_id = $1", userID).Scan(&count)
	if count != 1 {
		t.Errorf("Expected 1 ELEVATION_TRIGGERED audit event, got %d", count)
	}
}
