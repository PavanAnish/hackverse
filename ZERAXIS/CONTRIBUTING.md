# Contributing to ZERAXIS

Thank you for your interest in contributing. ZERAXIS is a security engineering project — please read this guide carefully before submitting changes, especially in security-sensitive areas.

---

## Development Environment
 
### Prerequisites.  


| Requirement | Version | Notes |
|-------------|---------|-------|
| Go | `1.27` | Specified in Dockerfiles; `go.mod` may specify a compatible version |
| Docker | `>= 24.0` | Required for integration tests and full lab |
| Docker Compose | `>= 2.20` | Required for the lab stack |
| PostgreSQL client (`psql`) | `>= 15` | For applying migrations manually |
| NATS CLI (optional) | latest | For inspecting subjects during development |

ZERAXIS does **not** currently use a `Makefile`. All commands are documented below.

---

## Starting the Local Lab

```bash
cd infrastructure/docker
docker-compose up -d postgres nats
```

Wait for PostgreSQL to pass its health check before applying migrations.

### Apply Migrations

Migrations must be applied in dependency order:

```bash
PGURL="postgres://postgres:postgres@localhost:5433/zeraxis?sslmode=disable"

for engine in discovery supply-chain detection runtime correlation shield; do
    for f in engines/$engine/migrations/*.sql; do
        echo "Applying $f..."
        psql "$PGURL" -f "$f"
    done
done
```

> **Note:** The `shield` migrations include `003_override_ledger_complete.sql` and `004_k8s_policy_distribution.sql`. Apply all four in order.

---

## Running Tests

### Important: Schema Isolation

Each engine's test suite drops and recreates the `public` PostgreSQL schema in `setupTestDB`. **Run engines sequentially**, not in parallel, to avoid schema collisions.

### Environment Variables

```bash
export TEST_DATABASE_URL="postgres://postgres:postgres@localhost:5433/zeraxis?sslmode=disable"
export NATS_URL="nats://localhost:4222"
```

### Per-Engine Commands

```bash
# Correlation engine
cd engines/correlation
go test -race -count=1 ./...

# Executor engine
cd engines/executor
go test -race -count=1 ./...

# Detection engine
cd engines/detection
go test -race -count=1 ./...

# Shield engine (run packages individually)
cd engines/shield
go test -race -count=1 ./internal/admission/...
go test -race -count=1 ./internal/cache/...
go test -race -count=1 ./internal/evaluator/...
go test -race -count=1 ./internal/breakglass/...
go test -race -count=1 ./internal/k8s/...        # webhook tests run without Docker/NATS
go test -race -count=1 ./internal/multicluster/... # requires NATS
go test -race -count=1 ./internal/integration/...  # requires Postgres + NATS
```

### Tests That Run Without Any Infrastructure

The following tests do not require PostgreSQL or NATS:

- `engines/shield/internal/k8s/` — `TestWebhookHandler_*` tests (5 tests, use `httptest`)
- `engines/correlation/internal/risk/` — risk scoring unit tests

### Tests That Require PostgreSQL Only

Most of the correlation, executor, detection, breakglass, and evaluator tests.

### Tests That Require Both PostgreSQL and NATS

- `engines/shield/internal/multicluster/`
- `engines/shield/internal/integration/`
- `engines/shield/internal/cache/` (`invalidation_sla_test.go`)
- `engines/executor/internal/engine/` (`executor_integration_test.go`)

### Race Detector

All tests are expected to pass with `-race`. Never submit a PR that introduces a data race.

```bash
go test -race -count=1 ./...
```

---

## Coding Standards

- **Go formatting:** All code must be formatted with `gofmt`. Run `gofmt -w .` before committing.
- **Error handling:** Never silently discard errors in production code paths.
- **Context propagation:** All database and network calls must accept and propagate `context.Context`.
- **Identity model:** Strictly distinguish `AssetID`, `ContainerID`, and `ImageDigest`. Never use one as a proxy for another.
- **Fail-closed:** Any security decision where the evaluator fails must default to blocking/denial.
- **No `log.Fatal` in libraries:** Only use `log.Fatal` in `main()`.

---

## Migration Rules

- Migrations are **forward-only**. Never modify an existing migration file.
- New migrations must be numbered sequentially and named descriptively.
- Never `DROP TABLE` or `DROP COLUMN` in a migration unless it is a schema cleanup migration with an explicit comment.
- Immutability triggers (`BEFORE UPDATE OR DELETE ... RAISE EXCEPTION`) must not be removed or weakened.
- Any new table in the audit or ledger path must have the appropriate immutability trigger applied.

---

## Test Expectations

- Every new feature must have a corresponding test.
- Security-sensitive logic (identity validation, authorization gates, HMAC validation) must have adversarial tests.
- Tests that require infrastructure must check for connectivity and skip gracefully if unavailable (not fail unconditionally).
- Do not count skipped tests as passing.
- Use `t.Skipf(...)` rather than `t.Fatalf(...)` when dependencies are unavailable.

---

## Documentation Requirements

- Any behaviour change must update the relevant section of `README.md`.
- Any new failure mode must be documented in `docs/FAILURE_SEMANTICS.md`.
- Any new trust boundary or attack surface must be reflected in `docs/THREAT_MODEL.md`.
- Do not claim a feature is "autonomous" if it requires human authorization.
- Do not claim "exactly-once delivery" — ZERAXIS uses NATS Core.
- Database table names in documentation must exactly match migration SQL.

---

## Security-Sensitive Changes

Changes to the following areas require extra review and must include adversarial test coverage:

- `engines/executor/internal/engine/` — any change to identity validation or FSM transitions
- `engines/shield/internal/breakglass/` — HMAC logic, threshold enforcement, revocation
- `engines/shield/internal/admission/` — default-deny logic
- `engines/correlation/internal/` — authorization gates, incident lifecycle
- Any migration that modifies immutability triggers or constraint definitions

---

## Pull Request Expectations

1. **One concern per PR.** Feature PRs should not bundle unrelated fixes.
2. **Tests first.** Failing test → implementation → passing test.
3. **Update docs.** If behaviour changes, update `README.md` and relevant `docs/` files.
4. **No secrets.** Run a secrets check before pushing. See `docs/SECURITY.md` for what constitutes a secret.
5. **`gofmt` clean.** CI will reject non-formatted code.
6. **Race detector clean.** All tests must pass with `-race`.
