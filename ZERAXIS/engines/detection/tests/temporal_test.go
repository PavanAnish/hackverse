package tests

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pavanvanish/zeraxis/engines/detection/evaluator"
	"github.com/pavanvanish/zeraxis/engines/detection/models"
	"github.com/pavanvanish/zeraxis/engines/detection/state"
	"github.com/pavanvanish/zeraxis/pkg/events"
)

func buildTestEngine(t *testing.T) (*evaluator.Engine, *state.Manager) {
	rules := []models.Rule{
		{
			RuleID: "rule-test-seq", RuleVersion: 1, Enabled: true,
			TimeWindow: 10 * time.Second,
			Sequence: []models.SequenceStep{
				{Name: "step1", EventType: "A", Condition: `event.metadata.val == "start"`},
				{Name: "step2", EventType: "B", Condition: `event.metadata.val == "mid"`},
				{Name: "step3", EventType: "C", Condition: `event.metadata.val == "end"`},
			},
		},
	}
	eng, err := evaluator.NewEngine(rules)
	if err != nil {
		t.Fatalf("Failed to build engine: %v", err)
	}
	return eng, state.NewManager(eng)
}

func makeEvent(typ, val, cid string, ts time.Time) events.Event {
	meta := map[string]interface{}{"val": val, "container_id": cid}
	b, _ := json.Marshal(meta)
	return events.Event{
		EventID:   uuid.NewString(),
		EventType: typ,
		Timestamp: ts,
		Metadata:  b,
	}
}

func TestTemporalA_B_C_WithinWindow(t *testing.T) {
	_, mgr := buildTestEngine(t)
	now := time.Now()
	
	e1 := makeEvent("A", "start", "c1", now)
	e2 := makeEvent("B", "mid", "c1", now.Add(1*time.Second))
	e3 := makeEvent("C", "end", "c1", now.Add(2*time.Second))
	eFlush := makeEvent("FLUSH", "", "c1", now.Add(5*time.Second)) // Advance watermark

	mgr.Receive(e1, nil)
	mgr.Receive(e2, nil)
	mgr.Receive(e3, nil)
	res := mgr.Receive(eFlush, nil)
	if len(res) != 1 {
		t.Fatalf("Expected 1 finding, got %d", len(res))
	}
}

func TestTemporalA_B_C_OutsideWindow(t *testing.T) {
	_, mgr := buildTestEngine(t)
	now := time.Now()
	
	e1 := makeEvent("A", "start", "c1", now)
	e2 := makeEvent("B", "mid", "c1", now.Add(1*time.Second))
	e3 := makeEvent("C", "end", "c1", now.Add(15*time.Second)) // outside 10s window
	eFlush := makeEvent("FLUSH", "", "c1", now.Add(20*time.Second)) // Advance watermark

	mgr.Receive(e1, nil)
	mgr.Receive(e2, nil)
	mgr.Receive(e3, nil)
	res := mgr.Receive(eFlush, nil)
	if len(res) != 0 {
		t.Fatal("Expected NO finding, window expired")
	}
}

func TestTemporalA_C_B_OutOfOrder(t *testing.T) {
	_, mgr := buildTestEngine(t)
	now := time.Now()
	
	e1 := makeEvent("A", "start", "c1", now)
	e2 := makeEvent("B", "mid", "c1", now.Add(1*time.Second))
	e3 := makeEvent("C", "end", "c1", now.Add(2*time.Second))
	eFlush := makeEvent("FLUSH", "", "c1", now.Add(5*time.Second)) // Advance watermark

	// Arrival order: e1, e3, e2
	mgr.Receive(e1, nil)
	mgr.Receive(e3, nil) // C arrives. Should be buffered.
	mgr.Receive(e2, nil) // B arrives
	res := mgr.Receive(eFlush, nil)
	if len(res) != 1 {
		t.Fatalf("Expected 1 finding due to out of order buffer, got %d", len(res))
	}
}

func TestDifferentContainerID(t *testing.T) {
	_, mgr := buildTestEngine(t)
	now := time.Now()
	
	e1 := makeEvent("A", "start", "c1", now)
	e2 := makeEvent("B", "mid", "c2", now.Add(1*time.Second)) // different container!
	e3 := makeEvent("C", "end", "c1", now.Add(2*time.Second))
	eFlush := makeEvent("FLUSH", "", "c1", now.Add(5*time.Second)) // Advance watermark

	mgr.Receive(e1, nil)
	mgr.Receive(e2, nil)
	mgr.Receive(e3, nil)
	res := mgr.Receive(eFlush, nil)
	if len(res) != 0 {
		t.Fatal("Expected NO finding, states are isolated by container_id")
	}
}
