# ZERAXIS Phase 5 Stage 2 Implementation Gate Report

## 1. Rule Language Decision
**Decision: Google CEL (Common Expression Language)**

**Evaluation:**
- **Security & Determinism**: CEL is strictly declarative, non-Turing complete, and designed for guaranteed termination. It cannot execute arbitrary code.
- **Performance**: Sub-microsecond evaluation times; parses into a highly optimized AST.
- **Complexity & Dependencies**: Minimal dependency footprint (`github.com/google/cel-go`). No need to maintain a custom parser.
- **Capabilities**: Natively handles JSON/Protobuf traversal (e.g., `event.metadata.path`), string matching (`startsWith`, `matches`), and boolean logic.
- **Alternative B (Go Matchers)**: Rejected because it requires recompiling the engine to add rules.
- **Alternative C (Custom Parser)**: Rejected due to unnecessary maintenance burden and security risk.

## 2. Finding Deduplication Fingerprint
**Design:**
The deterministic finding fingerprint must group related behaviors into a single actionable incident while preserving distinct attacks. 

**Fingerprint Formula:**
`SHA256(rule_id + asset_id + image_digest + quantized_time_window + hash(group_by_fields))`

By allowing rules to define `group_by_fields` (e.g., `metadata.destination_ip` or `metadata.process_name`), we achieve flexible deduplication:

**Concrete Examples:**
1. **Event Sequence:** Container executes `sh` 1000 times in 1 minute.
   - **Rule:** Unexpected Shell (group_by: `asset_id`)
   - **Fingerprint:** `Hash("rule-shell", "asset-A", "digest-X", "2026-08-27T12:00", "")`
   - **Behavior:** 1 Finding created, updated 999 times.
2. **Event Sequence:** Container connects to 10 distinct malicious IPs.
   - **Rule:** Malicious IP (group_by: `destination_ip`)
   - **Fingerprint:** 10 distinct fingerprints based on the 10 IPs.
   - **Behavior:** 10 separate Findings created.
3. **Event Sequence:** Image is rebuilt (new digest), deployed, and continues malicious behavior.
   - **Rule:** Any rule
   - **Fingerprint:** Changes because `image_digest` changed.
   - **Behavior:** New Finding created, as the underlying software artifact has fundamentally changed.
4. **Event Sequence:** Intermittent file writes to `/etc/passwd` over 5 days.
   - **Rule:** Sensitive File Write (quantized_window: 24h)
   - **Fingerprint:** Changes daily.
   - **Behavior:** 5 separate Findings (1 per day) to ensure stale alerts aren't buried in long-forgotten tickets.
5. **Event Sequence:** 1000 network connections to the same internal database.
   - **Rule:** Unexpected DB Connection (group_by: `destination_port`)
   - **Fingerprint:** Consistent for all 1000 events.
   - **Behavior:** 1 Finding, updated 999 times, preventing ticket flood.

## 3. Asset Lifecycle Verification
**Actual Phase 2 Behavior (Verified from `scanner.go`):**
The Discovery Engine assigns `AssetID` using a deterministic hash of the container name (`sha256("zeraxis:asset:" + containerName)`).

- **Container Restart:** Same `container_id`, same `asset_id`, same `image_digest`. (Temporal correlation: **Continue**)
- **Container Recreation:** *New* `container_id`, same `asset_id`, same `image_digest`. (Temporal correlation: **Reset**. A new filesystem/process tree means behavior is not a direct continuation of the dead container.)
- **Container Rename:** *New* `container_id`, *New* `asset_id`, same `image_digest`. (Temporal correlation: **Reset**)
- **Image Rebuild/Replacement:** *New* `container_id`, same `asset_id`, *New* `image_digest`. (Temporal correlation: **Reset**)

**Conclusion:** `AssetID` is canonical for Findings deduplication, but **`container_id` MUST be the primary key for temporal event correlation windows** to prevent bleeding state across destroyed and recreated containers.

## 4. Event Ordering Model
NATS guarantees at-most-once/at-least-once (depending on config) but not strict global chronological ordering, especially across multiple engines.

**Deterministic Ordering Model:**
- **Buffering**: Temporal correlation rules use an in-memory sliding window buffer (e.g., 5 seconds) keyed by `container_id`.
- **Sorting**: Events in the buffer are sorted by their embedded canonical `timestamp`, NOT arrival time.
- **Late Arrival**: Events arriving after the window has expired and state evicted are evaluated strictly as stateless events. They cannot retroactively trigger a stateful finding that required them. 
- **Duplicate Drops**: Handled upstream via an LRU cache of `event_id`s before evaluation.

## 5. Supply-Chain Context Model
The Detection Engine will fetch context via the Supply Chain API (`/api/v1/assets/{id}/supply-chain`) and maintain an LRU cache.

**Evidence Strength Classification:**
- **STRONG**: Not technically feasible without eBPF hashing binaries (Falco doesn't currently do this).
- **MEDIUM**: The `PROCESS_EXECUTION` executable path precisely matches a known vulnerable path in the SBOM. (e.g., executing `/usr/bin/openssl` when `openssl` has a CRITICAL CVE).
- **WEAK**: The container image has a CRITICAL vulnerability, and the container exhibits suspicious network behavior, but we cannot mathematically prove the vulnerable binary caused the network call.

## 6. Initial 5 Rules (Stage 2A)
Ranked for immediate value and testability in the lab:
1. **Package Manager Execution** 
   - *Value*: High (indicates live modification). *FP Risk*: Low. *Testable*: Yes (`apk add`).
2. **Unexpected Shell Spawning**
   - *Value*: High. *FP Risk*: Low. *Testable*: Yes (`docker exec -it sh`).
3. **Sensitive System File Write**
   - *Value*: High. *FP Risk*: Low (containers should be immutable). *Testable*: Yes (`touch /etc/passwd`).
4. **Unexpected External Connection**
   - *Value*: Medium. *FP Risk*: Medium. *Testable*: Yes (`wget example.com`).
5. **Critical Vuln + Shell Execution** (Contextual)
   - *Value*: High. *FP Risk*: Low. *Testable*: Yes (Deploy known vulnerable image, then exec shell).

## 7. Database Requirements
The proposed `findings` and `finding_events` tables are sufficient.
**Indexes**:
- `findings(fingerprint)`: UNIQUE constraint to enforce deterministic deduplication.
- `findings(asset_id, status)`: For API querying.
- `finding_events(finding_id)`: Foreign key index.

**Transitions**: 
- `OPEN` -> (receives new event) -> `UPDATED`.
- `UPDATED` -> (manual intervention via API) -> `RESOLVED`.
- `RESOLVED` -> (receives new event outside cooldown) -> Generates new Finding with new fingerprint (via shifted time window) or transitions back to `OPEN`.

## 8. Performance Analysis
- **Target**: 500 events/sec, <200MB RAM, <100ms latency.
- **Engineering Reality**: CEL evaluations take ~100ns per rule. For 100 rules * 500 events, CPU time is 5ms/sec (trivial). Memory will easily stay under 200MB because raw telemetry is discarded immediately after evaluation; only the fixed-size sliding window arrays and finding metadata remain in heap. 
- *Note*: These are engineering targets based on Go/CEL benchmarks, not verified lab load tests.

## 9. Security Analysis
- The Engine inherits the Stage 1 security posture: unprivileged, no Docker socket, no host mounts.
- **CEL Sandbox**: CEL strictly prevents arbitrary code execution, file system access, or network calls from within a rule evaluation.
- The Engine only interacts with NATS, Postgres, and internal APIs. It cannot mutate infrastructure.

## 10. Remaining Risks
- **Data Volume**: The `finding_events` table could grow massive if a single finding accumulates millions of linked events. We must cap linked events per finding (e.g., store the first 100 and the last 100, dropping the middle).
- **SBOM Pathing**: Correlating `metadata.process` to SBOM packages relies on standardized paths which distroless or custom Alpine images often obscure.

## 11. Implementation Prerequisites
1. Write the database migrations for `findings` and `finding_events`.
2. Introduce `github.com/google/cel-go` to the Go workspace.
3. Scaffold the `detection-engine` microservice shell.

==================================================
FINAL DECISION

**READY FOR STAGE 2A**

**Implementation Order:**
1. Database migrations (`findings`, `finding_events`, `rules`).
2. Generate Go models for Findings.
3. Build the CEL evaluation engine (Stateless only for 2A).
4. Implement NATS subscriber to route `zeraxis.events.runtime.>` to the CEL engine.
5. Implement the Fingerprint + Deduplication logic.
6. Build integration tests proving the first 5 rules operate deterministically.
