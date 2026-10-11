package repository

import (
	"database/sql"
	"testing"
	_ "github.com/lib/pq"
)

func SetupTestDB(t *testing.T) (*Repository, error) {
	dbURL := "postgres://postgres:postgres@localhost:5433/zeraxis?sslmode=disable"
	db, err := sql.Open("postgres", dbURL)
	if err == nil {
		_, _ = db.Exec("DROP SCHEMA public CASCADE; CREATE SCHEMA public;")
		db.Close()
	}
	return New(dbURL, "../../migrations")
}
