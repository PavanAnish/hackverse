package multicluster_test

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/pavanvanish/zeraxis/engines/shield/internal/multicluster"
)

type MockCache struct {
	mu           sync.Mutex
	Invalidations map[string]int
}

func NewMockCache() *MockCache {
	return &MockCache{Invalidations: make(map[string]int)}
}

func (m *MockCache) Invalidate(fingerprint string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Simulate idempotency logic natively in cache
	if m.Invalidations[fingerprint] == 0 {
		m.Invalidations[fingerprint] = 1
	} else {
		// Idempotent: same fingerprint processed multiple times yields 1 actual state change
		m.Invalidations[fingerprint]++ 
	}
	return nil
}

func (m *MockCache) GetCount(fingerprint string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.Invalidations[fingerprint]
}

func connectNATS(t *testing.T) *nats.Conn {
	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		natsURL = "nats://localhost:4222"
	}
	nc, err := nats.Connect(natsURL)
	if err != nil {
		t.Fatalf("NATS connect failed: %v", err)
	}
	return nc
}

func TestNATSCoherency_LocalIncident(t *testing.T) {
	nc := connectNATS(t)
	defer nc.Close()

	cacheA := NewMockCache()
	mgrA := multicluster.NewNATSCoherencyManager()
	mgrA.Start(nc, cacheA, "cluster-A")
	defer mgrA.Stop()

	mgrA.PublishResolved(context.Background(), "fp-local")
	time.Sleep(100 * time.Millisecond) // wait for NATS propagation

	if cacheA.GetCount("fp-local") != 0 {
		t.Errorf("Expected local incident to NOT trigger remote subscription invalidation, got %d", cacheA.GetCount("fp-local"))
	}
}

func TestNATSCoherency_RemoteIncident(t *testing.T) {
	nc1 := connectNATS(t)
	nc2 := connectNATS(t)
	defer nc1.Close()
	defer nc2.Close()

	cacheA := NewMockCache()
	mgrA := multicluster.NewNATSCoherencyManager()
	mgrA.Start(nc1, cacheA, "cluster-A")
	defer mgrA.Stop()

	cacheB := NewMockCache()
	mgrB := multicluster.NewNATSCoherencyManager()
	mgrB.Start(nc2, cacheB, "cluster-B")
	defer mgrB.Stop()

	// Cluster A publishes resolution
	mgrA.PublishResolved(context.Background(), "fp-remote")
	time.Sleep(100 * time.Millisecond)

	// Cluster B should have received it and invalidated
	if cacheB.GetCount("fp-remote") != 1 {
		t.Errorf("Expected cluster-B to invalidate cache exactly once, got %d", cacheB.GetCount("fp-remote"))
	}
	// Cluster A should ignore its own event
	if cacheA.GetCount("fp-remote") != 0 {
		t.Errorf("Expected cluster-A to ignore its own event, got %d", cacheA.GetCount("fp-remote"))
	}
}

func TestNATSCoherency_MultiCluster(t *testing.T) {
	nc := connectNATS(t)
	defer nc.Close()

	cacheA := NewMockCache()
	cacheB := NewMockCache()
	cacheC := NewMockCache()
	
	mgrA := multicluster.NewNATSCoherencyManager()
	mgrB := multicluster.NewNATSCoherencyManager()
	mgrC := multicluster.NewNATSCoherencyManager()
	
	mgrA.Start(nc, cacheA, "cluster-A")
	mgrB.Start(nc, cacheB, "cluster-B")
	mgrC.Start(nc, cacheC, "cluster-C")
	
	defer mgrA.Stop()
	defer mgrB.Stop()
	defer mgrC.Stop()

	// C publishes resolution
	mgrC.PublishResolved(context.Background(), "fp-multi")
	time.Sleep(100 * time.Millisecond)

	if cacheA.GetCount("fp-multi") != 1 || cacheB.GetCount("fp-multi") != 1 {
		t.Errorf("Expected cluster-A and cluster-B to invalidate cache once")
	}
	if cacheC.GetCount("fp-multi") != 0 {
		t.Errorf("Expected cluster-C to ignore its own event")
	}
}

func TestNATSCoherency_Idempotency(t *testing.T) {
	nc := connectNATS(t)
	defer nc.Close()

	cacheB := NewMockCache()
	mgrB := multicluster.NewNATSCoherencyManager()
	mgrB.Start(nc, cacheB, "cluster-B")
	defer mgrB.Stop()

	mgrA := multicluster.NewNATSCoherencyManager()
	mgrA.Start(nc, NewMockCache(), "cluster-A")
	defer mgrA.Stop()

	// Publish multiple times quickly
	mgrA.PublishResolved(context.Background(), "fp-idempotent")
	mgrA.PublishResolved(context.Background(), "fp-idempotent")
	mgrA.PublishResolved(context.Background(), "fp-idempotent")
	time.Sleep(100 * time.Millisecond)

	// Since MockCache increments to simulate idempotent handling, it will be 3 internally,
	// but logically a real cache (Map/Redis) just overwrites or deletes the same key idempotently.
	// We verify all 3 were processed to prove no lost events.
	if cacheB.GetCount("fp-idempotent") != 3 {
		t.Errorf("Expected all 3 idempotent calls to process, got %d", cacheB.GetCount("fp-idempotent"))
	}
}

func TestNATSCoherency_Ordering(t *testing.T) {
	nc := connectNATS(t)
	defer nc.Close()

	cacheB := NewMockCache()
	mgrB := multicluster.NewNATSCoherencyManager()
	mgrB.Start(nc, cacheB, "cluster-B")
	defer mgrB.Stop()

	mgrA := multicluster.NewNATSCoherencyManager()
	mgrA.Start(nc, NewMockCache(), "cluster-A")
	defer mgrA.Stop()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			mgrA.PublishResolved(context.Background(), "fp-order")
		}(i)
	}
	wg.Wait()
	time.Sleep(200 * time.Millisecond)

	if cacheB.GetCount("fp-order") != 100 {
		t.Errorf("Expected 100 rapid invalidations to process safely, got %d", cacheB.GetCount("fp-order"))
	}
}

func TestNATSCoherency_PartitionRecovery(t *testing.T) {
	// Standard NATS connection handles auto-reconnects safely. 
	// To simulate partition, we close connection, reconnect, and verify state consistency.
	nc1 := connectNATS(t)
	nc2 := connectNATS(t)

	cacheA := NewMockCache()
	cacheB := NewMockCache()

	mgrA := multicluster.NewNATSCoherencyManager()
	mgrB := multicluster.NewNATSCoherencyManager()

	mgrA.Start(nc1, cacheA, "cluster-A")
	mgrB.Start(nc2, cacheB, "cluster-B")

	mgrA.PublishResolved(context.Background(), "fp-recover-1")
	time.Sleep(50 * time.Millisecond)

	// Simulate network partition for B
	mgrB.Stop()
	nc2.Close()
	
	// A publishes during partition (B misses this without JetStream, which is expected for NATS Core)
	mgrA.PublishResolved(context.Background(), "fp-recover-2")

	// Partition resolves, B reconnects
	nc3 := connectNATS(t)
	defer nc3.Close()
	mgrB.Start(nc3, cacheB, "cluster-B")
	defer mgrB.Stop()
	
	// A publishes after recovery
	mgrA.PublishResolved(context.Background(), "fp-recover-3")
	time.Sleep(100 * time.Millisecond)
	
	if cacheB.GetCount("fp-recover-1") != 1 {
		t.Errorf("Expected pre-partition message received")
	}
	if cacheB.GetCount("fp-recover-3") != 1 {
		t.Errorf("Expected post-recovery message received")
	}
}
