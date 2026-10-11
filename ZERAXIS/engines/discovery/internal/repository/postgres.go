package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "github.com/lib/pq"
	"github.com/pavanvanish/zeraxis/engines/discovery/internal/models"
)

// Postgres implements the discovery Repository interface.
type Postgres struct {
	db *sql.DB
}

// NewPostgres creates a new PostgreSQL repository and runs migrations.
func NewPostgres(databaseURL string, migrationsDir string) (*Postgres, error) {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	repo := &Postgres{db: db}

	if err := repo.runMigrations(migrationsDir); err != nil {
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return repo, nil
}

// runMigrations applies SQL migration files in order.
func (p *Postgres) runMigrations(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("failed to read migrations dir: %w", err)
	}

	// Sort migration files by name
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)

	for _, f := range files {
		content, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil {
			return fmt.Errorf("failed to read migration %s: %w", f, err)
		}
		if _, err := p.db.Exec(string(content)); err != nil {
			return fmt.Errorf("failed to run migration %s: %w", f, err)
		}
		slog.Info("Migration applied", "file", f)
	}

	return nil
}

// Ping checks if the database is reachable.
func (p *Postgres) Ping() error {
	return p.db.Ping()
}

// UpsertAsset inserts or updates an asset by name.
func (p *Postgres) UpsertAsset(ctx context.Context, asset models.Asset) error {
	labelsJSON, _ := json.Marshal(asset.Labels)

	_, err := p.db.ExecContext(ctx, `
		INSERT INTO discovery_assets (asset_id, asset_type, name, container_id, image, image_id, status, created_at, started_at, labels, discovered_at, last_seen)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (name) DO UPDATE SET
			container_id = EXCLUDED.container_id,
			image = EXCLUDED.image,
			image_id = EXCLUDED.image_id,
			status = EXCLUDED.status,
			created_at = EXCLUDED.created_at,
			started_at = EXCLUDED.started_at,
			labels = EXCLUDED.labels,
			last_seen = EXCLUDED.last_seen
	`, asset.AssetID, asset.AssetType, asset.Name, asset.ContainerID,
		asset.Image, asset.ImageID, asset.Status,
		asset.CreatedAt, asset.StartedAt, labelsJSON,
		asset.DiscoveredAt, asset.LastSeen)

	return err
}

// UpsertPorts replaces all ports for an asset.
func (p *Postgres) UpsertPorts(ctx context.Context, assetID string, ports []models.AssetPort) error {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, "DELETE FROM discovery_asset_ports WHERE asset_id = $1", assetID)
	if err != nil {
		return err
	}

	for _, port := range ports {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO discovery_asset_ports (asset_id, container_port, host_port, protocol, host_ip)
			VALUES ($1, $2, $3, $4, $5)
		`, assetID, port.ContainerPort, port.HostPort, port.Protocol, port.HostIP)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// UpsertNetworks replaces all networks for an asset.
func (p *Postgres) UpsertNetworks(ctx context.Context, assetID string, networks []models.AssetNetwork) error {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, "DELETE FROM discovery_asset_networks WHERE asset_id = $1", assetID)
	if err != nil {
		return err
	}

	for _, net := range networks {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO discovery_asset_networks (asset_id, network, ip_address, gateway)
			VALUES ($1, $2, $3, $4)
		`, assetID, net.Network, net.IPAddress, net.Gateway)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// UpsertRelationship inserts or updates a relationship.
func (p *Postgres) UpsertRelationship(ctx context.Context, rel models.Relationship) error {
	_, err := p.db.ExecContext(ctx, `
		INSERT INTO discovery_relationships (id, source_asset_id, dest_asset_id, network, relationship_type, discovered_at, last_seen)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (source_asset_id, dest_asset_id, network, relationship_type) DO UPDATE SET
			last_seen = EXCLUDED.last_seen
	`, rel.ID, rel.SourceAssetID, rel.DestAssetID, rel.Network, rel.RelationshipType, rel.DiscoveredAt, rel.LastSeen)
	return err
}

// MarkAssetRemoved sets status to removed and updates last_seen.
func (p *Postgres) MarkAssetRemoved(ctx context.Context, assetID string, timestamp time.Time) error {
	_, err := p.db.ExecContext(ctx, `
		UPDATE discovery_assets SET status = 'removed', last_seen = $1 WHERE asset_id = $2
	`, timestamp, assetID)
	return err
}

// RemoveRelationship deletes a relationship by ID.
func (p *Postgres) RemoveRelationship(ctx context.Context, relID string) error {
	_, err := p.db.ExecContext(ctx, `DELETE FROM discovery_relationships WHERE id = $1`, relID)
	return err
}

// RecordEvent persists a discovery event.
func (p *Postgres) RecordEvent(ctx context.Context, event models.DiscoveryEvent) error {
	_, err := p.db.ExecContext(ctx, `
		INSERT INTO discovery_events (id, event_type, asset_id, asset_name, details, scan_id, timestamp)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, event.ID, event.EventType, event.AssetID, event.AssetName, event.Details, event.ScanID, event.Timestamp)
	return err
}

// GetAllAssets returns all known assets with their ports and networks.
func (p *Postgres) GetAllAssets(ctx context.Context) ([]models.Asset, error) {
	rows, err := p.db.QueryContext(ctx, `
		SELECT asset_id, asset_type, name, container_id, image, image_id, status,
		       created_at, started_at, labels, discovered_at, last_seen
		FROM discovery_assets
		ORDER BY name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var assets []models.Asset
	for rows.Next() {
		var a models.Asset
		var labelsJSON []byte
		var createdAt, startedAt sql.NullTime
		var containerID, image, imageID sql.NullString

		err := rows.Scan(&a.AssetID, &a.AssetType, &a.Name, &containerID,
			&image, &imageID, &a.Status,
			&createdAt, &startedAt, &labelsJSON,
			&a.DiscoveredAt, &a.LastSeen)
		if err != nil {
			return nil, err
		}

		if containerID.Valid {
			a.ContainerID = containerID.String
		}
		if image.Valid {
			a.Image = image.String
		}
		if imageID.Valid {
			a.ImageID = imageID.String
		}

		if createdAt.Valid {
			a.CreatedAt = createdAt.Time
		}
		if startedAt.Valid {
			a.StartedAt = startedAt.Time
		}

		if len(labelsJSON) > 0 {
			json.Unmarshal(labelsJSON, &a.Labels)
		}
		if a.Labels == nil {
			a.Labels = make(map[string]string)
		}

		assets = append(assets, a)
	}
	
	// Close rows to free the connection before making new queries
	rows.Close()

	for i := range assets {
		// Load ports
		assets[i].Ports, _ = p.getAssetPorts(ctx, assets[i].AssetID)
		// Load networks
		assets[i].Networks, _ = p.getAssetNetworks(ctx, assets[i].AssetID)
	}

	return assets, rows.Err()
}

// GetAssetByID returns a single asset by its ID.
func (p *Postgres) GetAssetByID(ctx context.Context, assetID string) (*models.Asset, error) {
	var a models.Asset
	var labelsJSON []byte
	var createdAt, startedAt sql.NullTime
	var containerID, image, imageID sql.NullString

	err := p.db.QueryRowContext(ctx, `
		SELECT asset_id, asset_type, name, container_id, image, image_id, status,
		       created_at, started_at, labels, discovered_at, last_seen
		FROM discovery_assets
		WHERE asset_id = $1
	`, assetID).Scan(&a.AssetID, &a.AssetType, &a.Name, &containerID,
		&image, &imageID, &a.Status,
		&createdAt, &startedAt, &labelsJSON,
		&a.DiscoveredAt, &a.LastSeen)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if containerID.Valid {
		a.ContainerID = containerID.String
	}
	if image.Valid {
		a.Image = image.String
	}
	if imageID.Valid {
		a.ImageID = imageID.String
	}

	if createdAt.Valid {
		a.CreatedAt = createdAt.Time
	}
	if startedAt.Valid {
		a.StartedAt = startedAt.Time
	}

	if len(labelsJSON) > 0 {
		json.Unmarshal(labelsJSON, &a.Labels)
	}
	if a.Labels == nil {
		a.Labels = make(map[string]string)
	}

	a.Ports, _ = p.getAssetPorts(ctx, a.AssetID)
	a.Networks, _ = p.getAssetNetworks(ctx, a.AssetID)

	return &a, nil
}

func (p *Postgres) getAssetPorts(ctx context.Context, assetID string) ([]models.AssetPort, error) {
	rows, err := p.db.QueryContext(ctx, `
		SELECT asset_id, container_port, host_port, protocol, host_ip
		FROM discovery_asset_ports
		WHERE asset_id = $1
		ORDER BY container_port
	`, assetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ports []models.AssetPort
	for rows.Next() {
		var p models.AssetPort
		if err := rows.Scan(&p.AssetID, &p.ContainerPort, &p.HostPort, &p.Protocol, &p.HostIP); err != nil {
			return nil, err
		}
		ports = append(ports, p)
	}
	return ports, rows.Err()
}

func (p *Postgres) getAssetNetworks(ctx context.Context, assetID string) ([]models.AssetNetwork, error) {
	rows, err := p.db.QueryContext(ctx, `
		SELECT asset_id, network, ip_address, gateway
		FROM discovery_asset_networks
		WHERE asset_id = $1
		ORDER BY network
	`, assetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var networks []models.AssetNetwork
	for rows.Next() {
		var n models.AssetNetwork
		if err := rows.Scan(&n.AssetID, &n.Network, &n.IPAddress, &n.Gateway); err != nil {
			return nil, err
		}
		networks = append(networks, n)
	}
	return networks, rows.Err()
}

// GetRelationships returns all relationships.
func (p *Postgres) GetRelationships(ctx context.Context) ([]models.Relationship, error) {
	rows, err := p.db.QueryContext(ctx, `
		SELECT r.id, r.source_asset_id, r.dest_asset_id, r.network, r.relationship_type, r.discovered_at, r.last_seen
		FROM discovery_relationships r
		ORDER BY r.network, r.source_asset_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rels []models.Relationship
	for rows.Next() {
		var r models.Relationship
		if err := rows.Scan(&r.ID, &r.SourceAssetID, &r.DestAssetID, &r.Network, &r.RelationshipType, &r.DiscoveredAt, &r.LastSeen); err != nil {
			return nil, err
		}
		rels = append(rels, r)
	}
	return rels, rows.Err()
}

// GetEvents returns the most recent discovery events.
func (p *Postgres) GetEvents(ctx context.Context, limit int) ([]models.DiscoveryEvent, error) {
	if limit <= 0 {
		limit = 100
	}

	rows, err := p.db.QueryContext(ctx, `
		SELECT id, event_type, asset_id, asset_name, details, scan_id, timestamp
		FROM discovery_events
		ORDER BY timestamp DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []models.DiscoveryEvent
	for rows.Next() {
		var e models.DiscoveryEvent
		if err := rows.Scan(&e.ID, &e.EventType, &e.AssetID, &e.AssetName, &e.Details, &e.ScanID, &e.Timestamp); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

func (p *Postgres) InsertUnifiedEvent(ctx context.Context, eventBytes []byte) error {
	// Unmarshal to access fields, but keep original bytes for JSONB
	// We need the raw struct to easily extract the fields.
	// Actually, we can just use a map or struct.
	// Let's decode into a map since we don't import pkg/events here to avoid circular dependencies if not needed,
	// wait we can import pkg/events since we already modified go.mod.
	// But to avoid importing pkg/events in repo if we just want to execute a query:
	
	// We should just accept the structured type from the caller or parse it.
	// Let's use the standard Go JSON parsing to extract what we need.
	var evt struct {
		EventID       string `json:"event_id"`
		EventType     string `json:"event_type"`
		EventVersion  int    `json:"event_version"`
		Timestamp     time.Time `json:"timestamp"`
		Source        string `json:"source"`
		AssetID       string `json:"asset_id"`
		ImageDigest   string `json:"image_digest"`
		Severity      string `json:"severity"`
		CorrelationID *string `json:"correlation_id"`
		TraceID       *string `json:"trace_id"`
		Metadata      json.RawMessage `json:"metadata"`
		Subject       string `json:"subject"`
	}
	if err := json.Unmarshal(eventBytes, &evt); err != nil {
		return err
	}

	_, err := p.db.ExecContext(ctx, `
		INSERT INTO events (
			event_id, event_type, event_version, timestamp, source, 
			asset_id, image_digest, severity, correlation_id, trace_id, 
			metadata, subject
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`, evt.EventID, evt.EventType, evt.EventVersion, evt.Timestamp, evt.Source,
		nullIfEmpty(evt.AssetID), nullIfEmpty(evt.ImageDigest), nullIfEmpty(evt.Severity),
		evt.CorrelationID, evt.TraceID, evt.Metadata, evt.Subject)
	return err
}

func nullIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// Close closes the database connection.
func (p *Postgres) Close() error {
	return p.db.Close()
}
