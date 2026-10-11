package tests

import (
	"encoding/json"
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pavanvanish/zeraxis/engines/detection/evaluator"
	"github.com/pavanvanish/zeraxis/engines/detection/models"
	"github.com/pavanvanish/zeraxis/engines/detection/state"
	"github.com/pavanvanish/zeraxis/pkg/events"
)

func buildBenchEngine(b *testing.B) (*evaluator.Engine, *state.Manager) {
	rules := []models.Rule{
		{
			RuleID: "rule-bench", RuleVersion: 1, Enabled: true,
			TimeWindow: 10 * time.Second,
			Sequence: []models.SequenceStep{
				{Name: "s1", EventType: "PROCESS_EXECUTION", Condition: `true`},
				{Name: "s2", EventType: "FILE_ACTIVITY", Condition: `true`},
				{Name: "s3", EventType: "NETWORK_CONNECTION", Condition: `true`},
			},
		},
	}
	eng, err := evaluator.NewEngine(rules)
	if err != nil {
		b.Fatalf("Failed to build engine: %v", err)
	}
	return eng, state.NewManager(eng)
}

func makeBenchEvent(typ, cid string, ts time.Time) events.Event {
	meta := map[string]interface{}{"container_id": cid}
	b, _ := json.Marshal(meta)
	return events.Event{
		EventID:   uuid.NewString(),
		EventType: typ,
		Timestamp: ts,
		Metadata:  b,
	}
}

func BenchmarkTemporalEngine_MemorySafety(b *testing.B) {
	_, mgr := buildBenchEngine(b)

	numContainers := 5000 // test up to max bounds
	b.ResetTimer()

	now := time.Now()
	for i := 0; i < b.N; i++ {
		cid := fmt.Sprintf("container-%d", i%numContainers)
		
		// Introduce 90% noise (Step 1 only) to force sequence tracking and eviction
		if i%10 != 0 {
			evt := makeBenchEvent("PROCESS_EXECUTION", cid, now.Add(time.Duration(i)*time.Millisecond))
			mgr.Receive(evt, nil)
		} else {
			// Complete a sequence
			e1 := makeBenchEvent("PROCESS_EXECUTION", cid, now.Add(time.Duration(i)*time.Millisecond))
			e2 := makeBenchEvent("FILE_ACTIVITY", cid, now.Add(time.Duration(i)*time.Millisecond+1))
			e3 := makeBenchEvent("NETWORK_CONNECTION", cid, now.Add(time.Duration(i)*time.Millisecond+2))
			eFlush := makeBenchEvent("FLUSH", cid, now.Add(time.Duration(i)*time.Millisecond+3000))
			mgr.Receive(e1, nil)
			mgr.Receive(e2, nil)
			mgr.Receive(e3, nil)
			mgr.Receive(eFlush, nil)
		}
	}

	b.StopTimer()

	// Print memory stats
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	b.Logf("Alloc = %v MB", m.Alloc/1024/1024)
	b.Logf("Sys = %v MB", m.Sys/1024/1024)
	b.Logf("Metrics: %+v", mgr.Metrics)
}
