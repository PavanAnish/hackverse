package engine

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	_ "github.com/lib/pq"
)

func setupTestDB(t *testing.T) *sql.DB {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		dbURL = os.Getenv("DATABASE_URL")
	}
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:5433/zeraxis?sslmode=disable"
	}
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		t.Skipf("DB not available: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Skipf("DB not reachable: %v", err)
	}

	db.Exec("DROP SCHEMA IF EXISTS public CASCADE")
	db.Exec("CREATE SCHEMA public")
	db.Exec("GRANT ALL ON SCHEMA public TO postgres")

	engines := []string{"discovery", "supply-chain", "runtime", "detection", "correlation"}
	for _, eng := range engines {
		dir := "../../../../engines/" + eng + "/migrations"
		files, err := os.ReadDir(dir)
		if err != nil { continue }
		for _, f := range files {
			if strings.HasSuffix(f.Name(), ".sql") {
				content, err := os.ReadFile(filepath.Join(dir, f.Name()))
				if err != nil { t.Fatalf("failed to read %s: %v", f.Name(), err) }
				if _, err := db.Exec(string(content)); err != nil {
					t.Fatalf("failed to exec %s: %v", f.Name(), err)
				}
			}
		}
	}
	return db
}
