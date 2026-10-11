# ZERAXIS Phase 5 Stage 2C Architecture Report

## 1. Executive Summary
Phase 5 Stage 2C bridges the static supply-chain context with dynamic runtime behavior. By correlating identified vulnerabilities in specific `image_digest` artifacts with the actual processes executed at runtime, ZERAXIS will generate contextual security findings. Crucially, the architecture avoids false causality ("vulnerable image = exploited") by implementing strict Evidence Strength metrics and deterministic contextual mapping, enriching Stage 2B/2A findings rather than creating duplicate isolated alerts.

## 2. Current Architecture
ZERAXIS operates a phased pipeline:
- **Phase 3 (Supply Chain)**: Scans OCI images, generating SBOMs (Syft), Vulnerability reports (Trivy), and Policy Decisions.
- **Phase 4 (Unified Events)**: PostgreSQL + NATS event backbone.
- **Phase 5 (Runtime Engine)**: Stateless (Stage 2A) and Temporal (Stage 2B) rules evaluate raw telemetry.

## 3. Stage 2C Objective
Connect the software supply chain to runtime execution deterministically. Stage 2C must determine if a vulnerable package is actively executed or exposed and classify the evidence strength without jumping to unprovable conclusions of exploitation.

## 4. Actual Phase 3 Data Model
Based on `engines/supply-chain/migrations/001_supply_chain_schema.sql`:
- **Table:** `supply_chain_images` (PK: `image_id`, fields: `digest`, `repository`, `tag`)
- **Table:** `supply_chain_sboms` (FK: `image_id`, fields: `raw_sbom`)
- **Table:** `supply_chain_packages` (PK: `package_id`, fields: `name`, `version`, `purl`)
- **Table:** `supply_chain_vulnerabilities` (PK: `vuln_id`, fields: `vulnerability_identifier`, `severity`, `installed_version`, `image_id`)
- **Table:** `supply_chain_policy_decisions` (fields: `result`, `critical_count`)

## 5. Actual Runtime Data Model
Based on unified event structure (`pkg/events/models.go`) and Falco telemetry mapping:
- `event_id`, `timestamp`, `event_type` (e.g. `PROCESS_EXECUTION`)
- `asset_id` (canonical Discovery identity)
- `image_digest` (immutable supply-chain identity)
- `metadata` map containing: `container_id`, `process`, `executable`, `path`, `destination_ip`

## 6. Identity Chain
- **Falco → `container_id`**: Deterministic.
- **`container_id` → `asset_id`**: Deterministic (handled by Discovery engine via container name hash).
- **`asset_id` → `image_digest`**: Deterministic.
- **`image_digest` → `vulnerabilities`**: Deterministic via `supply_chain_vulnerabilities.image_id`.
**Failure points**: External images lacking Phase 3 scans, or missing package `executable` paths in the raw SBOM.

## 7. Package → Process Mapping
This is intrinsically difficult. `PROCESS_EXECUTION` provides a path (e.g., `/usr/bin/openssl`), but `supply_chain_packages` maps by `name` and `version`, sometimes hiding the exact binary path.
**Safest Algorithm**: 
1. Query `raw_sbom` for the package `purl` associated with the vulnerability.
2. Check if the SBOM `files` array for that package includes a path exactly matching `event.metadata.executable`.
If not matched precisely, fallback to substring matching on package `name` vs `executable` (which reduces Evidence Strength).

## 8. Evidence Strength
- **STRONG**: Cryptographic process hash (unsupported by Falco currently) OR Exact file path match between SBOM package files array and runtime `event.metadata.executable`.
- **MEDIUM**: Substring match between vulnerable package name (e.g., `bash`) and runtime executable name (`/bin/bash`).
- **WEAK**: Image contains a Critical CVE somewhere, and the container exhibits suspicious network behavior, but causality cannot be established.
- **UNKNOWN**: Missing SBOM or Supply Chain API timeout.

## 9. Vulnerability Context
Runtime events will NOT contain full CVE descriptions. Findings will be enriched via foreign keys or minimal caching, utilizing the `/api/v1/assets/{id}/supply-chain` endpoint or direct DB lookups against `supply_chain_vulnerabilities`. We will append `CVE-XXXX`, `severity`, and `purl` to the Finding's `evidence` JSON.

## 10. Cache Architecture
To achieve >500 ev/sec, Stage 2C implements a bounded LRU memory cache.
- **Key**: `image_digest`
- **Value**: Pre-computed map of vulnerable executable paths (STRONG) and vulnerable package names (MEDIUM) + Policy Block status.
- **Size**: 1000 items max.
- **TTL**: 1 hour.
- **Negative Caching**: Cache `UNKNOWN` for 5 minutes if the Supply Chain API returns 404, preventing database DDoS.

## 11. Stage 2A/2B/2C Relationship
**Option A** is rejected (duplicative).
**Option C** is rejected (circular loops).
**Architecture**: Stage 2C consumes Stage 2A and 2B **Findings** directly as they are generated internally, effectively acting as an synchronous enrichment pipeline *before* the finding is finalized and published to NATS/Postgres. Alternatively, it evaluates as a separate contextual CEL rule matching on the raw event stream, appending to the same finding. 

*Recommended Direction*: Stage 2C operates as a parallel evaluator inside the Detection Engine. Contextual CEL rules (e.g., `context.has_cve(...)`) trigger Findings just like 2A/2B, grouping by fingerprint.

## 12. Contextual Detection Rules
1. **Critical Executable**: `PROCESS_EXECUTION` exact matches a `STRONG` vulnerable path with a CRITICAL CVE. (Key: `executable`, Confidence: HIGH).
2. **Blocked Image Execution**: `PROCESS_EXECUTION` from an image with a `policy_decision = BLOCK`. (Key: `asset_id`, Confidence: HIGH).
3. **Vulnerable Network Exposure**: `NETWORK_CONNECTION` egress from a binary tagged with a MEDIUM/WEAK network CVE. (Key: `process`, Confidence: MEDIUM).
4. **Vulnerable File Write**: `FILE_ACTIVITY` write by a vulnerable package. (Key: `process`, Confidence: MEDIUM).
5. **Contextual Upgrade**: A Stage 2B `Download -> Execute` sequence occurring on an image with a CRITICAL CVE. (Key: `asset_id`, Confidence: HIGH).

## 13. Exploitation Claim Boundaries
ZERAXIS must use precise terminology:
- **Exposed**: A vulnerable package binary was executed.
- **Behaviorally Suspicious**: Exposed + anomalous network/file behavior.
- **Potential Exploitation**: A matched temporal attack sequence involving a vulnerable binary.
The system will **NEVER** use "Confirmed Exploitation" as Falco telemetry lacks deep kernel payload inspection or memory layout analysis necessary to prove absolute exploit success.

## 14. CVE Correlation
Vulnerabilities are keyed directly to Phase 3's `supply_chain_vulnerabilities.vulnerability_identifier` (e.g., `CVE-2021-44228`). No secondary vulnerability translation mechanism is introduced.

## 15. Version Correlation
Stage 2C will completely trust the Phase 3 (Trivy) scanner's determination of `installed_version` vs affected range. Stage 2C will not implement its own semantic version comparisons at runtime.

## 16. Image Digest Handling
`image_digest` is immutable. If a container rebuilds, the new `container_id` uses the new `image_digest`. The cache must fetch the new digest's vulnerability context. Context is NEVER inherited by container name or tag.

## 17. Finding Model
Stage 2C **Updates** or **Expands** existing Stage 2A/2B Findings. It adds contextual evidence (e.g., `{"context": {"cve": "CVE-123", "strength": "STRONG"}}`) to the `evidence` field. It does not spawn duplicate child alerts.

## 18. Fingerprinting
Contextual fingerprints inherit the 2A/2B base:
`SHA256(rule_id + rule_version + asset_id + image_digest + normalized(group_by))`
A "Critical Vulnerability Executed" rule will naturally group all subsequent executions of that binary on that asset into a single updated Finding.

## 19. Evidence Graph
The data is natively traversable via SQL FKs:
`findings.asset_id` → `supply_chain_asset_images` → `supply_chain_images` → `supply_chain_vulnerabilities`.
The `GET /api/v1/findings/{id}/context` API will dynamically construct this JSON tree for operators. A dedicated graph database is unnecessary.

## 20. Finding Explanation
The JSON `evidence` array will synthesize an explanation object:
```json
{
  "explanation": "payment-service executed /usr/bin/openssl. Image digest sha256:XXX contains openssl 3.0.1 affected by CVE-XXXX. Evidence strength: STRONG."
}
```

## 21. API
Proposed API additions to `engines/detection`:
- `GET /api/v1/findings/{id}/context`: Joins finding events with Supply Chain CVE data.
- API bloat is actively avoided; no graph-query DSLs or arbitrary injection endpoints will be created.

## 22. Database
**No new tables are required.** The existing `findings` table combined with cross-schema joins to `supply_chain_vulnerabilities` and `supply_chain_sboms` is structurally complete.

## 23. NATS
Subscribes: `zeraxis.events.runtime.>`
Publishes: `zeraxis.events.detection.finding.created/updated`
Loops are prevented because the Detection engine never subscribes to its own `finding.>` output stream.

## 24. Performance
- Target: 500 runtime events/sec.
- Cache guarantees: 99% cache hit rate for `image_digest` lookups.
- DB Queries: 0 DB queries on cache hit for stateless contextual rules (excluding the final UPSERT finding step). Memory utilization remains < 100MB for the 1000-digest LRU cache.

## 25. Failure Behavior
If Phase 3 DB/API is unavailable:
- Cache misses resolve to `UNKNOWN` context.
- Contextual rules fail open (they do not trigger).
- The Engine explicitly will not transition "UNKNOWN" to "safe". `evidence_strength` gracefully downgrades.

## 26. Security
Runs entirely unprivileged. The engine reads Phase 3 Postgres tables but cannot mutate them. CEL contexts are strictly typed strings and maps, isolating the engine from SQL injection or RCE via malformed SBOMs.

## 27. Testing Strategy
- **Test 1**: Vulnerable Image + `PROCESS_EXECUTION` matching SBOM path -> STRONG Finding.
- **Test 2**: Vulnerable Image + unrelated execution -> No Finding.
- **Test 3**: Rebuilt Image (New digest) -> Old CVE Context missing -> No Finding.
- **Test 4**: API/DB outage -> Graceful degradation to UNKNOWN context.

## 28. Adversarial Testing
The `image_digest` cache is capped at 1000 items. An adversary aggressively spinning up 10,000 unique vulnerable images will trigger LRU cache evictions. The engine will seamlessly fall back to database lookups, bound by DB connection limits, preventing Detection Engine OOM crashes.

## 29. Observability
New Metrics:
- `context_cache_hits`, `context_cache_misses`, `context_vulnerabilities_matched`, `context_unknown_images`.

## 30. Phase 6 Compatibility
By pushing the Evidence JSON into the `findings` table and keeping the NATS envelope pure, Stage 2C naturally prepares the data for a future Phase 6 Risk Scoring engine without hardcoding arbitrary risk metrics today.

## 31. Implementation Breakdown
1. **2C.1**: LRU Image Context Cache + Phase 3 SQL Integrations.
2. **2C.2**: Evidence Strength Mapping (SBOM Path -> Process).
3. **2C.3**: Contextual CEL Functions (e.g., `context.has_critical_cve()`).
4. **2C.4**: Contextual Detection Rules.
5. **2C.5**: Lab Testing & Failure injection.

## 32. Risks
- **Distroless/Custom Binaries**: Go statically compiled binaries don't appear in typical OS package SBOM trees with paths, making STRONG execution mapping highly unreliable without advanced eBPF binary hashing.
- **Cache Stamping**: Sudden deployments of a new digest across 100 pods might trigger 100 simultaneous DB context lookups.

## 33. Known Limitations
- "Confirmed Exploitation" is technically unachievable.
- Path-based STRONG matching fails on renamed binaries, symlinks, or in-memory execution (e.g., `memfd_create`).

## 34. MUST SOLVE BEFORE IMPLEMENTATION
1. Cross-engine Database RBAC: Detection Engine must be granted read-only access to `supply_chain_%` tables if querying PostgreSQL directly, or a high-performance internal gRPC/HTTP API must be established to avoid DB coupling.

==================================================
FINAL GATE
==================================================
READY FOR STAGE 2C IMPLEMENTATION
