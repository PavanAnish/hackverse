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
)

// Postgres is the database repository for the runtime engine.
type Postgres struct {
	db *sql.DB
}

// NewPostgres connects to PostgreSQL and runs migrations.
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

// Ping checks database connectivity.
func (p *Postgres) Ping() error {
	return p.db.Ping()
}

// InsertUnifiedEvent persists a unified event to the events table.
func (p *Postgres) InsertUnifiedEvent(ctx context.Context, eventBytes []byte) error {
	var evt struct {
		EventID       string          `json:"event_id"`
		EventType     string          `json:"event_type"`
		EventVersion  int             `json:"event_version"`
		Timestamp     time.Time       `json:"timestamp"`
		Source        string          `json:"source"`
		AssetID       string          `json:"asset_id"`
		ImageDigest   string          `json:"image_digest"`
		Severity      string          `json:"severity"`
		CorrelationID *string         `json:"correlation_id"`
		TraceID       *string         `json:"trace_id"`
		Metadata      json.RawMessage `json:"metadata"`
		Subject       string          `json:"subject"`
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
