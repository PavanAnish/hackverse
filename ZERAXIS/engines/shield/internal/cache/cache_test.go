package cache_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/nats-io/nats.go"
	"github.com/pavanvanish/zeraxis/engines/shield/internal/cache"
	"github.com/pavanvanish/zeraxis/pkg/events"
)

func setupTestDB(t *testing.T) *sql.DB {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:5433/zeraxis?sslmode=disable"
	}
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		t.Skipf("DB not available: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Skipf("DB not reachable: %v", err)
	}

	db.Exec("DROP SCHEMA IF EXISTS public CASCADE")
	db.Exec("CREATE SCHEMA public")

	schema := `
	CREATE TABLE shield_cache_state (
		fingerprint TEXT PRIMARY KEY,
		block_reason TEXT,
		cached_at TIMESTAMPTZ DEFAULT NOW(),
		ttl_expires_at TIMESTAMPTZ,
		incident_id UUID,
		last_event_id TEXT
	);
	`
	_, err = db.Exec(schema)
	if err != nil {
		t.Fatalf("Failed to create schema: %v", err)
	}
	return db
}

func connectNATS(t *testing.T) (*nats.Conn, func()) {
	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		natsURL = "nats://localhost:4222"
	}
	nc, err := nats.Connect(natsURL)
	if err != nil {
		t.Skipf("NATS not available at %s", natsURL)
		return nil, func() {}
	}
	return nc, func() { nc.Close() }
}

func TestCacheHit(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	c := cache.NewMaterializedView(db)
	ctx := context.Background()

	err := c.Set(ctx, "fp1", "MALICIOUS_FILE", 5*time.Minute)
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	start := time.Now()
	reason, allowed, age, err := c.Get(ctx, "fp1")
	dur := time.Since(start)

	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if allowed || reason != "MALICIOUS_FILE" {
		t.Errorf("Expected blocked with MALICIOUS_FILE, got allowed=%v reason=%s age=%v", allowed, reason, age)
	}
	t.Logf("Cache hit latency: %v (P99 requirement < 50ms)", dur)
}

func TestCacheMiss(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	c := cache.NewMaterializedView(db)
	ctx := context.Background()

	reason, allowed, _, err := c.Get(ctx, "unknown-fp")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !allowed || reason != "" {
		t.Errorf("Expected allowed for unknown fingerprint")
	}
}

func TestCacheExpiry(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	c := cache.NewMaterializedView(db)
	ctx := context.Background()

	err := c.Set(ctx, "fp-expire", "EXPIRE_TEST", 100*time.Millisecond)
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	time.Sleep(150 * time.Millisecond)
	db.Exec("DELETE FROM shield_cache_state WHERE fingerprint = 'fp-expire'")

	reason, allowed, _, err := c.Get(ctx, "fp-expire")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !allowed || reason != "" {
		t.Errorf("Expected allowed after expiry and DB removal, got blocked=%s", reason)
	}
}

func TestConcurrentRead(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	c := cache.NewMaterializedView(db)
	ctx := context.Background()

	c.Set(ctx, "fp-concurrent", "CONCURRENT_BLOCK", 5*time.Minute)

	var wg sync.WaitGroup
	workers := 1000
	wg.Add(workers)

	start := time.Now()
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			reason, allowed, _, err := c.Get(ctx, "fp-concurrent")
			if err != nil || allowed || reason != "CONCURRENT_BLOCK" {
				t.Errorf("Concurrent get failed or returned wrong value")
			}
		}()
	}
	wg.Wait()
	t.Logf("1000 concurrent reads completed in %v", time.Since(start))
}

func TestNATSInvalidation(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	nc, cleanup := connectNATS(t)
	defer cleanup()

	c := cache.NewMaterializedView(db)
	ctx := context.Background()

	c.Set(ctx, "fp-nats-test", "TO_BE_RESOLVED", 5*time.Minute)

	listener := cache.NewNATSInvalidationListener(nc, c)
	err := listener.Start(ctx)
	if err != nil {
		t.Fatalf("Start listener failed: %v", err)
	}
	defer listener.Stop()

	time.Sleep(100 * time.Millisecond)

	metaBytes, _ := json.Marshal(map[string]interface{}{
		"fingerprint": "fp-nats-test",
	})
	envelope := events.Event{
		EventType: "INCIDENT_RESOLVED",
		Metadata:  metaBytes,
	}
	b, _ := json.Marshal(envelope)

	start := time.Now()
	nc.Publish("zeraxis.events.correlation.incident.resolved", b)
	nc.Flush()

	success := false
	var dur time.Duration
	for i := 0; i < 50; i++ {
		reason, allowed, _, _ := c.Get(ctx, "fp-nats-test")
		if allowed && reason == "" {
			success = true
			dur = time.Since(start)
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if !success {
		t.Fatalf("Cache invalidation did not occur within SLA (500ms)")
	}
	t.Logf("Cache invalidation propagation latency: %v (P99 requirement < 500ms)", dur)
}

func TestFallbackQuery(t *testing.T) {
	db, _ := sql.Open("postgres", "postgres://postgres:invalid@localhost:5433/zeraxis?sslmode=disable")
	c := cache.NewMaterializedView(db)
	ctx := context.Background()

	reason, allowed, _, err := c.Get(ctx, "fp-fallback-closed")
	if err == nil {
		t.Fatalf("Expected error for unreachable DB")
	}
	if allowed || reason != "DB_UNREACHABLE" {
		t.Errorf("Expected fail-closed (allowed=false, reason=DB_UNREACHABLE), got allowed=%v, reason=%s", allowed, reason)
	}
}
