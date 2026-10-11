package collector

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestTailer_PartialWrites(t *testing.T) {
	// Create a temp file
	f, err := os.CreateTemp("", "falco_test_*.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	// Write some dummy initial content so tailer seeks past it.
	// Actually, wait, tailer seeks to END on start.
	// We'll write to it after starting.
	f.WriteString(`{"time":"2023-01-01T00:00:00Z","rule":"Dummy"}` + "\n")
	
	tailer := NewTailer(f.Name(), 10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go tailer.Run(ctx)
	
	time.Sleep(100 * time.Millisecond) // Let tailer seek to end

	// Write full event
	f.WriteString(`{"time":"2023-01-01T00:00:01Z","rule":"Full Event"}` + "\n")
	
	select {
	case evt := <-tailer.Events():
		if evt.Rule != "Full Event" {
			t.Errorf("expected 'Full Event', got %s", evt.Rule)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for full event")
	}

	// Write partial event
	f.WriteString(`{"time":"2023-01-01T00:00:02Z","rule":`)
	time.Sleep(300 * time.Millisecond) // Let tailer read the partial and hit EOF
	f.WriteString(`"Partial Event"}` + "\n")

	select {
	case evt := <-tailer.Events():
		if evt.Rule != "Partial Event" {
			t.Errorf("expected 'Partial Event', got %s", evt.Rule)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for partial event")
	}

	// Write multiple partial reads
	f.WriteString(`{"time":"2023-01-01T00:00:03Z",`)
	time.Sleep(250 * time.Millisecond)
	f.WriteString(`"rule":`)
	time.Sleep(250 * time.Millisecond)
	f.WriteString(`"Multi Partial"}` + "\n")

	select {
	case evt := <-tailer.Events():
		if evt.Rule != "Multi Partial" {
			t.Errorf("expected 'Multi Partial', got %s", evt.Rule)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for multi partial event")
	}
	
	// Malformed JSON (should be logged/skipped, not panic)
	f.WriteString(`{"time":"2023-01-01T00:00:04Z", rule: malformed}` + "\n")
	
	// Empty line (skipped)
	f.WriteString("\n")
	
	// Another good event
	f.WriteString(`{"time":"2023-01-01T00:00:05Z","rule":"Final Event"}` + "\n")

	select {
	case evt := <-tailer.Events():
		if evt.Rule != "Final Event" {
			t.Errorf("expected 'Final Event', got %s", evt.Rule)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for final event")
	}
}
