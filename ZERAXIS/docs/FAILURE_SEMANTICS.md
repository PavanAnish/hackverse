# ZERAXIS Failure Semantics

**Version:** 1.0 (Phase 9)  
**Status:** Engineering reference document

This document describes how ZERAXIS behaves under failure conditions. The core principle throughout is:

> **The PostgreSQL database is the authoritative source of truth. NATS event publication is best-effort and non-atomic with respect to the database write.**

---

## 1. Database Unavailable

**Affected components:** All engines  

**Discovery Engine:** Poll loop skips the cycle. No events emitted. Assets marked `last_seen` at previous cycle remain in DB. No panic.

**Supply-Chain Engine:** Scan results cannot be persisted. Scans are retried on next cycle.

**Detection Engine:** Events received from NATS cannot be persisted. Events are discarded (NATS Core has no replay). Findings are not created.

**Correlation Engine:** New NATS events cannot be processed. Existing incidents retain their last known state in PostgreSQL.

**Executor Engine:** `Execute()` attempts DB transaction at start; if DB is unavailable, emits `EXECUTION_FAILED` audit entry (if DB write is possible) and returns error. No Docker mutation occurs.

**Shield Admission Handler:** Cache miss falls back to DB query. If DB is unreachable, the handler **blocks** the admission request (fail-closed). All requests return denied until DB recovers.

**Shield OPA Evaluator:** Policy cannot be loaded from DB. Evaluator returns denied (fail-closed).

---

## 2. Docker Daemon Unavailable

**Executor Engine:** `Execute()` calls Docker SDK `ContainerStop` or `ContainerDisconnect`. If the Docker socket is unavailable, the SDK call returns an error. The executor transitions the authorization to `FAILED`. No retry is automatic. A recovery recommendation is created by the Correlation Engine on the next cycle.

**Discovery Engine:** Docker API poll returns error. Cycle is skipped. Existing assets are not marked removed until they are absent from two consecutive successful scans (reconciliation logic).

**Verification step:** Post-execution verification calls `ContainerInspect`. If Docker is unavailable, verification status is set to `FAILED` (verification failure, not mutation failure — these are distinct states).

---

## 3. Target Container Missing at Execution Time

**Executor Engine:** Before Docker mutation, the executor calls `ContainerInspect` to revalidate the target. If the container is not found (404 from Docker API), the authorization transitions to `FAILED`. No mutation is attempted. This is the correct behaviour — the threat may have self-resolved (container already stopped/removed).

---

## 4. Container ID Mismatch

**Scenario:** The `target_container_id` in the `execution_authorizations` row no longer matches the running container for that `asset_id`. This can happen if a container was recreated after the recommendation was generated.

**Executor Engine:** `ValidateExecutionTarget()` compares `target_container_id` against the live Docker inspect result. Mismatch → authorization transitions to `FAILED`, no Docker mutation. The new container (with a new `ContainerID`) would require a fresh incident, recommendation, and authorization cycle.

---

## 5. Image Digest Mismatch

**Scenario:** The `target_image_digest` in the authorization does not appear in the live container's `RepoDigests`. This could indicate the container was started from a different image than recorded, or that the image is referenced only by `ImageID` (which is explicitly rejected as non-authoritative).

**Executor Engine:** `ValidateTargetDigest()` iterates `RepoDigests` looking for a canonical match (`<name>@sha256:<hash>` format). If no match is found, the execution is aborted and transitions to `FAILED`. `ImageID` values (which look like `sha256:<hash>` without a registry prefix) are explicitly rejected.

---

## 6. Stale Incident (Resolved Before Execution)

**Scenario:** An execution authorization was approved, but before the Executor claimed it, an operator manually resolved the incident (or another execution path resolved it).

**Executor Engine:** At the start of `Execute()`, the incident status is re-checked from PostgreSQL. If status is `RESOLVED` or `CLOSED`, the authorization is immediately transitioned to `FAILED` with reason `DENY: Incident closed`. No Docker mutation occurs. The ledger entry records this path.

---

## 7. Revoked Authorization

**Scenario:** An approver submitted an approval and then revoked it before a second approval was collected (only possible before the threshold is met for break-glass). For ordinary execution authorizations, revocation is handled separately in the approvals service.

**Approvals Service:** `RevokeAuthorization()` sets the authorization status to `REVOKED`. The Executor will not process an authorization that is not in `APPROVED` status.

**Break-Glass Override Service:** `RevokeApproval()` decrements the approval count. If the request is already `APPROVED` (threshold met), revocation is blocked with `ErrCannotRevoke`.

---

## 8. Expired Authorization

**Executor Engine:** At execution claim time, the executor checks `time.Since(approvedAt) > 1 hour`. If expired, the authorization transitions to `FAILED`. No Docker mutation occurs.

**Break-Glass Override Service:** If `ExecuteOverride()` is called after 1 hour, the request transitions to `EXPIRED` and an error is returned.

---

## 9. Execution Mutation Failure

**Scenario:** The Docker `ContainerStop` or `ContainerDisconnect` API call returns an error (e.g., the Docker daemon is overloaded, the container has already stopped).

**Executor Engine:** If `finalExecStatus` is set to `FAILED` at the mutation step, the authorization is written to `EXECUTED=FAILED` with the error logged. The ledger records `EXECUTING → FAILED`. A recovery recommendation is generated by the Correlation Engine.

**Important:** Container already being in a terminal state (`exited`, `dead`) at verification time is treated as **success** — the postcondition (container not running) is satisfied.

---

## 10. Verification Failure

**Scenario:** After a successful Docker mutation, post-execution verification checks whether the security postcondition was achieved. For `STOP_CONTAINER`, verification confirms the container is in `exited` or `dead` state and that the running `ImageID` matches the authorized digest.

**Executor Engine:** If verification fails, `execution_results.verification_status` is set to `FAILED`. This does **not** roll back the Docker mutation. The mutation occurred; the verification step only proves the postcondition. A separate recovery recommendation is created.

**What verification does not do:** Verification is read-only. It calls `ContainerInspect` and inspects network state. It does not perform additional Docker mutations.

---

## 11. DB Commit Succeeds, NATS Publish Fails

**This is the critical non-atomicity gap.**

**Scenario:** The Correlation Engine writes an incident to PostgreSQL (commit succeeds), then attempts to publish a `zeraxis.events.correlation.incident.resolved` event to NATS. The NATS server is temporarily unavailable. The publish call fails.

**Result:** The incident is durably `RESOLVED` in PostgreSQL. The Shield cache and other NATS subscribers do not receive the invalidation event. The cache remains stale until the next time the cache is refreshed (TTL expiry or next cache load).

**Mitigations present:**
- Cache has a 5-minute TTL — stale entries expire
- Shield falls back to DB on cache miss (so new requests after TTL will re-check DB)

**What is not mitigated:**
- Within the TTL window, a previously-blocked image may be admitted by the stale cache if the incident was resolved
- There is no outbox/transactional inbox pattern implemented — the publish is a best-effort call after commit

**Future work:** NATS JetStream with a transactional outbox would close this gap.

---

## 12. NATS Subscriber Downtime

**Scenario:** A Shield engine instance is offline when cross-cluster resolution events are published.

**Result:** Resolution events are permanently lost from the transport layer (NATS Core has no persistence). The offline cluster's cache remains stale until its TTL expires. On reconnect, the Shield engine subscribes fresh — it does not receive events that were published during its absence.

**Manual remediation:** See the Operations Manual for the cluster cache reconciliation procedure.

**What is not affected:** PostgreSQL state is unaffected. Any new admission request after cache TTL will re-check PostgreSQL and get the correct state.

---

## 13. Late Executor Completion

**Scenario:** An Executor is slow (Docker operation takes longer than expected). Meanwhile, a human operator manually remediates the issue and resolves the incident via the approvals API. The late Executor then attempts to claim the authorization.

**Executor Engine:** At claim time, the incident status is re-checked. If `RESOLVED`, the authorization transitions to `FAILED` immediately. The late Executor cannot affect the new incident state.

**`TestPhase78_LateOldExecutorAfterManuallyRemediatedCannotAlterIncident`** — verified in test suite.

---

## 14. Abandoned Execution

**Scenario:** An authorization enters `APPROVED` status but the Executor crashes before claiming it. The authorization sits in `APPROVED` state.

**Current behaviour:** There is no automatic abandonment timer in the current implementation. An unclaimed `APPROVED` authorization remains in that state. A crash mid-execution (`EXECUTING` at crash time) leaves the authorization in `EXECUTING`.

**Recovery:** Manual intervention is required — either a fresh Executor instance claims and re-validates the authorization, or an operator sets the status to `ABANDONED` directly. This is a known limitation documented in `docs/LIMITATIONS.md`.

---

## 15. Manual Remediation (Human Attestation)

**Scenario:** An operator physically intervenes to remediate a threat (e.g., manually stops a container, patches a vulnerability, removes malicious files). They submit a `human_remediation_attestation` record.

**What the attestation represents:** An operator's declaration that a specific remediation was performed, with the target identity (`target_asset_id`, `target_container_id`, `target_image_digest`) recorded immutably.

**What the attestation does NOT represent:**
- It is not Executor evidence — it does not appear in `execution_results`
- It does not set `verified_at` in `execution_results`
- It does not prove a security postcondition was achieved (unlike Executor verification which calls `ContainerInspect`)
- It is an operator's word, not a system-verified outcome

**`TestPhase78_HumanRemediationCreatesNoExecutionResults`** — verified in test suite.

---

## 16. Safe-to-Retry

**Scenario:** A failed execution (Docker mutation failed) results in the Correlation Engine generating a recovery recommendation. An operator reviews the failure and determines the root cause was transient (e.g., Docker daemon momentarily unavailable).

**What `OUTCOME_SAFE_TO_RETRY` means:** The operator attests that the original failed execution can be re-attempted. This creates a **fresh recommendation identity** — a new `recommendation_id` and `execution_authorization` are required. The original failed execution record is not modified.

**`TestPhase78_SafeToRetryCreatesFreshRecommendationIdentity`** — verified in test suite.

---

## 17. Resolved Incident Receiving a New Finding

**Scenario:** An incident is resolved (status `RESOLVED`). A new finding with the same base fingerprint arrives within the same 1-hour time bucket.

**Correlation Engine:** Due to migration `013_human_attestations.sql`, the `UNIQUE` constraint on `incidents.fingerprint` was replaced with a partial unique index: `UNIQUE (fingerprint) WHERE status NOT IN ('RESOLVED', 'CLOSED')`. A new `incident_id` is created for the new finding, even if the fingerprint matches a recently resolved incident.

**`TestPhase78_RecurringMatchingFindingCreatesNewIncidentIdentity`** — verified in test suite.

---

## Summary Table

| Failure | DB Preserved? | NATS Event Lost? | Docker Mutation? | Recovery |
|---------|--------------|-----------------|-----------------|---------|
| DB unavailable | N/A | Yes (not published) | No | Auto-retry on reconnect |
| Docker unavailable | Yes (`FAILED`) | Yes | No | Recovery recommendation |
| Container missing | Yes (`FAILED`) | N/A | No | New incident cycle |
| ContainerID mismatch | Yes (`FAILED`) | N/A | No | New incident cycle |
| Digest mismatch | Yes (`FAILED`) | N/A | No | New incident cycle |
| Stale incident | Yes (`FAILED`) | N/A | No | No action needed |
| Revoked authorization | Yes (`REVOKED`) | N/A | No | Manual |
| Expired authorization | Yes (`FAILED`) | N/A | No | New authorization |
| Mutation fails | Yes (`FAILED`) | Yes | No (or partial) | Recovery recommendation |
| Verification fails | Yes (`FAILED`) | No | Yes (mutation happened) | Recovery recommendation |
| DB commit + NATS fail | Yes (committed) | Yes | N/A | Cache TTL expiry |
| Subscriber offline | Yes (in DB) | Yes (lost in transport) | N/A | Manual reconciliation |
| Executor crash (EXECUTING) | Yes (`EXECUTING`) | N/A | Unknown | Manual intervention |
