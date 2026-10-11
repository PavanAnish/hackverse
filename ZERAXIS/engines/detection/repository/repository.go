package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"
	"github.com/pavanvanish/zeraxis/engines/detection/models"
)

type Repository struct {
	db *sql.DB
}

func New(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// EnsureSchema runs the initial table migrations.
func (r *Repository) EnsureSchema(ctx context.Context) error {
	// In a real app we'd load the .sql file, here we use a minimal embedded equivalent for testability
	schema := `
CREATE TABLE IF NOT EXISTS findings (
    finding_id      TEXT PRIMARY KEY,
    fingerprint     TEXT NOT NULL UNIQUE,
    rule_id         TEXT NOT NULL,
    rule_version    INTEGER NOT NULL,
    severity        TEXT NOT NULL,
    confidence      TEXT NOT NULL,
    status          TEXT NOT NULL,
    first_seen      TIMESTAMPTZ NOT NULL,
    last_seen       TIMESTAMPTZ NOT NULL,
    asset_id        TEXT,
    image_digest    TEXT,
    evidence        TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_findings_fingerprint ON findings(fingerprint);
CREATE INDEX IF NOT EXISTS idx_findings_asset_status ON findings(asset_id, status);

CREATE TABLE IF NOT EXISTS finding_events (
    finding_id      TEXT NOT NULL REFERENCES findings(finding_id) ON DELETE CASCADE,
    event_id        TEXT NOT NULL,
    PRIMARY KEY (finding_id, event_id)
);
`
	_, err := r.db.ExecContext(ctx, schema)
	return err
}

// UpsertFinding safely creates or updates a finding based on fingerprint.
// Returns the finding ID, and a boolean indicating if it is a NEW finding.
func (r *Repository) UpsertFinding(ctx context.Context, f models.Finding, eventID string) (string, bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return "", false, err
	}
	defer tx.Rollback()

	// 1. Try to insert or update the finding
	query := `
		INSERT INTO findings (
			finding_id, fingerprint, rule_id, rule_version, severity, confidence, 
			status, first_seen, last_seen, asset_id, image_digest, evidence, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, NOW(), NOW()
		)
		ON CONFLICT (fingerprint) DO UPDATE SET
			status = 'UPDATED',
			last_seen = EXCLUDED.last_seen,
			evidence = EXCLUDED.evidence,
			updated_at = NOW()
		RETURNING finding_id, (xmax = 0) AS is_new
	`
	
	findingID := f.FindingID
	if findingID == "" {
		findingID = uuid.New().String()
	}

	var returnedID string
	var isNew bool

	err = tx.QueryRowContext(ctx, query,
		findingID, f.Fingerprint, f.RuleID, f.RuleVersion, f.Severity, f.Confidence,
		"OPEN", f.FirstSeen, f.LastSeen, f.AssetID, f.ImageDigest, f.Evidence,
	).Scan(&returnedID, &isNew)

	if err != nil {
		return "", false, fmt.Errorf("upsert finding: %w", err)
	}

	// 2. Insert into finding_events safely
	eventQuery := `
		INSERT INTO finding_events (finding_id, event_id)
		VALUES ($1, $2)
		ON CONFLICT DO NOTHING
	`
	_, err = tx.ExecContext(ctx, eventQuery, returnedID, eventID)
	if err != nil {
		return "", false, fmt.Errorf("link event: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return "", false, err
	}

	return returnedID, isNew, nil
}
