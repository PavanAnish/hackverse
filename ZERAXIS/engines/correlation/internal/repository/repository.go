// Package repository handles all PostgreSQL interactions for the Correlation Engine.
// All SQL uses parameterized queries — no string interpolation of external data.
package repository

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "github.com/lib/pq"
	"github.com/pavanvanish/zeraxis/engines/correlation/internal/models"
)

// Repository wraps PostgreSQL operations for the Correlation Engine.
type Repository struct {
	db *sql.DB
}

// New creates a new Repository, connects to Postgres, and runs migrations.
func New(databaseURL, migrationsDir string) (*Repository, error) {
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

	r := &Repository{db: db}
	if err := r.runMigrations(migrationsDir); err != nil {
		return nil, fmt.Errorf("migrations failed: %w", err)
	}
	return r, nil
}

func (r *Repository) runMigrations(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("failed to read migrations dir %s: %w", dir, err)
	}
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
		if _, err := r.db.Exec(string(content)); err != nil {
			return fmt.Errorf("failed to apply migration %s: %w", f, err)
		}
		slog.Info("Migration applied", "file", f)
	}
	return nil
}

// Ping checks connectivity.
func (r *Repository) Ping() error { return r.db.Ping() }

// Close closes the database connection.
func (r *Repository) Close() error { return r.db.Close() }

// DB returns the underlying *sql.DB for use in tests.
func (r *Repository) DB() *sql.DB { return r.db }

// GetIncidentByFingerprint returns the incident matching the fingerprint, or nil if not found.
func (r *Repository) GetIncidentByFingerprint(ctx context.Context, fp string) (*models.Incident, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT incident_id, fingerprint, status, coalesce(asset_id,''),
		       coalesce(container_id,''), coalesce(image_digest,''),
		       severity, finding_count,
		       first_seen, last_seen, coalesce(resolved_at, '0001-01-01'::timestamptz),
		       created_at, updated_at
		FROM incidents
		WHERE fingerprint = $1
		ORDER BY created_at DESC
		LIMIT 1
	`, fp)

	inc := &models.Incident{}
	var resolvedAt time.Time
	err := row.Scan(
		&inc.IncidentID, &inc.Fingerprint, &inc.Status,
		&inc.AssetID, &inc.ContainerID, &inc.ImageDigest,
		&inc.Severity, &inc.FindingCount,
		&inc.FirstSeen, &inc.LastSeen, &resolvedAt,
		&inc.CreatedAt, &inc.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get incident by fingerprint: %w", err)
	}
	if !resolvedAt.IsZero() && resolvedAt.Year() > 1 {
		inc.ResolvedAt = &resolvedAt
	}
	return inc, nil
}

// GetIncidentByID returns an incident by its primary key.
func (r *Repository) GetIncidentByID(ctx context.Context, id string) (*models.Incident, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT incident_id, fingerprint, status, coalesce(asset_id,''),
		       coalesce(container_id,''), coalesce(image_digest,''),
		       severity, finding_count,
		       first_seen, last_seen, coalesce(resolved_at, '0001-01-01'::timestamptz),
		       created_at, updated_at
		FROM incidents
		WHERE incident_id = $1
	`, id)

	inc := &models.Incident{}
	var resolvedAt time.Time
	err := row.Scan(
		&inc.IncidentID, &inc.Fingerprint, &inc.Status,
		&inc.AssetID, &inc.ContainerID, &inc.ImageDigest,
		&inc.Severity, &inc.FindingCount,
		&inc.FirstSeen, &inc.LastSeen, &resolvedAt,
		&inc.CreatedAt, &inc.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get incident by id: %w", err)
	}
	if !resolvedAt.IsZero() && resolvedAt.Year() > 1 {
		inc.ResolvedAt = &resolvedAt
	}
	return inc, nil
}

// ListIncidents returns incidents filtered by optional status, with pagination.
// maxRows is capped at 100 to prevent full-table dumps.
func (r *Repository) ListIncidents(ctx context.Context, status string, limit, offset int) ([]*models.Incident, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	var (
		rows *sql.Rows
		err  error
	)
	if status != "" {
		rows, err = r.db.QueryContext(ctx, `
			SELECT incident_id, fingerprint, status, coalesce(asset_id,''),
			       coalesce(container_id,''), coalesce(image_digest,''),
			       severity, finding_count, first_seen, last_seen,
			       created_at, updated_at
			FROM incidents WHERE status = $1
			ORDER BY last_seen DESC LIMIT $2 OFFSET $3
		`, status, limit, offset)
	} else {
		rows, err = r.db.QueryContext(ctx, `
			SELECT incident_id, fingerprint, status, coalesce(asset_id,''),
			       coalesce(container_id,''), coalesce(image_digest,''),
			       severity, finding_count, first_seen, last_seen,
			       created_at, updated_at
			FROM incidents
			ORDER BY last_seen DESC LIMIT $1 OFFSET $2
		`, limit, offset)
	}
	if err != nil {
		return nil, fmt.Errorf("list incidents: %w", err)
	}
	defer rows.Close()

	var out []*models.Incident
	for rows.Next() {
		inc := &models.Incident{}
		if err := rows.Scan(
			&inc.IncidentID, &inc.Fingerprint, &inc.Status,
			&inc.AssetID, &inc.ContainerID, &inc.ImageDigest,
			&inc.Severity, &inc.FindingCount,
			&inc.FirstSeen, &inc.LastSeen,
			&inc.CreatedAt, &inc.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan incident: %w", err)
		}
		out = append(out, inc)
	}
	return out, rows.Err()
}

// GetIncidentFindings returns finding_ids linked to an incident.
func (r *Repository) GetIncidentFindings(ctx context.Context, incidentID string) ([]models.IncidentFinding, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT incident_id, finding_id, linked_at
		FROM incident_findings
		WHERE incident_id = $1
		ORDER BY linked_at ASC
	`, incidentID)
	if err != nil {
		return nil, fmt.Errorf("get incident findings: %w", err)
	}
	defer rows.Close()

	var out []models.IncidentFinding
	for rows.Next() {
		var f models.IncidentFinding
		if err := rows.Scan(&f.IncidentID, &f.FindingID, &f.LinkedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// CreateIncidentWithFinding atomically creates an incident and links the first finding.
// Implements race-safe upsert to handle concurrent finding correlation.
func (r *Repository) CreateIncidentWithFinding(ctx context.Context, inc *models.Incident, findingID string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	var actualIncidentID string
	err = tx.QueryRowContext(ctx, `
		INSERT INTO incidents (
			incident_id, fingerprint, status, asset_id, container_id, image_digest,
			severity, finding_count, first_seen, last_seen, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NOW(),NOW())
		ON CONFLICT (fingerprint) WHERE status NOT IN ('RESOLVED', 'CLOSED') DO UPDATE SET 
			last_seen = GREATEST(incidents.last_seen, EXCLUDED.last_seen),
			updated_at = NOW()
		RETURNING incident_id
	`,
		inc.IncidentID, inc.Fingerprint, string(inc.Status),
		nullStr(inc.AssetID), nullStr(inc.ContainerID), nullStr(inc.ImageDigest),
		inc.Severity, 1,
		inc.FirstSeen, inc.LastSeen,
	).Scan(&actualIncidentID)
	
	if err != nil {
		return fmt.Errorf("insert/upsert incident: %w", err)
	}

	inc.IncidentID = actualIncidentID

	res, err := tx.ExecContext(ctx, `
		INSERT INTO incident_findings (incident_id, finding_id)
		VALUES ($1, $2)
		ON CONFLICT DO NOTHING
	`, inc.IncidentID, findingID)
	if err != nil {
		return fmt.Errorf("link finding: %w", err)
	}

	rowsAffected, _ := res.RowsAffected()
	if rowsAffected > 0 {
		_, err = tx.ExecContext(ctx, `
			UPDATE incidents 
			SET finding_count = (SELECT count(*) FROM incident_findings WHERE incident_id = $1)
			WHERE incident_id = $1
		`, inc.IncidentID)
		if err != nil {
			return fmt.Errorf("update finding count: %w", err)
		}
	}

	return tx.Commit()
}

// AddFindingToIncident atomically links a finding and updates the incident state.
// Returns false if the finding was already linked (idempotent).
func (r *Repository) AddFindingToIncident(ctx context.Context, incidentID, findingID string, lastSeen time.Time, newSeverity string) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	// Insert link — idempotent
	res, err := tx.ExecContext(ctx, `
		INSERT INTO incident_findings (incident_id, finding_id)
		VALUES ($1, $2)
		ON CONFLICT DO NOTHING
	`, incidentID, findingID)
	if err != nil {
		return false, fmt.Errorf("link finding: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		// Already linked — no update needed
		tx.Commit() //nolint:errcheck
		return false, nil
	}

	// Update incident: last_seen, severity (monotonically increasing), finding_count, status → ACTIVE
	_, err = tx.ExecContext(ctx, `
		UPDATE incidents SET
			last_seen     = GREATEST(last_seen, $1),
			severity      = CASE
				WHEN $2 = 'CRITICAL' THEN 'CRITICAL'
				WHEN severity = 'CRITICAL' THEN 'CRITICAL'
				WHEN $2 = 'HIGH' AND severity IN ('INFO','LOW','MEDIUM') THEN 'HIGH'
				WHEN severity = 'HIGH' THEN 'HIGH'
				WHEN $2 = 'MEDIUM' AND severity IN ('INFO','LOW') THEN 'MEDIUM'
				WHEN severity = 'MEDIUM' THEN 'MEDIUM'
				WHEN $2 = 'LOW' AND severity = 'INFO' THEN 'LOW'
				ELSE severity
			END,
			finding_count = finding_count + 1,
			status        = 'ACTIVE',
			updated_at    = NOW()
		WHERE incident_id = $3
	`, lastSeen, newSeverity, incidentID)
	if err != nil {
		return false, fmt.Errorf("update incident: %w", err)
	}

	return true, tx.Commit()
}

// IsAlreadyLinked checks whether a finding is already linked to any incident.
func (r *Repository) IsAlreadyLinked(ctx context.Context, findingID string) (bool, string, error) {
	var incidentID string
	err := r.db.QueryRowContext(ctx, `
		SELECT incident_id FROM incident_findings WHERE finding_id = $1 LIMIT 1
	`, findingID).Scan(&incidentID)
	if err == sql.ErrNoRows {
		return false, "", nil
	}
	if err != nil {
		return false, "", err
	}
	return true, incidentID, nil
}

// CountActiveIncidents returns the number of non-CLOSED incidents for memory bound checks.
func (r *Repository) CountActiveIncidents(ctx context.Context) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM incidents WHERE status != 'CLOSED'`).Scan(&n)
	return n, err
}

func nullStr(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
