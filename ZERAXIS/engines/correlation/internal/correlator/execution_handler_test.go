package correlator_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pavanvanish/zeraxis/engines/correlation/internal/correlator"
	"github.com/pavanvanish/zeraxis/engines/correlation/internal/models"
	"github.com/pavanvanish/zeraxis/engines/correlation/pkg/constants"
	pkgevents "github.com/pavanvanish/zeraxis/pkg/events"
)

func insertTestExecutionRecord(t *testing.T, db *sql.DB, incStatus, errLog string, parentRecoveryFor *string) (incID, execID, recID string) {
	t.Helper()
	incID = uuid.New().String()
	recID = uuid.New().String()
	execID = uuid.New().String()

	_, err := db.Exec(`INSERT INTO incidents (incident_id, fingerprint, status, severity, finding_count, first_seen, last_seen) VALUES ($1, $2, $3, 'HIGH', 1, NOW(), NOW())`, incID, incID, incStatus)
	if err != nil {
		t.Fatalf("insert incident: %v", err)
	}

	if parentRecoveryFor != nil {
		pRecID := uuid.New().String()
		_, _ = db.Exec(`
			INSERT INTO incident_recommendations (recommendation_id, incident_id, fingerprint, action, status, priority, reason, evidence, model_version)
			VALUES ($1, $2, $3, 'STOP_CONTAINER', 'EXECUTED', 'HIGH', 'Test', '{}', '1.0')
		`, pRecID, incID, uuid.New().String())
		_, _ = db.Exec(`
			INSERT INTO execution_authorizations (execution_id, recommendation_id, approver_id, status)
			VALUES ($1, $2, 'user1', 'EXECUTED')
		`, *parentRecoveryFor, pRecID)
		_, _ = db.Exec(`
			INSERT INTO execution_results (execution_id, recommendation_id, status, verification_status, error_log)
			VALUES ($1, $2, 'FAILED', 'FAILED', '')
		`, *parentRecoveryFor, pRecID)
	}

	_, err = db.Exec(`
		INSERT INTO incident_recommendations (
			recommendation_id, incident_id, fingerprint, action, status, priority, reason, evidence, model_version, recovery_for_execution_id, target_container_id
		) VALUES ($1, $2, $3, 'STOP_CONTAINER', 'APPROVED', 'HIGH', 'Test', '{}', '1.0', $4, 'test-cont')
	`, recID, incID, recID, parentRecoveryFor)
	if err != nil {
		t.Fatalf("insert recommendation: %v", err)
	}

	_, err = db.Exec(`
		INSERT INTO execution_authorizations (execution_id, recommendation_id, approver_id, status)
		VALUES ($1, $2, 'user1', 'EXECUTED')
	`, execID, recID)
	if err != nil {
		t.Fatalf("insert auth: %v", err)
	}

	_, err = db.Exec(`
		INSERT INTO execution_results (execution_id, recommendation_id, status, error_log)
		VALUES ($1, $2, 'FAILED', $3)
	`, execID, recID, errLog)
	if err != nil {
		t.Fatalf("insert result: %v", err)
	}

	return incID, execID, recID
}

func makeExecutionEvent(execID string) []byte {
	ev := pkgevents.NewEvent(
		"EXECUTION_FAILED",
		"executor",
		time.Now().UTC(),
	)
	ev.Metadata = json.RawMessage(fmt.Sprintf(`{"execution_id": "%s", "error": "mock error"}`, execID))
	b, _ := json.Marshal(ev)
	return b
}

type MockPublisher struct {
	mu     sync.Mutex
	events []*pkgevents.Event
}

func (m *MockPublisher) Publish(ctx context.Context, ev *pkgevents.Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, ev)
	return nil
}

func (m *MockPublisher) Close() {}

func TestExecutionHandler_RecoveryGeneration(t *testing.T) {
	repo := testRepo(t)
	pub := &MockPublisher{}
	corr := correlator.New(repo, pub)
	ctx := context.Background()

	t.Run("Standard Recovery - Postcondition Not Met", func(t *testing.T) {
		pub.mu.Lock()
		pub.events = nil
		pub.mu.Unlock()

		incID, execID, _ := insertTestExecutionRecord(t, repo.DB(), "OPEN", constants.PrefixPostconditionNotMet+" state is running", nil)

		corr.HandleExecutionAudit(ctx, makeExecutionEvent(execID))

		recs, err := repo.GetIncidentRecommendations(ctx, incID)
		if err != nil {
			t.Fatal(err)
		}
		// Expect 2: the original one + the recovery one
		if len(recs) != 2 {
			t.Fatalf("expected 2 recommendations, got %d", len(recs))
		}

		var recoveryRec *models.Recommendation
		for i := range recs {
			if recs[i].RecoveryForExecutionID != nil {
				recoveryRec = &recs[i]
				break
			}
		}

		if recoveryRec == nil {
			t.Fatal("no recovery recommendation found")
		}

		if recoveryRec.Action != constants.ActionInvestigateProcess {
			t.Errorf("expected INVESTIGATE_PROCESS, got %s", recoveryRec.Action)
		}
		if recoveryRec.Status != "PROPOSED" {
			t.Errorf("expected PROPOSED, got %s", recoveryRec.Status)
		}
		if *recoveryRec.RecoveryForExecutionID != execID {
			t.Errorf("expected recovery for %s, got %v", execID, *recoveryRec.RecoveryForExecutionID)
		}
		expectedFP := fmt.Sprintf("recovery:%s:%s", execID, constants.ActionInvestigateProcess)
		// Check that the NATS event was emitted
		pub.mu.Lock()
		defer pub.mu.Unlock()
		if len(pub.events) != 1 {
			t.Fatalf("expected 1 event emitted, got %d", len(pub.events))
		}
		if pub.events[0].Subject != "zeraxis.events.correlation.recovery" {
			t.Errorf("wrong subject: %s", pub.events[0].Subject)
		}
		
		// Verify fingerprint via raw query
		var fp string
		_ = repo.DB().QueryRow("SELECT fingerprint FROM incident_recommendations WHERE recommendation_id = $1", recoveryRec.RecommendationID).Scan(&fp)
		if fp != expectedFP {
			t.Errorf("expected fingerprint %s, got %s", expectedFP, fp)
		}
	})

	t.Run("Duplicate Event - Idempotent ON CONFLICT DO NOTHING", func(t *testing.T) {
		pub.mu.Lock()
		pub.events = nil
		pub.mu.Unlock()

		incID, execID, _ := insertTestExecutionRecord(t, repo.DB(), "OPEN", constants.PrefixImageDigestMismatch, nil)
		evData := makeExecutionEvent(execID)

		// Fire twice sequentially
		corr.HandleExecutionAudit(ctx, evData)
		corr.HandleExecutionAudit(ctx, evData)

		recs, _ := repo.GetIncidentRecommendations(ctx, incID)
		if len(recs) != 2 {
			t.Fatalf("expected exactly 2 recommendations (1 orig + 1 recovery), got %d", len(recs))
		}

		// Only one event should be emitted
		pub.mu.Lock()
		defer pub.mu.Unlock()
		if len(pub.events) != 1 {
			t.Fatalf("expected exactly 1 event emitted, got %d", len(pub.events))
		}
	})

	t.Run("Concurrent Duplicate Processing", func(t *testing.T) {
		incID, execID, _ := insertTestExecutionRecord(t, repo.DB(), "OPEN", constants.PrefixTargetIdentityMismatch, nil)
		evData := makeExecutionEvent(execID)

		var wg sync.WaitGroup
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				corr.HandleExecutionAudit(ctx, evData)
			}()
		}
		wg.Wait()

		recs, _ := repo.GetIncidentRecommendations(ctx, incID)
		if len(recs) != 2 {
			t.Fatalf("expected exactly 2 recommendations despite concurrency, got %d", len(recs))
		}
	})

	t.Run("Max Depth Bound", func(t *testing.T) {
		// Mock that this execution itself was a recovery
		parentExec := uuid.New().String()
		incID, execID, _ := insertTestExecutionRecord(t, repo.DB(), "OPEN", "Mutation failed", &parentExec)

		corr.HandleExecutionAudit(ctx, makeExecutionEvent(execID))

		recs, _ := repo.GetIncidentRecommendations(ctx, incID)
		// Should still only be 2 (the parent and child inserted in the fixture, no new one generated)
		if len(recs) != 2 {
			t.Fatalf("expected no new recommendation generated due to max depth, got %d", len(recs))
		}
	})

	t.Run("Resolved Incident", func(t *testing.T) {
		incID, execID, _ := insertTestExecutionRecord(t, repo.DB(), "RESOLVED", "Mutation failed", nil)

		corr.HandleExecutionAudit(ctx, makeExecutionEvent(execID))

		recs, _ := repo.GetIncidentRecommendations(ctx, incID)
		if len(recs) != 1 {
			t.Fatalf("expected no recovery for resolved incident, got %d", len(recs))
		}
	})

	t.Run("No Recovery Mapped - Target Missing", func(t *testing.T) {
		incID, execID, _ := insertTestExecutionRecord(t, repo.DB(), "OPEN", constants.PrefixTargetMissing, nil)
		corr.HandleExecutionAudit(ctx, makeExecutionEvent(execID))
		recs, _ := repo.GetIncidentRecommendations(ctx, incID)
		if len(recs) != 1 {
			t.Fatalf("expected no recovery for TARGET_MISSING, got %d", len(recs))
		}
	})

	t.Run("No Recovery Mapped - Unknown Failure", func(t *testing.T) {
		incID, execID, _ := insertTestExecutionRecord(t, repo.DB(), "OPEN", "Some weird unrecognized error string 123", nil)
		corr.HandleExecutionAudit(ctx, makeExecutionEvent(execID))
		recs, _ := repo.GetIncidentRecommendations(ctx, incID)
		if len(recs) != 1 {
			t.Fatalf("expected no recovery for unknown failure, got %d", len(recs))
		}
	})

	t.Run("Valid EXECUTION_VERIFIED -> INCIDENT_RESOLVED", func(t *testing.T) {
		pub.mu.Lock()
		pub.events = nil
		pub.mu.Unlock()

		_, execID, _ := insertTestExecutionRecord(t, repo.DB(), "OPEN", "", nil)
		repo.DB().Exec(`UPDATE execution_results SET verification_status = 'VERIFIED' WHERE execution_id = $1`, execID)

		evData := []byte(fmt.Sprintf(`{"event_id":"%s", "event_type":"EXECUTION_VERIFIED", "metadata": {"details": "Execution %s verified: postcondition confirmed"}}`, uuid.New().String(), execID))
		corr.HandleExecutionAudit(ctx, evData)

		pub.mu.Lock()
		if len(pub.events) != 1 {
			t.Fatalf("Expected exactly 1 published event, got %d", len(pub.events))
		}
		if pub.events[0].EventType != "INCIDENT_RESOLVED" {
			t.Errorf("Expected INCIDENT_RESOLVED, got %s", pub.events[0].EventType)
		}
		pub.mu.Unlock()
	})

	t.Run("Duplicate EXECUTION_VERIFIED Event", func(t *testing.T) {
		pub.mu.Lock()
		pub.events = nil
		pub.mu.Unlock()

		_, execID, _ := insertTestExecutionRecord(t, repo.DB(), "OPEN", "", nil)
		repo.DB().Exec(`UPDATE execution_results SET verification_status = 'VERIFIED' WHERE execution_id = $1`, execID)

		evData := []byte(fmt.Sprintf(`{"event_id":"%s", "event_type":"EXECUTION_VERIFIED", "metadata": {"details": "Execution %s verified: postcondition confirmed"}}`, uuid.New().String(), execID))
		corr.HandleExecutionAudit(ctx, evData)
		corr.HandleExecutionAudit(ctx, evData) // Duplicate!

		pub.mu.Lock()
		if len(pub.events) != 1 {
			t.Fatalf("Expected exactly 1 published event despite duplicates, got %d", len(pub.events))
		}
		pub.mu.Unlock()
	})

	t.Run("Malformed or Missing execution_id", func(t *testing.T) {
		pub.mu.Lock()
		pub.events = nil
		pub.mu.Unlock()

		evData := []byte(`{"event_id":"` + uuid.New().String() + `", "event_type":"EXECUTION_VERIFIED", "metadata": {"details": "Execution   verified: malformed"}}`)
		corr.HandleExecutionAudit(ctx, evData)
		
		evDataMissing := []byte(`{"event_id":"` + uuid.New().String() + `", "event_type":"EXECUTION_VERIFIED", "metadata": {}}`)
		corr.HandleExecutionAudit(ctx, evDataMissing)

		pub.mu.Lock()
		if len(pub.events) != 0 {
			t.Fatalf("Expected 0 published events for malformed payload, got %d", len(pub.events))
		}
		pub.mu.Unlock()
	})

	t.Run("Non-EXECUTION_VERIFIED event handling", func(t *testing.T) {
		pub.mu.Lock()
		pub.events = nil
		pub.mu.Unlock()

		_, execID, _ := insertTestExecutionRecord(t, repo.DB(), "OPEN", "", nil)
		repo.DB().Exec(`UPDATE execution_results SET verification_status = 'VERIFIED' WHERE execution_id = $1`, execID)

		evData := []byte(fmt.Sprintf(`{"event_id":"%s", "event_type":"SOMETHING_ELSE", "metadata": {"details": "Execution %s verified"}}`, uuid.New().String(), execID))
		corr.HandleExecutionAudit(ctx, evData)

		pub.mu.Lock()
		if len(pub.events) != 0 {
			t.Fatalf("Expected 0 published events for unrelated event type, got %d", len(pub.events))
		}
		pub.mu.Unlock()
	})
}
