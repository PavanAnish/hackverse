# ZERAXIS

> A closed-loop cyber defense and security automation platform combining asset discovery, software supply-chain intelligence, runtime telemetry, deterministic risk scoring, attack-chain analysis, controlled execution, post-execution verification, and policy-based admission control — with explicit authorization boundaries and human-attestation support for sensitive operations.

[![License](https://img.shields.io/badge/license-Apache%202.0-blue)](LICENSE)
[![Go](https://img.shields.io/badge/go-1.27-00ADD8)](/)
[![Status](https://img.shields.io/badge/status-v0.1.1--alpha-orange)](/)

> **Project Status:** v0.1.1-alpha — Engineering / security research prototype. Not a commercially supported product. Review [SECURITY.md](SECURITY.md) and [docs/LIMITATIONS.md](docs/LIMITATIONS.md) before deploying in any environment.

---

## Operator Quickstart Guide

This guide walks you through operating ZERAXIS locally to observe the full closed-loop defense lifecycle.

### 1. Configure the Environment
ZERAXIS requires secrets to be explicitly provided. Copy the template and edit it:
```bash
cp .env.example .env
# Edit .env and set secure values for POSTGRES_PASSWORD and API_INTERNAL_SECRET
```

### 2. Start the Environment
Boot the complete platform (PostgreSQL, NATS, Falco, API Gateway, and all Engines):
```bash
cd infrastructure/docker
docker compose up --build -d
```
*Note: The `db-migrate` service runs automatically to initialize the database schema before application engines boot.*

### 3. Simulate a Threat
Trigger suspicious activity inside one of the monitored containers. For example, touch a sensitive file in the Postgres container (which Falco monitors):
```bash
docker exec docker-postgres-1 touch /tmp/e2e-regression-test
```

### 4. Observe the Incident
Check the Correlation Engine logs. You will see ZERAXIS correlate the Falco runtime finding into a new incident, calculate risk, and propose a mitigation recommendation:
```bash
docker compose logs -f correlation-engine
# Look for: "New incident created" and "recommendation_id"
```

### 5. Approve the Mitigation
ZERAXIS requires human authorization before mutating infrastructure. Copy the `recommendation_id` from the correlation logs, and approve it via the API Gateway using the break-glass administrative credentials:
```bash
curl -s -X POST http://localhost:8080/api/v1/recommendations/<RECOMMENDATION_ID>/approve -u admin:admin
```
*A successful approval returns an HTTP 200 OK with a generated `execution_id`.*

### 6. Verify Execution
The Executor Engine validates the risk level, the image digest, and the container identity before carrying out the action. Verify the result in the Executor logs:
```bash
docker compose logs -f executor-engine
# Look for: "DENY: current risk level is insufficient" or "Execution result recorded"
```


### 7. Operational Guidance & Troubleshooting

**Database State Inspection:**
To see exactly how the state machine tracks incidents and executions, you can query the PostgreSQL database directly:
```bash
docker exec -it docker-postgres-1 psql -U postgres -d zeraxis
```
Helpful queries:
- `SELECT * FROM incidents ORDER BY created_at DESC;`
- `SELECT recommendation_id, status FROM incident_recommendations;`
- `SELECT * FROM execution_state_transitions;` (This is the append-only audit ledger tracking mitigation progress).

**Understanding the Execution Boundary (Risk Thresholds):**
If you see `DENY: current risk level is insufficient for mutation` in the Executor logs, the system is working as designed. ZERAXIS prevents potentially destructive container mutations (like stopping a production container) unless the aggregate risk score justifies the action. For local simulation, touching a file triggers a `LOW` risk incident, which correctly halts at the execution gate even after human approval.

**Authentication & The API Gateway:**
In a production deployment, direct access to the correlation engine is blocked. All operator approvals MUST flow through the API Gateway (port 8080), which authenticates the user and safely injects an internal cryptographic secret (`X-Internal-Secret`) into the upstream request.

**Resetting the Lab Environment:**
To clear all NATS events, PostgreSQL databases, and incidents to start fresh:
```bash
docker compose down -v
docker compose up --build -d
```

---

## What ZERAXIS Does

ZERAXIS continuously monitors running container infrastructure for security threats and enforces admission controls to prevent known-malicious images from starting. It operates across two complementary paths:

**Reactive path:** When a vulnerability or runtime anomaly is detected, ZERAXIS correlates findings into an incident, scores risk deterministically, generates a remediation recommendation, gates execution through an authorization control, and verifies the postcondition. Human authorization is required before any disruptive Docker action occurs.

**Proactive path:** Before any container starts (Docker or Kubernetes), ZERAXIS evaluates the image's OCI digest against a policy and a cache of known-blocked fingerprints. This evaluation is fail-closed — an evaluation failure blocks the request.

ZERAXIS does **not** claim to be fully autonomous. Detection, correlation, risk calculation, and recommendation generation are automated. Disruptive remediation actions require explicit authorization. Human investigation paths and manual remediation attestations are first-class concepts in the data model.

---

## Identity Model

Three distinct identity concepts are used throughout. **They must not be confused.**

| Identity | Type | Derives From | Authoritative For |
|----------|------|-------------|------------------|
| **AssetID** | Logical asset identity | `UUID(SHA-256("zeraxis:asset:" + containerName))` | Stable identity across container recreation |
| **ContainerID** | Runtime lifecycle identity | Docker-assigned container ID (12-char prefix) | Lifecycle-specific correlation and execution targeting |
| **ImageDigest** | Supply-chain identity | OCI manifest digest: `sha256:<64 lowercase hex>` | Admission decisions, vulnerability mapping, executor revalidation |

Image tags and image names are **not authoritative**. Any admission, block, recommendation, or execution record references `ImageDigest` — not a tag or name.

---

## Table of Contents

- [Architecture](#architecture)
- [The Two Paths](#the-two-paths)
- [Repository Structure](#repository-structure)
- [Engines](#engines)
- [Risk Model](#risk-model)
- [Execution Authorization FSM](#execution-authorization-fsm)
- [Event Backbone (NATS)](#event-backbone-nats)
- [Database Schema](#database-schema)
- [Test Coverage](#test-coverage)
- [Project Status and Phase Table](#project-status-and-phase-table)
- [Quick Start](#quick-start)
- [Running Tests](#running-tests)
- [Environment Variables](#environment-variables)
- [Port Reference](#port-reference)
- [Security Architecture](#security-architecture)
- [Known Limitations](#known-limitations)
- [Future Roadmap](#future-roadmap)
- [Documentation](#documentation)

---

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                        REACTIVE PATH                            │
│                                                                 │
│  Discovery ──▶ Supply-Chain ──▶ Detection ──▶ Correlation      │
│  (AssetID)     (ImageDigest)    (Fingerprint)  (Incident/Risk) │
│                                                     │           │
│              ┌──────────────────────────────────────▼───────┐  │
│              │  NATS Event Backbone                         │  │
│              │  zeraxis.events.<source>.<action>            │  │
│              └──────────────────────────────────────┬───────┘  │
│                                                     │           │
│  ┌──────────────────────────────────────────────────▼───────┐  │
│  │  Recommendation ──▶ [AUTHORIZATION GATE] ──▶ Executor   │  │
│  │                           │                    │         │  │
│  │              Human Authorization      Docker SDK Mutation│  │
│  │              or Manual Attestation    (fail-closed,      │  │
│  │                                        re-validates      │  │
│  │                                        identity)         │  │
│  └─────────────────────────────────────────────────┬────────┘  │
│                                                     │           │
│                              Execution Audit Ledger (append-only│
│                              execution_state_transitions)       │
└─────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────┐
│                        PROACTIVE PATH                           │
│                                                                 │
│  incident.resolved ──▶ NATS ──▶ Shield Cache (5min TTL)       │
│                                        │                        │
│                                   OPA Evaluator                 │
│                                   (Rego policy)                 │
│                                        │                        │
│                    ┌───────────────────┼────────────────┐      │
│                    ▼                   ▼                ▼       │
│            Docker AuthZ       K8s Webhook        Multi-Cluster  │
│            Plugin             (fail-closed,      NATS Coherency │
│            (blocks run)       2s timeout)        (wildcard sub) │
└─────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────┐
│                       EMERGENCY PATH                            │
│                                                                 │
│  Break-Glass Override (M-of-2 HMAC) ──▶ Elevation Detector    │
│  PENDING ──▶ APPROVED ──▶ EXECUTED     (≥3/hour → POLICY_ABUSE│
│           └──▶ EXPIRED (1h)             incident + user block) │
│                                                                 │
│  Policy Distributor: Canary Rollout (10% → 50% → 100%)        │
│  Immutable K8s ConfigMaps per cluster                          │
└─────────────────────────────────────────────────────────────────┘
```

---

## The Two Paths

### Reactive Path

| Step | Engine | What Happens | Authorization? |
|------|--------|-------------|---------------|
| 1 | Discovery | Scans Docker API (read-only) every 30s; derives stable `AssetID` from container name | No |
| 2 | Supply-Chain | Fetches SBOM, runs Trivy scan; records CVEs keyed by `ImageDigest` | No |
| 3 | Runtime (Falco) | Monitors kernel syscalls via eBPF; emits normalized events | No |
| 4 | Detection | Correlates runtime events with supply-chain data; generates findings with deterministic fingerprints | No |
| 5 | Correlation | Groups findings into incidents; calculates risk score; generates remediation recommendations | No |
| 6 | **Authorization** | Recommendation requires `APPROVED` authorization before execution | **Yes — explicit** |
| 7 | Executor | Re-validates `AssetID` + `ContainerID` + `ImageDigest` against live Docker state; performs mutation | Only after step 6 |
| 8 | Verification | Calls `ContainerInspect` to verify security postcondition; read-only | No |
| 9 | Audit Ledger | All FSM transitions written to append-only `execution_state_transitions` | Automatic |

### Proactive Path

| Step | Component | What Happens |
|------|-----------|-------------|
| 1 | Shield Cache | Materialized view of blocked `ImageDigest` fingerprints, invalidated via NATS |
| 2 | OPA Evaluator | Rego policy evaluation against active policy version |
| 3 | Docker AuthZ Plugin | Intercepts `docker run`; blocks before container starts |
| 4 | K8s ValidatingWebhook | Intercepts Pod CREATE/UPDATE; fail-closed with 2s timeout |
| 5 | Multi-Cluster Sync | Publishes resolution events; other clusters invalidate their caches |

---

## Repository Structure

```
ZERAXIS/
├── engines/
│   ├── correlation/          # Incident lifecycle, risk model, recommendations, authorizations
│   │   ├── internal/
│   │   │   ├── api/          # HTTP API server
│   │   │   ├── correlator/   # NATS event consumer, incident grouping
│   │   │   ├── models/       # Data models (Incident, Recommendation, FindingRef…)
│   │   │   ├── recommender/  # Recommendation generation + recovery path
│   │   │   ├── repository/   # PostgreSQL data layer
│   │   │   └── risk/         # Risk scoring engine (Model v1.0)
│   │   ├── migrations/       # 013 migrations (001–013)
│   │   └── pkg/approvals/    # Authorization workflow (approve, revoke, human attestation)
│   │
│   ├── detection/            # Finding detection + temporal correlation
│   │   ├── context/          # Container context resolver (asset ↔ vulnerability matching)
│   │   ├── migrations/
│   │   └── rules/rules.yaml  # Detection rule definitions
│   │
│   ├── discovery/            # Container asset inventory via Docker API
│   │   ├── internal/
│   │   │   ├── discovery/    # Scanner, reconciler, loop
│   │   │   ├── models/       # Asset, AssetPort, AssetNetwork, Relationship
│   │   │   └── repository/   # PostgreSQL data layer
│   │   └── migrations/
│   │
│   ├── executor/             # Authoritative Docker action execution
│   │   ├── internal/
│   │   │   ├── docker/       # Docker SDK wrapper (ContainerStop, NetworkDisconnect)
│   │   │   └── engine/       # FSM, identity revalidation, ledger writes
│   │   └── Dockerfile
│   │
│   ├── runtime/              # Falco JSON event consumer + normalizer
│   │   ├── falco/            # Falco config (zeraxis_config.yaml, rules.local.yaml)
│   │   └── migrations/
│   │
│   ├── shield/               # Proactive admission control (Phases 8–9)
│   │   ├── internal/
│   │   │   ├── admission/    # POST /v1/admit handler
│   │   │   ├── breakglass/   # Override FSM + HMAC validation + elevation detector
│   │   │   ├── cache/        # Materialized cache + NATS invalidation + singleflight
│   │   │   ├── docker/       # Docker AuthZ plugin
│   │   │   ├── evaluator/    # OPA/Rego policy evaluation
│   │   │   ├── integration/  # Phase 9 E2E integration test
│   │   │   ├── k8s/          # ValidatingWebhook + PolicyDistributor + RolloutManager
│   │   │   └── multicluster/ # NATSCoherencyManager (cross-cluster invalidation)
│   │   ├── migrations/       # 4 migrations (001–004)
│   │   └── go.mod            # Shield has its own Go module
│   │
│   └── supply-chain/         # SBOM generation + Trivy vulnerability scanning
│       ├── internal/scanner/
│       └── migrations/
│
├── infrastructure/
│   └── docker/
│       ├── docker-compose.yml     # Full platform stack
│       └── postgres/init.sql
│
├── pkg/
│   └── events/                    # Shared: event types, NATS subjects, publisher
│       ├── event.go               # Envelope type
│       ├── types.go               # EventType constants + GenerateSubject()
│       ├── admission_events.go    # Shield-specific event types
│       ├── override_events.go     # Break-glass + distribution event types
│       └── publisher.go           # NATS publisher wrapper
│
├── services/                      # Demo target microservices (monitored by ZERAXIS)
│   ├── auth/                      # Auth service — returns mock token (demo only)
│   ├── gateway/                   # Reverse proxy
│   ├── payment/                   # Payment service
│   └── product/                   # Product service
│
├── docs/
│   ├── THREAT_MODEL.md
│   ├── FAILURE_SEMANTICS.md
│   └── LIMITATIONS.md
│
├── LICENSE
├── SECURITY.md
├── CONTRIBUTING.md
└── README.md
```

---

## Engines

### 1. Discovery Engine (Phase 2) — Port 8090

Polls the Docker API (read-only socket mount) every 30 seconds and maintains a live inventory of containers.

**AssetID derivation:** `UUID(SHA-256("zeraxis:asset:" + containerName))` — stable across container recreation because it is derived from the container name, not the container ID.

**Key tables:** `discovery_assets`, `discovery_asset_networks`, `discovery_asset_ports`, `discovery_relationships`

**Note:** The Discovery Engine has no dedicated unit test files. It is exercised through integration testing in downstream engines.

---

### 2. Supply-Chain Engine (Phase 3) — Port 8091

Scans images discovered by the Discovery Engine. Generates SBOMs and runs Trivy vulnerability analysis. Records all findings keyed by `ImageDigest`.

**Identity enforcement:** Migration `002_cleanup_mutable_identities.sql` removes any records using mutable tag-based identity. Only `sha256:<hash>` digests are authoritative.

**Key tables:** `supply_chain_images`, `supply_chain_vulnerabilities`, `supply_chain_sboms`, `supply_chain_scans`, `supply_chain_packages`

**Dependency:** Requires Trivy to be available in the engine container.

**Note:** The Supply-Chain Engine has no dedicated unit test files.

---

### 3. Runtime Engine + Falco (Phase 5) — Port 8092

Falco monitors container syscalls via eBPF. The Runtime Engine tails Falco's JSONL output, resolves container identity via the Discovery API, and writes normalized events to PostgreSQL + NATS.

**Falco capabilities:** `SYS_ADMIN`, `BPF`, `PERFMON`, `SYS_RESOURCE`, `SYS_PTRACE` — explicit grants, no `privileged: true`.

**Shared volume:** `falco_events:/var/run/falco` — Falco writes, Runtime Engine reads.

**Note:** The Runtime Engine has no dedicated unit test files.

---

### 4. Detection Engine (Phase 6) — Port 8093

Consumes runtime events from NATS, correlates with supply-chain vulnerability data, and generates findings. Applies rule-based temporal analysis (correlation windows, sequence detection).

**Fingerprint:** Deterministically derived from `(rule_id, rule_version, asset_id, image_digest, time_bucket)`. Callers cannot inject an arbitrary fingerprint.

**Rules file:** `engines/detection/rules/rules.yaml`

---

### 5. Correlation Engine (Phase 6)

The central state machine. Groups findings into incidents, computes risk scores, generates recommendations, manages the authorization workflow, and drives the execution lifecycle.

**Key packages:**
- `correlator` — NATS consumer, incident fingerprint grouping
- `risk` — Risk Model v1.0 (see [Risk Model](#risk-model))
- `recommender` — Recommendation generation + recovery path
- `repository` — PostgreSQL data layer with 13 migrations
- `pkg/approvals` — Authorization workflow: approve, revoke, human investigation, attestation

---

### 6. Executor Engine (Phase 7)

The only engine with write access to the Docker socket. Executes remediation actions **only** after authorization and identity revalidation.

**Actions supported:** `STOP_CONTAINER`, `QUARANTINE_ASSET` (network isolation)

**Identity revalidation (before every Docker mutation):**
1. Live container found for `target_container_id`
2. `target_container_id` matches live Docker inspect
3. `target_image_digest` found in live container's `RepoDigests` (canonical `sha256:` form)
4. `ImageID` values explicitly rejected as non-authoritative

See [Execution Authorization FSM](#execution-authorization-fsm).

---

### 7. Shield Engine (Phase 8) — Ports 8094, 8095

**Admission Handler (`/v1/admit`):** Evaluates image digest requests against cache + OPA. 50ms hard timeout. Fail-closed.

**Cache:** In-process materialized view. 5-minute TTL. Singleflight coalescing (100 concurrent requests for the same fingerprint hit the DB once). Invalidated via NATS `zeraxis.events.*.correlation.incident.resolved`.

**OPA Evaluator:** Rego policy evaluation. Policy versioning FSM: `DRAFT → STAGING → ACTIVE → DEPRECATED → RETIRED`. Exactly one `ACTIVE` policy at any time.

**Docker AuthZ Plugin:** Intercepts `docker run` before container starts. Calls admission handler. Blocks if evaluation fails for any reason.

---

### 8. Break-Glass Override Service (Phase 9)

Emergency admission bypass requiring M-of-2 HMAC-SHA256 signatures.

**FSM:** `PENDING → APPROVED → EXECUTED`, or `EXPIRED` after 1 hour.

**Elevation Detector:** ≥3 override requests per user per rolling 1-hour window → automatic `POLICY_ABUSE` incident (severity `CRITICAL`) + user block (`ON CONFLICT DO NOTHING` ensures concurrency safety).

**HMAC:** `HMAC-SHA256(requestID|approverID, SHIELD_HMAC_SECRET)`

**Audit:** All transitions written to `policy_audit_trail` (append-only).

---

### 9. K8s Webhook + Policy Distributor (Phase 9) — Port 8096

**ValidatingWebhook:** Intercepts Pod `CREATE`/`UPDATE` cluster-wide. Evaluates all containers (including init and ephemeral). `failurePolicy: Fail`, `timeoutSeconds: 2`.

**PolicyDistributor:** Pushes OPA policies to K8s clusters as immutable ConfigMaps (`Immutable: true`). Canary rollout: 10% → 50% → 100% (deterministic by sorted cluster name).

---

### 10. Multi-Cluster NATS Coherency (Phase 9)

**Subscribe:** `zeraxis.events.*.correlation.incident.resolved`  
**Publish:** `zeraxis.events.<own_cluster_id>.correlation.incident.resolved`  
**Loopback filter:** Events from own cluster are ignored.  
**Startup sync:** `nc.Flush()` after subscribe prevents delivery race.

**Limitation:** NATS Core — events missed during partition are permanently lost. See [docs/LIMITATIONS.md](docs/LIMITATIONS.md).

---

## Risk Model

**Model version:** 1.0 (`const ModelVersion = "1.0"` in `engines/correlation/internal/risk/risk.go`)

### Static Risk

```
baseScore:
  CRITICAL → 80
  HIGH     → 60
  MEDIUM   → 40
  LOW      → 20
  INFO / UNKNOWN → 0

PolicyBlockModifier: if policy == "BLOCK" → +20

StaticRisk = min(100, baseScore + PolicyBlockModifier)
```

### Dynamic Risk

```
baseDynamic = baseSeverityScore
              × TemporalDensityMultiplier
              × AttackChainModifier

TemporalDensityMultiplier = 1.0 + (min(20, findingCount) × 0.05)
  [range: 1.0x to 2.0x]

AttackChainModifier:
  IMPACT           → 2.0×
  NETWORK_ACTIVITY → 1.5×
  EXECUTION        → 1.25×
  EXPOSURE/UNKNOWN → 1.0×

Capped at 100.

DynamicRisk = decayed(baseDynamic):
  silence = hours since lastSeen
  if silence >= 24h → DynamicRisk = 0
  else → DynamicRisk = round(baseDynamic × (1 - silence/24))
```

### Combined Risk

```
CombinedRisk = round(min(100, StaticRisk × 0.4 + DynamicRisk × 0.6))
```

### Risk Levels

| Score | Level |
|-------|-------|
| 0–19 | INFO |
| 20–39 | LOW |
| 40–69 | MEDIUM |
| 70–89 | HIGH |
| 90–100 | CRITICAL |

### Notes

- Confidence is calculated separately from risk (based on evidence strength: STRONG/MEDIUM/WEAK/UNKNOWN)
- `UNKNOWN` supply-chain context does **not** mean SAFE — `baseScore = 0` for `UNKNOWN` severity, but the incident still exists
- Dynamic risk decays to 0 after 24 hours of silence
- No AI or LLM is involved in risk calculation — it is entirely deterministic

### Attack Chain States

`UNKNOWN → EXPOSURE → EXECUTION → NETWORK_ACTIVITY → IMPACT`

The highest state with evidence wins. Detection rule IDs are mapped to states in the correlator.

---

## Execution Authorization FSM

```
Correlation Engine:
  [recommendation PROPOSED]
       │
       ▼
  [APPROVED by authorizer]
       │
       ├─── expired (> 1h) ──▶ FAILED (no mutation)
       ├─── incident RESOLVED ──▶ FAILED (no mutation)
       ├─── ContainerID mismatch ──▶ FAILED (no mutation)
       ├─── ImageDigest mismatch ──▶ FAILED (no mutation)
       ▼
  EXECUTING (claimed by Executor, TX1 commit)
       │
       ├─── Docker mutation fails ──▶ FAILED
       │                              (recovery recommendation generated)
       ▼
  EXECUTED
       │
       ▼
  [Verification: ContainerInspect]
       ├─── postcondition satisfied ──▶ verification_status = VERIFIED
       └─── postcondition not met  ──▶ verification_status = FAILED
                                       (recovery recommendation generated)

Human paths (Correlation Engine / Approvals):
  MANUAL_INVESTIGATION_REQUIRED
       │
       ├─── operator submits attestation (OUTCOME_MANUALLY_REMEDIATED)
       │         → human_remediation_attestations record created
       │         → recommendation status → MANUALLY_REMEDIATED
       │         → incident → RESOLVED
       │
       └─── operator submits attestation (OUTCOME_SAFE_TO_RETRY)
                 → fresh recommendation created (new recommendation_id)
                 → new authorization required
```

**All FSM transitions are written to `execution_state_transitions` (append-only, `BEFORE UPDATE OR DELETE` trigger raises exception).**

---

## Event Backbone (NATS)

ZERAXIS uses **NATS Core** (fire-and-forget). Database writes happen **before** NATS publish. NATS publish failure does not roll back the database. See [docs/FAILURE_SEMANTICS.md](docs/FAILURE_SEMANTICS.md).

### Subject Convention

```
zeraxis.events.<source>.<action>
```

### Key Subjects

| Subject | Publisher | Subscriber(s) |
|---------|-----------|--------------|
| `zeraxis.events.discovery.asset.discovered` | Discovery | Supply-Chain |
| `zeraxis.events.supplychain.vulnerability.found` | Supply-Chain | Detection |
| `zeraxis.events.detection.finding.created` | Detection | Correlation |
| `zeraxis.events.correlation.incident.resolved` | Correlation | Shield Cache |
| `zeraxis.events.<cluster_id>.correlation.incident.resolved` | Shield (per cluster) | All other clusters |
| `zeraxis.events.correlation.incident.policy_abuse` | Elevation Detector | Correlation Engine |

### Subject Generation

```go
// pkg/events/types.go
func GenerateSubject(source, eventType string) string {
    domain := strings.ToLower(strings.ReplaceAll(source, "-", ""))
    action := strings.ToLower(strings.ReplaceAll(eventType, "_", "."))
    return fmt.Sprintf("zeraxis.events.%s.%s", domain, action)
}
```

---

## Database Schema

All state is stored in a single PostgreSQL database. Tables are append-only where noted.

### Discovery (2 migrations)

| Table | Description |
|-------|-------------|
| `discovery_assets` | Container inventory (AssetID, ContainerID, ImageDigest, status) |
| `discovery_asset_networks` | Container network memberships |
| `discovery_asset_ports` | Container port mappings |
| `discovery_relationships` | Container-to-container network relationships |
| `discovery_events` | Discovery event log |

### Supply-Chain (3 migrations)

| Table | Description |
|-------|-------------|
| `supply_chain_images` | Scanned images (keyed by ImageDigest) |
| `supply_chain_vulnerabilities` | CVEs per image (severity, CVSS, package) |
| `supply_chain_sboms` | SBOM records |
| `supply_chain_scans` | Scan history |
| `supply_chain_packages` | Package inventory |
| `supply_chain_asset_images` | Asset ↔ image mapping |

### Detection (1 migration)

| Table | Description |
|-------|-------------|
| `findings` | Detected security findings |
| `finding_events` | Events that contributed to each finding |

### Correlation (13 migrations)

| Table | Description | Immutable? |
|-------|-------------|-----------|
| `incidents` | Core incident records (fingerprint, status, risk scores) | No |
| `incident_findings` | Many-to-many: incidents ↔ findings | No |
| `incident_recommendations` | Remediation recommendations (status FSM: PROPOSED → MANUALLY_REMEDIATED) | No |
| `execution_authorizations` | Approved execution requests | No |
| `execution_results` | Executor action outcomes + verification status | No |
| `execution_state_transitions` | FSM transition ledger | **Yes — append-only trigger** |
| `human_remediation_attestations` | Operator-submitted manual remediation records (Phase 7.8) | No |

> **About migration 013:** `013_human_attestations.sql` creates the `human_remediation_attestations` table for Phase 7.8 human investigation/manual remediation workflows. This is **not** the Phase 9 M-of-N break-glass system. The break-glass schema is in `engines/shield/migrations/003_override_ledger_complete.sql`.

### Shield (4 migrations)

| Table | Description | Immutable? |
|-------|-------------|-----------|
| `policy_definitions` | OPA Rego policies (versioning FSM) | No |
| `admission_requests` | Admission decision log | No |
| `shield_cache_state` | Materialized cache backing store | No |
| `override_requests` | Break-glass override requests (Phase 9) | No |
| `override_approvals` | Per-approver HMAC signatures | **Yes — append-only trigger** (exception: `revoked_at` may be set once) |
| `policy_audit_trail` | All Shield events log | **Yes — append-only trigger** |
| `override_ledger` | Override execution history | **Yes — append-only trigger** |
| `k8s_policy_snapshots` | Policy versions for K8s distribution | No |
| `k8s_cluster_policy_state` | Per-cluster current + previous snapshot | No |
| `k8s_webhook_registrations` | Registered webhook endpoints | No |

### Append-Only Trigger Pattern

```sql
CREATE FUNCTION prevent_modification() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'This table is append-only. Updates and deletes are prohibited.';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER enforce_append_only
    BEFORE UPDATE OR DELETE ON execution_state_transitions
    FOR EACH ROW EXECUTE FUNCTION prevent_modification();
```

**Exception:** `override_approvals.revoked_at` may be updated exactly once (from `NULL` to a timestamp) when an approver revokes their signature before the threshold is met. This is the only permitted UPDATE on any of the append-only tables.

---

## Test Coverage

**Total test functions:** 183 (`func Test...` across all `_test.go` files in `engines/`)

| Engine | Test Files | Approx. Tests | Infrastructure Needed |
|--------|-----------|--------------|----------------------|
| Correlation | 9 files | ~82 | PostgreSQL + NATS |
| Shield | 11 files | ~51 | Varies (see below) |
| Executor | 9 files | ~39 | PostgreSQL + Docker |
| Detection | 4 files | ~11 | PostgreSQL |
| Discovery | 0 files | 0 | — (no unit tests) |
| Supply-Chain | 0 files | 0 | — (no unit tests) |
| Runtime | 0 files | 0 | — (no unit tests) |

### Tests by Category

**No infrastructure required (run anywhere):**
- `engines/shield/internal/k8s/` — `TestWebhookHandler_*` (5 tests, use `net/http/httptest`)
- `engines/correlation/internal/risk/` — risk scoring unit tests (pure computation)
- `engines/executor/internal/engine/digest_test.go` — digest validation unit tests

**Requires PostgreSQL only:**
- Most of `engines/correlation/`, `engines/executor/`, `engines/shield/internal/evaluator/`, `engines/shield/internal/breakglass/`

**Requires PostgreSQL + NATS:**
- `engines/shield/internal/multicluster/`
- `engines/shield/internal/integration/` (Phase 9 E2E)
- `engines/shield/internal/cache/invalidation_sla_test.go`
- `engines/executor/internal/engine/executor_integration_test.go`

**Requires Docker socket + PostgreSQL + NATS:**
- `engines/executor/internal/engine/executor_integration_test.go` (`TestIntegration_DockerNATSExecution`)

### Verification Status

| Category | Status |
|----------|--------|
| Pure unit tests (no infra) | ✓ Verified — pass without any infrastructure |
| DB-only integration tests | Verified when Postgres is running (lab environment) |
| DB + NATS integration tests | Verified when both are running (lab environment) |
| Docker-dependent tests | Verified in the lab environment with Docker running |
| Runtime/Discovery/Supply-Chain | **Not unit tested** — no test files exist for these engines |

Tests that require infrastructure are written to `t.Skipf(...)` when the dependency is unavailable, not `t.Fatalf(...)`. Skipped tests are not counted as passing.

---

## Project Status and Phase Table

| Phase | Name | Implementation | Unit Tested | Runtime Verified | Key Deliverable |
|-------|------|---------------|-------------|-----------------|-----------------|
| 1 | Local Lab | ✓ Complete | N/A | ✓ Verified | Docker Compose, Postgres, NATS stack |
| 2 | Discovery Engine | ✓ Complete | ✗ No unit tests | Verified (lab) | Asset inventory, AssetID derivation |
| 3 | Supply-Chain Engine | ✓ Complete | ✗ No unit tests | Verified (lab) | SBOM + Trivy CVE scanning |
| 4 | Event Backbone | ✓ Complete | Partial | Verified (lab) | NATS subjects, unified envelope |
| 5 | Runtime Engine + Falco | ✓ Complete | ✗ No unit tests | Verified (lab) | eBPF syscall monitoring |
| 6 | Detection + Correlation | ✓ Complete | ✓ Unit tested | Verified (lab) | Risk Model v1.0, incident grouping |
| 7 | Execution Boundary | ✓ Complete | ✓ Extensively tested | Verified (lab) | Authorized execution, human attestation, ledger |
| 8 | Shield (Proactive) | ✓ Complete | ✓ Unit tested | Verified (lab) | Admission cache, OPA, Docker AuthZ |
| 9 | K8s + Break-Glass + Multi-Cluster | ✓ Complete | ✓ Unit + E2E tested | Verified (lab) | Webhook, canary rollout, coherency |

**Overall:** All 9 phases are implemented. Unit test coverage exists for Phases 6–9. Phases 2, 3, and 5 (Discovery, Supply-Chain, Runtime) have no unit test files.

---

## Quick Start

### Prerequisites

```
Docker >= 24.0
Docker Compose >= 2.20
Go 1.27 (for local development)
```

> **Note:** Falco with eBPF is only supported on Linux. On macOS with Docker Desktop, Falco runs inside the LinuxKit VM. Some eBPF capabilities may behave differently than on native Linux.

### 1. Start Infrastructure

```bash
cd infrastructure/docker
docker-compose up -d postgres nats
```

Wait for PostgreSQL and NATS to initialize:
```bash
docker compose up -d postgres nats
```

### 2. Start ZERAXIS and Initialize Database

```bash
cd infrastructure/docker
docker compose up --build -d
```

> **Note:** The `db-migrate` service runs automatically on startup. Application engines will wait for successful schema migration before booting.

### 3. Register K8s ValidatingWebhook (K8s only)

```yaml
apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingWebhookConfiguration
metadata:
  name: zeraxis-shield
webhooks:
  - name: shield.zeraxis.io
    admissionReviewVersions: ["v1"]
    sideEffects: None
    rules:
      - apiGroups: [""]
        apiVersions: ["v1"]
        operations: ["CREATE", "UPDATE"]
        resources: ["pods"]
    clientConfig:
      service:
        name: zeraxis-shield
        namespace: zeraxis
        path: /v1/k8s/validate
    failurePolicy: Fail
    timeoutSeconds: 2
```

> **Note:** TLS must be configured between the Kubernetes API server and the webhook service. This repository does not include certificate provisioning. Use cert-manager or equivalent.

---

## Running Tests

> **Important:** Each engine drops and recreates the `public` PostgreSQL schema in `setupTestDB`. Run packages individually, not with `go test ./...` across engines simultaneously.

```bash
export TEST_DATABASE_URL="postgres://postgres:postgres@localhost:5433/zeraxis?sslmode=disable"
export NATS_URL="nats://localhost:4222"

# Tests that require no infrastructure
cd engines/shield
go test -race -count=1 ./internal/k8s/ -run TestWebhookHandler

# Correlation (requires Postgres + NATS)
cd engines/correlation
go test -race -count=1 ./internal/risk/...  # no infra needed
go test -race -count=1 ./internal/correlator/...
go test -race -count=1 ./pkg/approvals/...

# Executor (requires Postgres; Docker for integration test)
cd engines/executor
go test -race -count=1 ./internal/engine/...

# Shield packages (sequential — each drops public schema)
cd engines/shield
go test -race -count=1 ./internal/admission/...
go test -race -count=1 ./internal/cache/...
go test -race -count=1 ./internal/evaluator/...
go test -race -count=1 ./internal/breakglass/...
go test -race -count=1 ./internal/k8s/...
go test -race -count=1 ./internal/multicluster/...  # requires NATS
go test -race -count=1 ./internal/integration/...   # requires Postgres + NATS
```

---

## Environment Variables

| Variable | Default | Required By |
|----------|---------|------------|
| `DATABASE_URL` | — | All engines |
| `TEST_DATABASE_URL` | — | All test suites |
| `NATS_URL` | `nats://nats:4222` | Shield, Correlation, Runtime, Detection, Executor |
| `SHIELD_HMAC_SECRET` | — | Break-Glass service (must match across all Shield replicas) |
| `CLUSTER_ID` | — | Multi-Cluster Coherency Manager |
| `DISCOVERY_INTERVAL` | `30s` | Discovery Engine |
| `SCAN_INTERVAL` | `1h` | Supply-Chain Engine |
| `DISCOVERY_API_URL` | `http://discovery-engine:8090` | Supply-Chain, Runtime |
| `SUPPLY_CHAIN_API_URL` | `http://supply-chain-engine:8091` | Runtime |
| `FALCO_EVENTS_PATH` | `/var/run/falco/events.jsonl` | Runtime Engine |
| `API_PORT` | varies | All HTTP engines |
| `REGISTRY_URL` | `registry:5000` | Supply-Chain Engine |

> **Security:** Never hardcode `SHIELD_HMAC_SECRET` or database credentials. Use a secrets manager (Vault, AWS Secrets Manager, K8s Secret).

---

## Port Reference

| Port | Service | Notes |
|------|---------|-------|
| `5433` | PostgreSQL | Exposed for local debugging (internal: `5432`) |
| `4222` | NATS client | For tests and local tooling |
| `8222` | NATS monitor | Monitoring UI (debug only) |
| `8080` | API Gateway | Demo services reverse proxy |
| `8090` | Discovery Engine | Asset inventory API |
| `8091` | Supply-Chain Engine | Vulnerability scan API |
| `8092` | Runtime Engine | Event ingestion API |
| `8093` | Detection Engine | Finding API |
| `8094` | Correlation Engine / Shield Admission | Admission: `POST /v1/admit` |
| `8095` | Docker AuthZ Plugin | `POST /AuthzPlugin.AuthZReq` |
| `8096` | K8s Webhook Simulator | `POST /v1/k8s/validate` |

---

## Security Architecture

### Fail-Closed Everywhere

| Component | Failure Mode | Behaviour |
|-----------|-------------|-----------|
| Docker AuthZ Plugin | DB unreachable, evaluator error | Blocks container start |
| Admission Handler | Timeout (50ms) or any error | Returns denied |
| K8s Webhook | Deadline exceeded (2s) | `allowed: false` (failurePolicy: Fail) |
| OPA Evaluator | Policy load error | Denied |
| Executor | Identity mismatch, expired auth, closed incident | `FAILED` — no Docker mutation |

### What is Authenticated

- **Executor target identity:** Re-derived from live Docker inspect at execution time
- **Break-glass signatures:** HMAC-SHA256 validated before approval is counted
- **NATS (reference config):** Not authenticated — NKey/TLS required for production
- **HTTP API inbound:** Not authenticated at the application layer — relies on upstream proxy

### Audit Trail

Three tables are enforced append-only by PostgreSQL triggers:

- `execution_state_transitions` — every executor FSM transition
- `policy_audit_trail` — every Shield decision, override, canary stage
- `override_approvals` — every HMAC signature (one permitted update: `revoked_at` before threshold)

---

## Known Limitations

Key limitations (full list in [docs/LIMITATIONS.md](docs/LIMITATIONS.md)):

- **NATS Core is not durable** — events missed during subscriber downtime are lost
- **DB→NATS is non-atomic** — a crash between commit and publish leaves NATS stale
- **No exactly-once execution guarantee** — a crash mid-execution requires manual recovery
- **Discovery, Supply-Chain, Runtime engines have no unit tests**
- **K8s TLS not provisioned** in this repository — cert-manager or equivalent required
- **No HTTP API authentication** at the application layer — upstream proxy required
- **macOS/Docker Desktop scope** — Falco eBPF behaviour may differ on production Linux
- **Human attestation ≠ Executor verification** — attestations are operator declarations, not system-verified postconditions

---

## Future Roadmap

Items not yet implemented (only listing what is consistent with the architecture):

- [ ] NATS JetStream with transactional outbox — eliminates DB→NATS non-atomicity
- [ ] Unit test coverage for Discovery, Supply-Chain, and Runtime engines
- [ ] K8s TLS certificate provisioning guide (cert-manager)
- [ ] HTTP API authentication layer (service mesh integration, or JWT middleware)
- [ ] Live multi-cluster testing (current: in-process NATS only)
- [ ] Broader execution actions (image update, pod restart, network policy injection)
- [ ] Auto-abandonment timer for stuck `EXECUTING` authorizations
- [ ] Production Linux / bare-metal Falco driver guide
- [ ] Structured observability (metrics, traces) — currently log-based only
- [ ] Policy test coverage gates before staging promotion

---

## Documentation

| Document | Purpose |
|----------|---------|
| [README.md](README.md) | This document — architecture, setup, reference |
| [SECURITY.md](SECURITY.md) | Vulnerability reporting, known security limitations, trust boundaries |
| [CONTRIBUTING.md](CONTRIBUTING.md) | Development setup, test commands, coding standards |
| [LICENSE](LICENSE) | Apache License 2.0 |
| [docs/THREAT_MODEL.md](docs/THREAT_MODEL.md) | Trust boundaries, attack surfaces, mitigations |
| [docs/FAILURE_SEMANTICS.md](docs/FAILURE_SEMANTICS.md) | Failure mode behaviour for 17 scenarios |
| [docs/LIMITATIONS.md](docs/LIMITATIONS.md) | Known limitations and gaps |

---

> **Services note:** The `services/` directory contains demo target microservices (`auth`, `product`, `payment`, `gateway`). These are intentionally simple applications that ZERAXIS monitors. The auth service returns a placeholder token — this is by design for the local lab demo, not a security concern within ZERAXIS itself.

---

*ZERAXIS — v0.1.1-alpha | Apache 2.0 | Go 1.27*
