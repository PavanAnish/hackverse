package cache_test

import (
	"context"
	"encoding/json"
	"sort"
	"testing"
	"time"

	"github.com/pavanvanish/zeraxis/engines/shield/internal/cache"
	"github.com/pavanvanish/zeraxis/pkg/events"
)

func TestInvalidationSLA_ResolutionPropagation(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	nc, cleanup := connectNATS(t)
	defer cleanup()

	c := cache.NewMaterializedView(db)
	ctx := context.Background()

	// SETUP: Block fingerprint "IMAGE_X" in cache
	const fp = "IMAGE_X"
	err := c.Set(ctx, fp, "ACTIVE_INCIDENT_BLOCK", 5*time.Minute)
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	// STEP 1: Verify fingerprint is blocked before resolution
	_, isAllowed, _, err := c.Get(ctx, fp)
	if err != nil {
		t.Fatalf("Pre-resolution Get failed: %v", err)
	}
	if isAllowed {
		t.Fatalf("STEP 1 FAILED: fingerprint should be blocked before resolution")
	}
	t.Logf("✓ STEP 1: fingerprint=IMAGE_X is BLOCKED (allowed=false)")

	// STEP 2: Start NATS listener
	listener := cache.NewNATSInvalidationListener(nc, c)
	if err := listener.Start(ctx); err != nil {
		t.Fatalf("Failed to start NATS listener: %v", err)
	}
	defer listener.Stop()
	time.Sleep(100 * time.Millisecond) // Subscription warmup

	// STEP 3: Publish INCIDENT_RESOLVED event
	metaBytes, _ := json.Marshal(map[string]interface{}{
		"fingerprint": fp,
	})
	envelope := events.Event{
		EventType: "INCIDENT_RESOLVED",
		Metadata:  metaBytes,
	}
	b, _ := json.Marshal(envelope)

	resolutionTime := time.Now()
	nc.Publish("zeraxis.events.correlation.incident.resolved", b)
	nc.Flush()

	// STEP 4: Poll until cache reports ALLOWED (or 500ms timeout)
	var invalidationLatency time.Duration
	invalidated := false
	for i := 0; i < 100; i++ {
		_, allowed, _, _ := c.Get(ctx, fp)
		if allowed {
			invalidationLatency = time.Since(resolutionTime)
			invalidated = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	// ASSERTION 1: Invalidation < 500ms SLA
	if !invalidated {
		t.Fatalf("ASSERTION 1 FAILED: cache invalidation did not occur within 500ms SLA")
	}
	if invalidationLatency > 500*time.Millisecond {
		t.Errorf("ASSERTION 1 FAILED: invalidation latency %v exceeds 500ms SLA", invalidationLatency)
	} else {
		t.Logf("✓ ASSERTION 1: Invalidation latency = %v (SLA < 500ms)", invalidationLatency)
	}

	// ASSERTION 2: In-memory entry cleared — subsequent Get returns allowed=true
	reason, allowed, _, _ := c.Get(ctx, fp)
	if !allowed || reason != "" {
		t.Errorf("ASSERTION 2 FAILED: After invalidation, expected allowed=true reason='', got allowed=%v reason=%s", allowed, reason)
	} else {
		t.Logf("✓ ASSERTION 2: In-memory cache entry for IMAGE_X deleted, fallback returns allowed=true")
	}

	// ASSERTION 3: DB row cleared
	var dbCount int
	db.QueryRow("SELECT COUNT(*) FROM shield_cache_state WHERE fingerprint = $1 AND block_reason IS NOT NULL", fp).Scan(&dbCount)
	if dbCount > 0 {
		t.Errorf("ASSERTION 3 FAILED: DB still has blocked entry for fingerprint=%s", fp)
	} else {
		t.Logf("✓ ASSERTION 3: DB cache row for IMAGE_X cleared")
	}
}

func TestInvalidationSLA_HistogramMultipleResolutions(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	nc, cleanup := connectNATS(t)
	defer cleanup()

	c := cache.NewMaterializedView(db)
	ctx := context.Background()

	listener := cache.NewNATSInvalidationListener(nc, c)
	if err := listener.Start(ctx); err != nil {
		t.Fatalf("Failed to start listener: %v", err)
	}
	defer listener.Stop()
	time.Sleep(100 * time.Millisecond)

	const n = 10
	latencies := make([]time.Duration, 0, n)

	for i := 0; i < n; i++ {
		fp := "IMAGE_HISTOGRAM_" + string(rune('A'+i))
		c.Set(ctx, fp, "INCIDENT_BLOCK", 5*time.Minute)

		metaBytes, _ := json.Marshal(map[string]interface{}{"fingerprint": fp})
		envelope := events.Event{EventType: "INCIDENT_RESOLVED", Metadata: metaBytes}
		b, _ := json.Marshal(envelope)

		start := time.Now()
		nc.Publish("zeraxis.events.correlation.incident.resolved", b)
		nc.Flush()

		for j := 0; j < 100; j++ {
			_, allowed, _, _ := c.Get(ctx, fp)
			if allowed {
				latencies = append(latencies, time.Since(start))
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
	}

	if len(latencies) < n {
		t.Fatalf("Not all invalidations completed: %d/%d", len(latencies), n)
	}

	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })

	var total time.Duration
	for _, l := range latencies {
		total += l
	}
	mean := total / time.Duration(n)
	p50 := latencies[n/2]
	p99 := latencies[n-1]

	t.Logf("✓ Invalidation Latency Histogram (n=%d):", n)
	t.Logf("  mean=%v  P50=%v  P99=%v", mean, p50, p99)
	for i, l := range latencies {
		t.Logf("  sample[%d]=%v", i, l)
	}

	if p99 > 500*time.Millisecond {
		t.Errorf("P99 invalidation latency %v exceeds 500ms SLA", p99)
	}
}
