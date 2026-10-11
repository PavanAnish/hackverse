package tests

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/pavanvanish/zeraxis/engines/detection/evaluator"
	"github.com/pavanvanish/zeraxis/engines/detection/fingerprint"
	"github.com/pavanvanish/zeraxis/engines/detection/models"
	"github.com/pavanvanish/zeraxis/engines/detection/repository"
	"github.com/pavanvanish/zeraxis/pkg/events"
)

func TestCELEvaluator(t *testing.T) {
	rules := []models.Rule{
		{
			RuleID: "rule-unexpected-shell", RuleVersion: 1, Name: "Shell", Severity: "HIGH", Confidence: "HIGH", Enabled: true, RequiredEventTypes: []string{"PROCESS_EXECUTION"},
			Conditions: `event.metadata.process in ["sh", "bash"]`,
			GroupByFields: []string{"metadata.process"},
		},
		{
			RuleID: "rule-invalid", RuleVersion: 1, Enabled: true, RequiredEventTypes: []string{"PROCESS_EXECUTION"},
			Conditions: `event.metadata.process === "bad"`, // Invalid CEL syntax
		},
	}

	engine, err := evaluator.NewEngine(rules)
	if err == nil {
		t.Fatal("Expected error building engine with invalid CEL")
	}

	// Retry with valid rules
	engine, err = evaluator.NewEngine(rules[:1])
	if err != nil {
		t.Fatalf("Failed to build valid engine: %v", err)
	}

	evt := events.Event{
		EventType: "PROCESS_EXECUTION",
		Metadata:  []byte(`{"process": "bash"}`),
	}

	matches, err := engine.EvaluateStateless(evt, nil)
	if err != nil {
		t.Fatalf("Evaluation failed: %v", err)
	}

	if len(matches) != 1 || matches[0].RuleID != "rule-unexpected-shell" {
		t.Fatalf("Expected 1 match for rule-unexpected-shell, got: %v", matches)
	}
}

func TestFingerprint(t *testing.T) {
	fp1 := fingerprint.Generate("rule1", 1, "asset1", "digest1", map[string]string{"process": "sh"})
	fp2 := fingerprint.Generate("rule1", 1, "asset1", "digest1", map[string]string{"process": "sh"})
	fp3 := fingerprint.Generate("rule1", 1, "asset1", "digest1", map[string]string{"process": "bash"}) // different group

	if fp1 != fp2 {
		t.Fatal("Deterministic fingerprints for same inputs did not match")
	}
	if fp1 == fp3 {
		t.Fatal("Fingerprints for different inputs matched")
	}
}

func TestIntegrationDB(t *testing.T) {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:5433/zeraxis?sslmode=disable"
	}
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		t.Skip("Database not available for integration test")
		return
	}
	if err := db.Ping(); err != nil {
		t.Skip("Database ping failed")
		return
	}

	repo := repository.New(db)
	repo.EnsureSchema(context.Background())

	f := models.Finding{
		Fingerprint: "test-fp-" + uuid.New().String(),
		RuleID:      "test-rule",
		RuleVersion: 1,
		Severity:    "HIGH",
		Confidence:  "HIGH",
		Status:      "OPEN",
		FirstSeen:   time.Now(),
		LastSeen:    time.Now(),
		Evidence:    "test",
	}

	// 1. First event
	eventID1 := uuid.New().String()
	findingID, isNew, err := repo.UpsertFinding(context.Background(), f, eventID1)
	if err != nil {
		t.Fatalf("Failed to upsert first: %v", err)
	}
	if !isNew {
		t.Fatal("Expected finding to be new")
	}

	// 2. Second event (same fingerprint)
	eventID2 := uuid.New().String()
	f.LastSeen = time.Now()
	findingID2, isNew2, err := repo.UpsertFinding(context.Background(), f, eventID2)
	if err != nil {
		t.Fatalf("Failed to upsert second: %v", err)
	}
	if isNew2 {
		t.Fatal("Expected finding to be updated, not new")
	}
	if findingID != findingID2 {
		t.Fatal("Finding IDs should match")
	}

	// 3. Verify counts
	var count int
	db.QueryRow("SELECT COUNT(*) FROM finding_events WHERE finding_id = $1", findingID).Scan(&count)
	if count != 2 {
		t.Fatalf("Expected 2 finding_events, got %d", count)
	}

	// 4. Test duplicate finding_event insertion prevention
	_, _, err = repo.UpsertFinding(context.Background(), f, eventID2)
	if err != nil {
		t.Fatalf("Failed to upsert duplicate event: %v", err)
	}
	db.QueryRow("SELECT COUNT(*) FROM finding_events WHERE finding_id = $1", findingID).Scan(&count)
	if count != 2 {
		t.Fatalf("Expected duplicate finding_event to be ignored, count is %d", count)
	}
}
