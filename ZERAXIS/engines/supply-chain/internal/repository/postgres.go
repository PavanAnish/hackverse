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
	"github.com/pavanvanish/zeraxis/engines/supply-chain/internal/models"
)

type Postgres struct {
	db *sql.DB
}

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

func (p *Postgres) runMigrations(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("failed to read migrations dir: %w", err)
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
		if _, err := p.db.Exec(string(content)); err != nil {
			return fmt.Errorf("failed to run migration %s: %w", f, err)
		}
		slog.Info("Migration applied", "file", f)
	}

	return nil
}

func (p *Postgres) Ping() error {
	return p.db.Ping()
}

func (p *Postgres) UpsertImage(ctx context.Context, img models.Image) error {
	_, err := p.db.ExecContext(ctx, `
		INSERT INTO supply_chain_images (image_id, repository, tag, digest, created_at, size_bytes, discovered_at, last_scanned_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (image_id) DO UPDATE SET
			repository = EXCLUDED.repository,
			tag = EXCLUDED.tag,
			digest = EXCLUDED.digest,
			last_scanned_at = EXCLUDED.last_scanned_at
	`, img.ImageID, img.Repository, img.Tag, img.Digest, img.CreatedAt, img.SizeBytes, img.DiscoveredAt, img.LastScannedAt)
	return err
}

func (p *Postgres) UpsertAssetImage(ctx context.Context, assetID, imageID string) error {
	_, err := p.db.ExecContext(ctx, `
		INSERT INTO supply_chain_asset_images (asset_id, image_id, discovered_at, last_seen)
		VALUES ($1, $2, NOW(), NOW())
		ON CONFLICT (asset_id, image_id) DO UPDATE SET
			last_seen = NOW()
	`, assetID, imageID)
	return err
}

func (p *Postgres) CreateScan(ctx context.Context, scan models.Scan) error {
	_, err := p.db.ExecContext(ctx, `
		INSERT INTO supply_chain_scans (scan_id, image_id, scan_type, status, started_at, syft_version, trivy_version)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, scan.ScanID, scan.ImageID, scan.ScanType, scan.Status, scan.StartedAt, scan.SyftVersion, scan.TrivyVersion)
	return err
}

func (p *Postgres) CompleteScan(ctx context.Context, scanID, status, errorMsg string) error {
	_, err := p.db.ExecContext(ctx, `
		UPDATE supply_chain_scans 
		SET status = $1, completed_at = NOW(), error = $2
		WHERE scan_id = $3
	`, status, errorMsg, scanID)
	return err
}

func (p *Postgres) CreateSBOM(ctx context.Context, sbom models.SBOM) error {
	_, err := p.db.ExecContext(ctx, `
		INSERT INTO supply_chain_sboms (sbom_id, image_id, scan_id, format, spec_version, generated_at, tool, tool_version, document_digest, package_count, raw_sbom)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`, sbom.SBOMID, sbom.ImageID, sbom.ScanID, sbom.Format, sbom.SpecVersion, sbom.GeneratedAt, sbom.Tool, sbom.ToolVersion, sbom.DocumentDigest, sbom.PackageCount, sbom.RawSBOM)
	return err
}

func (p *Postgres) UpsertPackage(ctx context.Context, pkg models.Package) error {
	_, err := p.db.ExecContext(ctx, `
		INSERT INTO supply_chain_packages (package_id, name, version, package_type, purl)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (name, version, package_type) DO UPDATE SET
			purl = EXCLUDED.purl
	`, pkg.PackageID, pkg.Name, pkg.Version, pkg.PackageType, pkg.PURL)
	return err
}

func (p *Postgres) LinkSBOMPackage(ctx context.Context, sbomID, packageID string) error {
	_, err := p.db.ExecContext(ctx, `
		INSERT INTO supply_chain_sbom_packages (sbom_id, package_id)
		VALUES ($1, $2)
		ON CONFLICT (sbom_id, package_id) DO NOTHING
	`, sbomID, packageID)
	return err
}

func (p *Postgres) UpsertVulnerability(ctx context.Context, vuln models.Vulnerability) error {
	_, err := p.db.ExecContext(ctx, `
		INSERT INTO supply_chain_vulnerabilities (vuln_id, vulnerability_identifier, package_id, image_id, scan_id, severity, installed_version, fixed_version, title, description, source, detected_at, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (vulnerability_identifier, package_id, image_id) DO UPDATE SET
			scan_id = EXCLUDED.scan_id,
			severity = EXCLUDED.severity,
			fixed_version = EXCLUDED.fixed_version,
			status = EXCLUDED.status
	`, vuln.VulnID, vuln.VulnerabilityIdentifier, vuln.PackageID, vuln.ImageID, vuln.ScanID, vuln.Severity, vuln.InstalledVersion, vuln.FixedVersion, vuln.Title, vuln.Description, vuln.Source, vuln.DetectedAt, vuln.Status)
	return err
}

func (p *Postgres) CreatePolicyDecision(ctx context.Context, decision models.PolicyDecision) error {
	_, err := p.db.ExecContext(ctx, `
		INSERT INTO supply_chain_policy_decisions (decision_id, image_id, scan_id, policy_version, result, reason, critical_count, high_count, medium_count, low_count, evaluated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`, decision.DecisionID, decision.ImageID, decision.ScanID, decision.PolicyVersion, decision.Result, decision.Reason, decision.CriticalCount, decision.HighCount, decision.MediumCount, decision.LowCount, decision.EvaluatedAt)
	return err
}

func (p *Postgres) GetImages(ctx context.Context) ([]models.Image, error) {
	rows, err := p.db.QueryContext(ctx, `
		SELECT image_id, repository, tag, digest, created_at, size_bytes, discovered_at, last_scanned_at
		FROM supply_chain_images
		ORDER BY discovered_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var images []models.Image
	for rows.Next() {
		var img models.Image
		var createdAt, lastScanned sql.NullTime
		if err := rows.Scan(&img.ImageID, &img.Repository, &img.Tag, &img.Digest, &createdAt, &img.SizeBytes, &img.DiscoveredAt, &lastScanned); err != nil {
			return nil, err
		}
		if createdAt.Valid {
			img.CreatedAt = createdAt.Time
		}
		if lastScanned.Valid {
			img.LastScannedAt = lastScanned.Time
		}
		images = append(images, img)
	}
	return images, nil
}

func (p *Postgres) GetSBOMByImage(ctx context.Context, imageID string) ([]models.SBOM, error) {
	rows, err := p.db.QueryContext(ctx, `
		SELECT sbom_id, image_id, scan_id, format, spec_version, generated_at, tool, tool_version, document_digest, package_count
		FROM supply_chain_sboms
		WHERE image_id = $1
		ORDER BY generated_at DESC
	`, imageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sboms []models.SBOM
	for rows.Next() {
		var s models.SBOM
		var spec sql.NullString
		var toolVersion sql.NullString
		if err := rows.Scan(&s.SBOMID, &s.ImageID, &s.ScanID, &s.Format, &spec, &s.GeneratedAt, &s.Tool, &toolVersion, &s.DocumentDigest, &s.PackageCount); err != nil {
			return nil, err
		}
		if spec.Valid {
			s.SpecVersion = spec.String
		}
		if toolVersion.Valid {
			s.ToolVersion = toolVersion.String
		}
		sboms = append(sboms, s)
	}
	return sboms, nil
}

func (p *Postgres) GetVulnerabilitiesByImage(ctx context.Context, imageID string) ([]models.Vulnerability, error) {
	rows, err := p.db.QueryContext(ctx, `
		SELECT vuln_id, vulnerability_identifier, package_id, image_id, scan_id, severity, installed_version, fixed_version, title, description, source, detected_at, status
		FROM supply_chain_vulnerabilities
		WHERE image_id = $1
	`, imageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var vulns []models.Vulnerability
	for rows.Next() {
		var v models.Vulnerability
		var inst, fixed, title, desc sql.NullString
		if err := rows.Scan(&v.VulnID, &v.VulnerabilityIdentifier, &v.PackageID, &v.ImageID, &v.ScanID, &v.Severity, &inst, &fixed, &title, &desc, &v.Source, &v.DetectedAt, &v.Status); err != nil {
			return nil, err
		}
		if inst.Valid {
			v.InstalledVersion = inst.String
		}
		if fixed.Valid {
			v.FixedVersion = fixed.String
		}
		if title.Valid {
			v.Title = title.String
		}
		if desc.Valid {
			v.Description = desc.String
		}
		vulns = append(vulns, v)
	}
	return vulns, nil
}

func (p *Postgres) GetPolicyDecisions(ctx context.Context) ([]models.PolicyDecision, error) {
	rows, err := p.db.QueryContext(ctx, `
		SELECT decision_id, image_id, scan_id, policy_version, result, reason, critical_count, high_count, medium_count, low_count, evaluated_at
		FROM supply_chain_policy_decisions
		ORDER BY evaluated_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var decisions []models.PolicyDecision
	for rows.Next() {
		var d models.PolicyDecision
		if err := rows.Scan(&d.DecisionID, &d.ImageID, &d.ScanID, &d.PolicyVersion, &d.Result, &d.Reason, &d.CriticalCount, &d.HighCount, &d.MediumCount, &d.LowCount, &d.EvaluatedAt); err != nil {
			return nil, err
		}
		decisions = append(decisions, d)
	}
	return decisions, nil
}

func (p *Postgres) GetScans(ctx context.Context) ([]models.Scan, error) {
	rows, err := p.db.QueryContext(ctx, `
		SELECT scan_id, image_id, scan_type, status, started_at, completed_at, error, syft_version, trivy_version
		FROM supply_chain_scans
		ORDER BY started_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var scans []models.Scan
	for rows.Next() {
		var s models.Scan
		var comp sql.NullTime
		var errStr, syft, trivy sql.NullString
		if err := rows.Scan(&s.ScanID, &s.ImageID, &s.ScanType, &s.Status, &s.StartedAt, &comp, &errStr, &syft, &trivy); err != nil {
			return nil, err
		}
		if comp.Valid { s.CompletedAt = comp.Time }
		if errStr.Valid { s.Error = errStr.String }
		if syft.Valid { s.SyftVersion = syft.String }
		if trivy.Valid { s.TrivyVersion = trivy.String }
		scans = append(scans, s)
	}
	return scans, nil
}

func (p *Postgres) GetAssetSupplyChain(ctx context.Context, assetID string) (models.AssetSupplyChain, error) {
	var result models.AssetSupplyChain
	result.AssetID = assetID

	// Get latest image for asset
	row := p.db.QueryRowContext(ctx, `
		SELECT i.image_id, i.repository, i.tag, i.digest, i.created_at, i.size_bytes, i.discovered_at, i.last_scanned_at
		FROM supply_chain_images i
		JOIN supply_chain_asset_images a ON i.image_id = a.image_id
		WHERE a.asset_id = $1
		ORDER BY a.last_seen DESC LIMIT 1
	`, assetID)

	var img models.Image
	var createdAt, lastScanned sql.NullTime
	err := row.Scan(&img.ImageID, &img.Repository, &img.Tag, &img.Digest, &createdAt, &img.SizeBytes, &img.DiscoveredAt, &lastScanned)
	if err != nil {
		if err == sql.ErrNoRows {
			return result, fmt.Errorf("no image found for asset %s", assetID)
		}
		return result, err
	}
	if createdAt.Valid { img.CreatedAt = createdAt.Time }
	if lastScanned.Valid { img.LastScannedAt = lastScanned.Time }
	result.Image = img

	// Get SBOMs
	sboms, err := p.GetSBOMByImage(ctx, img.ImageID)
	if err != nil { return result, err }
	result.SBOMs = sboms

	// Get Vulnerabilities
	vulns, err := p.GetVulnerabilitiesByImage(ctx, img.ImageID)
	if err != nil { return result, err }
	result.Vulnerabilities = vulns

	// Get Decisions
	rows, err := p.db.QueryContext(ctx, `
		SELECT decision_id, image_id, scan_id, policy_version, result, reason, critical_count, high_count, medium_count, low_count, evaluated_at
		FROM supply_chain_policy_decisions
		WHERE image_id = $1
		ORDER BY evaluated_at DESC LIMIT 5
	`, img.ImageID)
	if err != nil { return result, err }
	defer rows.Close()

	for rows.Next() {
		var d models.PolicyDecision
		if err := rows.Scan(&d.DecisionID, &d.ImageID, &d.ScanID, &d.PolicyVersion, &d.Result, &d.Reason, &d.CriticalCount, &d.HighCount, &d.MediumCount, &d.LowCount, &d.EvaluatedAt); err != nil {
			return result, err
		}
		result.Decisions = append(result.Decisions, d)
	}

	return result, nil
}

func (p *Postgres) InsertUnifiedEvent(ctx context.Context, eventBytes []byte) error {
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

func (p *Postgres) Close() error {
	return p.db.Close()
}
