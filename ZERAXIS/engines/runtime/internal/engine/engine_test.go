package engine

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/pavanvanish/zeraxis/engines/runtime/internal/collector"
	"github.com/pavanvanish/zeraxis/engines/runtime/internal/normalizer"
	"github.com/pavanvanish/zeraxis/engines/runtime/internal/resolver"
	"github.com/pavanvanish/zeraxis/pkg/events"
)

import "sync"
type mockPublisher struct {
	mu sync.Mutex
	events []*events.Event
}

func (m *mockPublisher) Publish(ctx context.Context, e *events.Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, e)
	return nil
}

func (m *mockPublisher) Close() {}

func TestEngine_NonBlockingResolution(t *testing.T) {
	f, _ := os.CreateTemp("", "falco_test_*.jsonl")
	defer os.Remove(f.Name())

	tailer := collector.NewTailer(f.Name(), 2000)
	
	client := resolver.NewDiscoveryClient("http://localhost:9999", "http://localhost:9998")
	res := resolver.NewResolver(client, 1*time.Minute)
	norm := normalizer.NewNormalizer(res)
	pub := &mockPublisher{}
	
	eng := NewEngine(tailer, norm, nil, pub, res)
	
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	go tailer.Run(ctx)
	go eng.Run(ctx)
	
	time.Sleep(100 * time.Millisecond) // Let tailer seek to end

	for i := 0; i < 1015; i++ {
		f.WriteString(`{"time":"2023-01-01T00:00:00Z","rule":"ZERAXIS_PROCESS_EXEC","output_fields":{"container.id":"unresolved_cnt"}}` + "\n")
	}
	
	time.Sleep(1 * time.Second)
	
	stats := eng.Stats()
	if stats["events_bypassed"] == 0 {
		t.Errorf("Expected some bypassed events due to full queue, got 0")
	}
}
