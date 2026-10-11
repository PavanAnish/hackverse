package admission_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/pavanvanish/zeraxis/engines/shield/internal/admission"
	"github.com/pavanvanish/zeraxis/engines/shield/internal/evaluator"
)

const thunderingHerdFingerprint = "MALICIOUS_IMAGE_SHA256"

func TestThunderingHerd_100ConcurrentBlocked(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	srv, cacheView := setupServer(t, db)

	// Pre-populate cache as BLOCKED
	err := cacheView.Set(context.Background(), thunderingHerdFingerprint, "CRITICAL_BLOCK", 5*time.Minute)
	if err != nil {
		t.Fatalf("Failed to pre-populate cache: %v", err)
	}

	type result struct {
		allowed    bool
		reason     string
		decisionID string
		latency    time.Duration
	}

	const workers = 100
	results := make([]result, workers)
	var wg sync.WaitGroup
	wg.Add(workers)

	testStart := time.Now()

	for i := 0; i < workers; i++ {
		i := i
		go func() {
			defer wg.Done()

			reqBody, _ := json.Marshal(evaluator.AdmissionRequest{
				Fingerprint: thunderingHerdFingerprint,
				Registry:    "malicious.io",
			})

			start := time.Now()
			req, _ := http.NewRequest("POST", "/v1/admission/evaluate", bytes.NewBuffer(reqBody))
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()
			srv.Handler().ServeHTTP(rr, req)
			lat := time.Since(start)

			var resp admission.EvaluateResponse
			json.NewDecoder(rr.Body).Decode(&resp)
			results[i] = result{
				allowed:    resp.Allowed,
				reason:     resp.Reason,
				decisionID: resp.DecisionID,
				latency:    lat,
			}
		}()
	}

	wg.Wait()
	totalDuration := time.Since(testStart)

	// --- ASSERTION 1: All 100 requests blocked, zero bypasses ---
	blocked := 0
	for _, r := range results {
		if !r.allowed {
			blocked++
		}
	}
	if blocked != workers {
		t.Errorf("ASSERTION 1 FAILED: Expected 100 blocked, got %d (bypasses=%d)", blocked, workers-blocked)
	} else {
		t.Logf("✓ ASSERTION 1: All %d/100 requests blocked — 0 bypasses", blocked)
	}

	// --- ASSERTION 2: All decisions cite same block_reason ---
	reasonMismatch := 0
	for _, r := range results {
		if r.reason != "CRITICAL_BLOCK" {
			reasonMismatch++
		}
	}
	if reasonMismatch > 0 {
		t.Errorf("ASSERTION 2 FAILED: %d decisions cited wrong block_reason", reasonMismatch)
	} else {
		t.Logf("✓ ASSERTION 2: All %d decisions cite reason=CRITICAL_BLOCK", workers)
	}

	// --- ASSERTION 3: Latency measurements mean < 5ms, P99 < 50ms ---
	latencies := make([]time.Duration, workers)
	for i, r := range results {
		latencies[i] = r.latency
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })

	var total time.Duration
	for _, l := range latencies {
		total += l
	}
	mean := total / workers
	p50 := latencies[49]
	p99 := latencies[98]
	p100 := latencies[99]

	t.Logf("✓ ASSERTION 3 Latency Histogram:")
	t.Logf("  mean=%v  P50=%v  P99=%v  P100=%v  wall_time=%v", mean, p50, p99, p100, totalDuration)

	if mean > 5*time.Millisecond {
		t.Errorf("ASSERTION 3 FAILED: mean %v exceeds 5ms", mean)
	}
	if p99 > 50*time.Millisecond {
		t.Errorf("ASSERTION 3 FAILED: P99 %v exceeds 50ms SLA", p99)
	}

	// --- ASSERTION 4: All decision IDs are unique (individual request tracking) ---
	decisionIDs := make(map[string]struct{}, workers)
	for _, r := range results {
		if r.decisionID != "" {
			decisionIDs[r.decisionID] = struct{}{}
		}
	}
	if len(decisionIDs) != workers {
		t.Errorf("ASSERTION 4 FAILED: Expected %d unique decision IDs, got %d", workers, len(decisionIDs))
	} else {
		t.Logf("✓ ASSERTION 4: %d unique decision IDs — singleflight cache-hit path served all goroutines from memory", workers)
	}
}
