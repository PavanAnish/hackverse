package discovery

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/google/uuid"
	dockerclient "github.com/pavanvanish/zeraxis/engines/discovery/internal/docker"
	"github.com/pavanvanish/zeraxis/engines/discovery/internal/models"
)

// Scanner discovers infrastructure by querying the Docker Engine API.
type Scanner struct {
	docker *dockerclient.Client
}

// NewScanner creates a new discovery scanner.
func NewScanner(docker *dockerclient.Client) *Scanner {
	return &Scanner{docker: docker}
}

// Scan performs a full discovery scan of the Docker environment.
// It is safe to call repeatedly — produces a consistent snapshot each time.
func (s *Scanner) Scan(ctx context.Context) (*models.DiscoveryResult, error) {
	scanID := uuid.New().String()
	scanTime := time.Now().UTC()

	slog.Info("Starting discovery scan", "scan_id", scanID)

	containers, err := s.docker.ListContainers(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	var assets []models.Asset
	// Track which assets are on which networks for relationship building
	networkMembers := make(map[string][]string) // network -> []assetID

	for _, c := range containers {
		info, err := s.docker.InspectContainer(ctx, c.ID)
		if err != nil {
			// Don't fail the entire scan because one container disappeared
			slog.Warn("Failed to inspect container, skipping",
				"container_id", c.ID[:12],
				"error", err,
				"scan_id", scanID,
			)
			continue
		}

		assetID := generateAssetID(info.Name)

		asset := models.Asset{
			AssetID:     assetID,
			AssetType:   "container",
			Name:        info.Name,
			ContainerID: info.ID,
			Image:       info.Image,
			ImageID:     info.ImageID,
			Status:      info.Status,
			Labels:      info.Labels,
			DiscoveredAt: scanTime,
			LastSeen:    scanTime,
		}

		// Parse timestamps
		if t, err := time.Parse(time.RFC3339Nano, info.CreatedAt); err == nil {
			asset.CreatedAt = t
		}
		if t, err := time.Parse(time.RFC3339Nano, info.StartedAt); err == nil {
			asset.StartedAt = t
		}

		// Extract ports
		for _, p := range info.Ports {
			asset.Ports = append(asset.Ports, models.AssetPort{
				AssetID:       assetID,
				ContainerPort: p.ContainerPort,
				HostPort:      p.HostPort,
				Protocol:      p.Protocol,
				HostIP:        p.HostIP,
			})
		}

		// Extract networks
		for _, n := range info.Networks {
			asset.Networks = append(asset.Networks, models.AssetNetwork{
				AssetID:   assetID,
				Network:   n.Name,
				IPAddress: n.IPAddress,
				Gateway:   n.Gateway,
			})
			networkMembers[n.Name] = append(networkMembers[n.Name], assetID)
		}

		assets = append(assets, asset)
	}

	// Build network relationships
	// NETWORK_MEMBERSHIP means two assets share the same Docker network,
	// establishing potential connectivity. This does NOT indicate that
	// actual application-level communication has been observed between them.
	var relationships []models.Relationship
	for networkName, memberIDs := range networkMembers {
		// Sort for deterministic relationship IDs
		sort.Strings(memberIDs)
		for i := 0; i < len(memberIDs); i++ {
			for j := i + 1; j < len(memberIDs); j++ {
				relID := generateRelationshipID(memberIDs[i], memberIDs[j], networkName)
				relationships = append(relationships, models.Relationship{
					ID:               relID,
					SourceAssetID:    memberIDs[i],
					DestAssetID:      memberIDs[j],
					Network:          networkName,
					RelationshipType: "NETWORK_MEMBERSHIP",
					DiscoveredAt:     scanTime,
					LastSeen:         scanTime,
				})
			}
		}
	}

	result := &models.DiscoveryResult{
		ScanID:        scanID,
		Timestamp:     scanTime,
		Assets:        assets,
		Relationships: relationships,
	}

	slog.Info("Discovery scan complete",
		"scan_id", scanID,
		"assets_found", len(assets),
		"relationships_found", len(relationships),
		"duration_ms", time.Since(scanTime).Milliseconds(),
	)

	return result, nil
}

// generateAssetID creates a stable, deterministic UUID for a container
// based on its name. This ensures the same container name always maps to
// the same asset_id, surviving container recreations.
func generateAssetID(containerName string) string {
	h := sha256.Sum256([]byte("zeraxis:asset:" + containerName))
	id, _ := uuid.FromBytes(h[:16])
	return id.String()
}

// generateRelationshipID creates a deterministic ID for a relationship.
func generateRelationshipID(sourceID, destID, network string) string {
	h := sha256.Sum256([]byte("zeraxis:rel:" + sourceID + ":" + destID + ":" + network))
	id, _ := uuid.FromBytes(h[:16])
	return id.String()
}
