package discovery

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/pavanvanish/zeraxis/engines/discovery/internal/models"
	"github.com/pavanvanish/zeraxis/pkg/events"
)

// Repository defines the persistence interface for the discovery loop.
type Repository interface {
	GetAllAssets(ctx context.Context) ([]models.Asset, error)
	GetRelationships(ctx context.Context) ([]models.Relationship, error)
	UpsertAsset(ctx context.Context, asset models.Asset) error
	UpsertPorts(ctx context.Context, assetID string, ports []models.AssetPort) error
	UpsertNetworks(ctx context.Context, assetID string, networks []models.AssetNetwork) error
	UpsertRelationship(ctx context.Context, rel models.Relationship) error
	MarkAssetRemoved(ctx context.Context, assetID string, timestamp time.Time) error
	RemoveRelationship(ctx context.Context, relID string) error
	RecordEvent(ctx context.Context, event models.DiscoveryEvent) error
	InsertUnifiedEvent(ctx context.Context, eventBytes []byte) error
}

// Loop runs periodic discovery scans and reconciles the results.
type Loop struct {
	scanner    *Scanner
	reconciler *Reconciler
	repo       Repository
	publisher  events.Publisher
	interval   time.Duration
}

// NewLoop creates a new discovery loop.
func NewLoop(scanner *Scanner, reconciler *Reconciler, repo Repository, pub events.Publisher, interval time.Duration) *Loop {
	return &Loop{
		scanner:    scanner,
		reconciler: reconciler,
		repo:       repo,
		publisher:  pub,
		interval:   interval,
	}
}

// Run starts the discovery loop. It blocks until the context is cancelled.
func (l *Loop) Run(ctx context.Context) {
	// Run an initial scan immediately
	l.runScan(ctx)

	ticker := time.NewTicker(l.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("Discovery loop shutting down")
			return
		case <-ticker.C:
			l.runScan(ctx)
		}
	}
}

// RunOnce performs a single discovery scan. Useful for testing.
func (l *Loop) RunOnce(ctx context.Context) {
	l.runScan(ctx)
}

func (l *Loop) runScan(ctx context.Context) {
	start := time.Now()

	// Scan Docker environment
	result, err := l.scanner.Scan(ctx)
	if err != nil {
		slog.Error("Discovery scan failed",
			"error", err,
			"source", "discovery-engine",
		)
		return
	}

	// Load stored state for reconciliation
	storedAssets, err := l.repo.GetAllAssets(ctx)
	if err != nil {
		slog.Error("Failed to load stored assets",
			"error", err,
			"scan_id", result.ScanID,
		)
		return
	}

	storedRels, err := l.repo.GetRelationships(ctx)
	if err != nil {
		slog.Error("Failed to load stored relationships",
			"error", err,
			"scan_id", result.ScanID,
		)
		return
	}

	// Reconcile
	reconciled := l.reconciler.Reconcile(
		result.ScanID,
		result.Timestamp,
		result.Assets,
		storedAssets,
		result.Relationships,
		storedRels,
	)

	// Persist scanned assets
	for _, asset := range result.Assets {
		if err := l.repo.UpsertAsset(ctx, asset); err != nil {
			slog.Error("Failed to upsert asset",
				"asset_id", asset.AssetID,
				"asset_name", asset.Name,
				"error", err,
			)
			continue
		}
		if err := l.repo.UpsertPorts(ctx, asset.AssetID, asset.Ports); err != nil {
			slog.Error("Failed to upsert ports",
				"asset_id", asset.AssetID,
				"error", err,
			)
		}
		if err := l.repo.UpsertNetworks(ctx, asset.AssetID, asset.Networks); err != nil {
			slog.Error("Failed to upsert networks",
				"asset_id", asset.AssetID,
				"error", err,
			)
		}
	}

	// Persist relationships
	scannedRelMap := make(map[string]bool)
	for _, rel := range result.Relationships {
		scannedRelMap[rel.ID] = true
		if err := l.repo.UpsertRelationship(ctx, rel); err != nil {
			slog.Error("Failed to upsert relationship",
				"relationship_id", rel.ID,
				"error", err,
			)
		}
	}

	// Handle removed assets
	scannedNames := make(map[string]bool)
	for _, a := range result.Assets {
		scannedNames[a.Name] = true
	}
	for _, prev := range storedAssets {
		if prev.Status == "removed" {
			continue
		}
		if !scannedNames[prev.Name] {
			if err := l.repo.MarkAssetRemoved(ctx, prev.AssetID, result.Timestamp); err != nil {
				slog.Error("Failed to mark asset removed",
					"asset_id", prev.AssetID,
					"error", err,
				)
			}
		}
	}

	// Handle removed relationships
	for _, prev := range storedRels {
		if !scannedRelMap[prev.ID] {
			if err := l.repo.RemoveRelationship(ctx, prev.ID); err != nil {
				slog.Error("Failed to remove relationship",
					"relationship_id", prev.ID,
					"error", err,
				)
			}
		}
	}

	// Record events
	for _, event := range reconciled.Events {
		// Old event record for Phase 2 compatibility
		if err := l.repo.RecordEvent(ctx, event); err != nil {
			slog.Error("Failed to record legacy event",
				"event_id", event.ID,
				"error", err,
			)
		}

		// Create unified event
		ue := events.NewEvent(event.EventType, events.SourceDiscovery, event.Timestamp)
		ue.EventID = event.ID // Use the SAME ID for deduplication
		ue.AssetID = event.AssetID
		ue.Subject = events.GenerateSubject(events.SourceDiscovery, event.EventType)
		
		// Move details to metadata
		metadata := map[string]interface{}{
			"asset_name": event.AssetName,
			"scan_id":    event.ScanID,
		}
		
		// If details is a json object, merge it
		var details map[string]interface{}
		if err := json.Unmarshal([]byte(event.Details), &details); err == nil {
			for k, v := range details {
				metadata[k] = v
			}
		} else {
			metadata["details"] = event.Details
		}
		
		metaBytes, _ := json.Marshal(metadata)
		ue.Metadata = metaBytes

		if err := ue.Validate(); err != nil {
			slog.Error("Invalid unified event", "error", err, "event_id", ue.EventID)
			continue
		}

		ueBytes, _ := json.Marshal(ue)
		
		// 1. PostgreSQL
		if err := l.repo.InsertUnifiedEvent(ctx, ueBytes); err != nil {
			slog.Error("Failed to persist unified event to PostgreSQL", "error", err, "event_id", ue.EventID)
			continue // If Postgres fails, do NOT publish to NATS. It lacks durability.
		}

		// 2. NATS Publish
		if err := l.publisher.Publish(ctx, ue); err != nil {
			slog.Error("Failed to publish event to NATS", "error", err, "event_id", ue.EventID)
			// We DO NOT rollback Postgres. Postgres is durable. NATS failure is logged.
		}
	}

	slog.Info("Discovery cycle complete",
		"scan_id", result.ScanID,
		"assets_discovered", len(result.Assets),
		"events_generated", len(reconciled.Events),
		"duration_ms", time.Since(start).Milliseconds(),
		"source", "discovery-engine",
	)
}
