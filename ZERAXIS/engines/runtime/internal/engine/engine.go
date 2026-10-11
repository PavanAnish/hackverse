package engine

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pavanvanish/zeraxis/engines/runtime/internal/collector"
	"github.com/pavanvanish/zeraxis/engines/runtime/internal/normalizer"
	"github.com/pavanvanish/zeraxis/engines/runtime/internal/repository"
	"github.com/pavanvanish/zeraxis/engines/runtime/internal/resolver"
	"github.com/pavanvanish/zeraxis/pkg/events"
)

// Engine is the main runtime monitoring processing loop.
type Engine struct {
	tailer          *collector.Tailer
	normalizer      *normalizer.Normalizer
	repo            *repository.Postgres
	publisher       events.Publisher
	resolver        *resolver.Resolver
	resolutionQueue chan collector.RawFalcoEvent
	wg              sync.WaitGroup

	normalized atomic.Int64
	persisted  atomic.Int64
	published  atomic.Int64
	bypassed   atomic.Int64
}

// NewEngine creates a new runtime engine.
func NewEngine(tailer *collector.Tailer, norm *normalizer.Normalizer, repo *repository.Postgres, pub events.Publisher, res *resolver.Resolver) *Engine {
	return &Engine{
		tailer:          tailer,
		normalizer:      norm,
		repo:            repo,
		publisher:       pub,
		resolver:        res,
		resolutionQueue: make(chan collector.RawFalcoEvent, 100000), // bounded queue
	}
}

// Stats returns current engine statistics.
func (e *Engine) Stats() map[string]int64 {
	return map[string]int64{
		"events_received":   e.tailer.Received(),
		"events_normalized": e.normalized.Load(),
		"events_persisted":  e.persisted.Load(),
		"events_published":  e.published.Load(),
		"events_dropped":    e.tailer.Dropped(),
		"events_unresolved": e.resolver.Unresolved(),
		"events_bypassed":   e.bypassed.Load(),
	}
}

// Run starts the engine processing loop. Blocks until context is cancelled.
func (e *Engine) Run(ctx context.Context) {
	slog.Info("Runtime engine started")

	// Periodically evict expired cache entries
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				e.resolver.EvictExpired()
			}
		}
	}()

	// Start bounded worker pool
	const numWorkers = 100
	e.wg.Add(numWorkers)
	for i := 0; i < numWorkers; i++ {
		go e.worker(ctx)
	}

	for raw := range e.tailer.Events() {
		if ctx.Err() != nil {
			break
		}
		
		select {
		case e.resolutionQueue <- raw:
			// successfully queued for resolution
		default:
			// A. identity resolves immediately (in worker)
			// B. identity resolves after retry (in worker)
			// C. identity never resolves (in worker -> UNRESOLVED)
			// D. resolution queue is full -> bypass queue and process as UNRESOLVED immediately!
			e.bypassed.Add(1)
			e.processEventBypassed(ctx, &raw)
		}
	}

	// Wait for workers to finish pending events (graceful shutdown)
	// E. runtime engine shuts down while resolutions are pending -> wait for them
	close(e.resolutionQueue)
	e.wg.Wait()

	slog.Info("Runtime engine stopped", "stats", e.Stats())
}

func (e *Engine) worker(ctx context.Context) {
	defer e.wg.Done()
	for raw := range e.resolutionQueue {
		if ctx.Err() != nil {
			return
		}
		e.processEvent(ctx, &raw)
	}
}

func (e *Engine) processEvent(ctx context.Context, raw *collector.RawFalcoEvent) {
	evt := e.normalizer.Normalize(ctx, raw)
	if evt != nil {
		e.publishEvent(ctx, evt)
	}
}

// processEventBypassed processes an event immediately without waiting for resolution retries.
func (e *Engine) processEventBypassed(ctx context.Context, raw *collector.RawFalcoEvent) {
	evt := e.normalizer.NormalizeBypassed(ctx, raw)
	if evt != nil {
		e.publishEvent(ctx, evt)
	}
}

func (e *Engine) publishEvent(ctx context.Context, evt *events.Event) {
	e.normalized.Add(1)

	// Validate
	if err := evt.Validate(); err != nil {
		slog.Error("Invalid runtime event", "error", err, "event_type", evt.EventType)
		return
	}

	// Persist to PostgreSQL first (durable truth)
	eventBytes, err := json.Marshal(evt)
	if err != nil {
		slog.Error("Failed to marshal event", "error", err)
		return
	}

	if e.repo != nil {
		if err := e.repo.InsertUnifiedEvent(ctx, eventBytes); err != nil {
			slog.Error("Failed to persist runtime event to PostgreSQL",
				"error", err,
				"event_id", evt.EventID,
				"event_type", evt.EventType,
			)
			return
		}
	}
	e.persisted.Add(1)

	// Publish to NATS (best-effort transport)
	if err := e.publisher.Publish(ctx, evt); err != nil {
		slog.Error("Failed to publish runtime event to NATS",
			"error", err,
			"event_id", evt.EventID,
			"event_type", evt.EventType,
		)
	} else {
		e.published.Add(1)
	}

	slog.Info("Runtime event processed",
		"event_id", evt.EventID,
		"event_type", evt.EventType,
		"asset_id", evt.AssetID,
		"image_digest", evt.ImageDigest,
		"subject", evt.Subject,
	)
}
