package models

import "time"

// Asset represents a discovered infrastructure component (currently only containers).
type Asset struct {
	AssetID     string            `json:"asset_id"`
	AssetType   string            `json:"asset_type"` // "container"
	Name        string            `json:"name"`        // Docker container name (cleaned, no leading /)
	ContainerID string            `json:"container_id"` // Docker container ID (short, 12 chars)
	Image       string            `json:"image"`
	ImageID     string            `json:"image_id"`
	Status      string            `json:"status"` // "running", "exited", "paused", "created", "removed"
	CreatedAt   time.Time         `json:"created_at"`
	StartedAt   time.Time         `json:"started_at"`
	Labels      map[string]string `json:"labels"`
	Ports       []AssetPort       `json:"ports"`
	Networks    []AssetNetwork    `json:"networks"`

	DiscoveredAt time.Time `json:"discovered_at"`
	LastSeen     time.Time `json:"last_seen"`
}

// AssetPort represents a port exposed or published by a container.
type AssetPort struct {
	AssetID       string `json:"asset_id"`
	ContainerPort int    `json:"container_port"`
	HostPort      int    `json:"host_port"` // 0 if not published to host
	Protocol      string `json:"protocol"`  // "tcp" or "udp"
	HostIP        string `json:"host_ip"`   // "" if not published
}

// AssetNetwork represents a container's membership in a Docker network.
// This represents potential connectivity only — sharing a network does NOT
// mean actual communication has been observed. Observed communication
// relationships belong to the runtime monitoring phase.
type AssetNetwork struct {
	AssetID   string `json:"asset_id"`
	Network   string `json:"network"`
	IPAddress string `json:"ip_address"`
	Gateway   string `json:"gateway"`
}

// Relationship represents a network-level relationship between two assets.
// In Phase 2, the only relationship type is NETWORK_MEMBERSHIP, which means
// both assets are attached to the same Docker network and therefore have
// potential connectivity. This does NOT imply observed application-level
// communication occurred between them.
type Relationship struct {
	ID               string    `json:"id"`
	SourceAssetID    string    `json:"source_asset_id"`
	DestAssetID      string    `json:"dest_asset_id"`
	Network          string    `json:"network"`
	RelationshipType string    `json:"relationship_type"` // "NETWORK_MEMBERSHIP"
	DiscoveredAt     time.Time `json:"discovered_at"`
	LastSeen         time.Time `json:"last_seen"`
}

// DiscoveryEvent represents a change detected by the discovery engine.
type DiscoveryEvent struct {
	ID        string    `json:"id"`
	EventType string    `json:"event_type"` // ASSET_DISCOVERED, ASSET_UPDATED, ASSET_REMOVED, etc.
	AssetID   string    `json:"asset_id"`
	AssetName string    `json:"asset_name"`
	Details   string    `json:"details"` // JSON string with change details
	ScanID    string    `json:"scan_id"`
	Timestamp time.Time `json:"timestamp"`
}

// Event type constants.
const (
	EventAssetDiscovered           = "ASSET_DISCOVERED"
	EventAssetUpdated              = "ASSET_UPDATED"
	EventAssetRemoved              = "ASSET_REMOVED"
	EventNetworkRelationshipCreated = "NETWORK_RELATIONSHIP_CREATED"
	EventNetworkRelationshipRemoved = "NETWORK_RELATIONSHIP_REMOVED"
	EventPortDiscovered            = "PORT_DISCOVERED"
)

// DiscoveryResult holds the output of a single discovery scan.
type DiscoveryResult struct {
	ScanID        string         `json:"scan_id"`
	Timestamp     time.Time      `json:"timestamp"`
	Assets        []Asset        `json:"assets"`
	Relationships []Relationship `json:"relationships"`
}
