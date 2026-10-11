package discovery

import (
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/pavanvanish/zeraxis/engines/discovery/internal/models"
)

// Reconciler compares current scan results with stored state to produce
// change events. It detects new, updated, and removed assets and relationships.
type Reconciler struct{}

// NewReconciler creates a new Reconciler.
func NewReconciler() *Reconciler {
	return &Reconciler{}
}

// ReconcileResult contains the events produced by reconciliation.
type ReconcileResult struct {
	Events       []models.DiscoveryEvent
	SeenAssetIDs []string
}

// Reconcile compares scanned assets against previously stored assets and
// produces discovery events for any changes.
func (r *Reconciler) Reconcile(
	scanID string,
	scanTime time.Time,
	scanned []models.Asset,
	stored []models.Asset,
	scannedRels []models.Relationship,
	storedRels []models.Relationship,
) *ReconcileResult {
	result := &ReconcileResult{}

	// Index stored assets by name for lookup
	storedByName := make(map[string]models.Asset)
	for _, a := range stored {
		storedByName[a.Name] = a
	}

	// Index scanned assets by name
	scannedByName := make(map[string]models.Asset)
	for _, a := range scanned {
		scannedByName[a.Name] = a
	}

	// Check for new and updated assets
	for _, current := range scanned {
		result.SeenAssetIDs = append(result.SeenAssetIDs, current.AssetID)

		prev, existed := storedByName[current.Name]
		if !existed {
			// New asset discovered
			event := models.DiscoveryEvent{
				ID:        uuid.New().String(),
				EventType: models.EventAssetDiscovered,
				AssetID:   current.AssetID,
				AssetName: current.Name,
				Details:   toJSON(map[string]interface{}{
					"asset_type":    current.AssetType,
					"image":        current.Image,
					"status":       current.Status,
					"container_id": current.ContainerID,
				}),
				ScanID:    scanID,
				Timestamp: scanTime,
			}
			result.Events = append(result.Events, event)
			slog.Info("Asset discovered",
				"event_type", models.EventAssetDiscovered,
				"asset_id", current.AssetID,
				"asset_name", current.Name,
				"asset_type", current.AssetType,
				"image", current.Image,
				"source", "discovery-engine",
				"scan_id", scanID,
			)

			// Log port discovery events
			for _, p := range current.Ports {
				portEvent := models.DiscoveryEvent{
					ID:        uuid.New().String(),
					EventType: models.EventPortDiscovered,
					AssetID:   current.AssetID,
					AssetName: current.Name,
					Details:   toJSON(map[string]interface{}{
						"container_port": p.ContainerPort,
						"host_port":      p.HostPort,
						"protocol":       p.Protocol,
					}),
					ScanID:    scanID,
					Timestamp: scanTime,
				}
				result.Events = append(result.Events, portEvent)
			}
		} else {
			// Check for changes
			changes := detectChanges(prev, current)
			if len(changes) > 0 {
				event := models.DiscoveryEvent{
					ID:        uuid.New().String(),
					EventType: models.EventAssetUpdated,
					AssetID:   current.AssetID,
					AssetName: current.Name,
					Details:   toJSON(changes),
					ScanID:    scanID,
					Timestamp: scanTime,
				}
				result.Events = append(result.Events, event)
				slog.Info("Asset updated",
					"event_type", models.EventAssetUpdated,
					"asset_id", current.AssetID,
					"asset_name", current.Name,
					"changes", changes,
					"source", "discovery-engine",
					"scan_id", scanID,
				)
			}
		}
	}

	// Check for removed assets (were stored but not in current scan)
	for _, prev := range stored {
		if prev.Status == "removed" {
			continue // Already marked as removed
		}
		if _, stillExists := scannedByName[prev.Name]; !stillExists {
			event := models.DiscoveryEvent{
				ID:        uuid.New().String(),
				EventType: models.EventAssetRemoved,
				AssetID:   prev.AssetID,
				AssetName: prev.Name,
				Details:   toJSON(map[string]interface{}{
					"previous_status": prev.Status,
					"image":          prev.Image,
				}),
				ScanID:    scanID,
				Timestamp: scanTime,
			}
			result.Events = append(result.Events, event)
			slog.Info("Asset removed",
				"event_type", models.EventAssetRemoved,
				"asset_id", prev.AssetID,
				"asset_name", prev.Name,
				"source", "discovery-engine",
				"scan_id", scanID,
			)
		}
	}

	// Reconcile relationships
	storedRelMap := make(map[string]bool)
	for _, r := range storedRels {
		storedRelMap[r.ID] = true
	}
	scannedRelMap := make(map[string]bool)
	for _, r := range scannedRels {
		scannedRelMap[r.ID] = true
	}

	for _, rel := range scannedRels {
		if !storedRelMap[rel.ID] {
			event := models.DiscoveryEvent{
				ID:        uuid.New().String(),
				EventType: models.EventNetworkRelationshipCreated,
				AssetID:   rel.SourceAssetID,
				AssetName: "",
				Details:   toJSON(map[string]interface{}{
					"source_asset_id": rel.SourceAssetID,
					"dest_asset_id":   rel.DestAssetID,
					"network":         rel.Network,
					"relationship_type": rel.RelationshipType,
				}),
				ScanID:    scanID,
				Timestamp: scanTime,
			}
			result.Events = append(result.Events, event)
		}
	}

	for _, rel := range storedRels {
		if !scannedRelMap[rel.ID] {
			event := models.DiscoveryEvent{
				ID:        uuid.New().String(),
				EventType: models.EventNetworkRelationshipRemoved,
				AssetID:   rel.SourceAssetID,
				AssetName: "",
				Details:   toJSON(map[string]interface{}{
					"source_asset_id": rel.SourceAssetID,
					"dest_asset_id":   rel.DestAssetID,
					"network":         rel.Network,
				}),
				ScanID:    scanID,
				Timestamp: scanTime,
			}
			result.Events = append(result.Events, event)
		}
	}

	return result
}

// detectChanges compares two versions of the same asset and returns
// a map of field names to their old and new values.
func detectChanges(prev, current models.Asset) map[string]interface{} {
	changes := make(map[string]interface{})

	if prev.Status != current.Status {
		changes["status"] = map[string]string{"old": prev.Status, "new": current.Status}
	}
	if prev.Image != current.Image {
		changes["image"] = map[string]string{"old": prev.Image, "new": current.Image}
	}
	if prev.ImageID != current.ImageID {
		changes["image_id"] = map[string]string{"old": prev.ImageID, "new": current.ImageID}
	}
	if prev.ContainerID != current.ContainerID {
		changes["container_id"] = map[string]string{"old": prev.ContainerID, "new": current.ContainerID}
	}

	return changes
}

func toJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}
