import os

os.chdir("engines/executor/internal/engine")

file = "executor_integration_test.go"
with open(file, "r") as f:
    content = f.read()

content = content.replace(
    'db, dbErr := sql.Open("postgres", "postgres://postgres:postgres@localhost:5433/zeraxis?sslmode=disable")',
    'db := setupTestDB(t)\n\tvar dbErr error'
)

with open(file, "w") as f:
    f.write(content)

