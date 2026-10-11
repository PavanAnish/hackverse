# ZERAXIS Known Limitations

**Version:** 1.0 (Phase 9)  
**Status:** Engineering reference document

This document honestly records the known limitations and scope constraints of ZERAXIS at its current state of development. Limitations are not failures of the design — they are deliberate scope decisions or future work items.

---

## 1. Event Delivery: Core NATS Is Non-Durable

ZERAXIS uses **NATS Core** for all inter-engine event transport. NATS Core is a fire-and-forget publish/subscribe system with no persistence.

**Consequences:**
- An event published while its subscriber is offline is permanently lost from the transport layer
- There is no message replay capability
- Cross-cluster cache invalidation events published during a network partition are not delivered after reconnection

**What mitigates this:** The database is written before NATS publish. All durable state lives in PostgreSQL. Consumers can reconstruct state from the DB on restart or reconciliation.

**Future work:** Migrating to NATS JetStream with durable consumers and a transactional outbox pattern would provide at-least-once delivery guarantees.

---

## 2. DB → NATS Non-Atomicity

A database commit and the subsequent NATS publish are not an atomic operation. If the process crashes between a successful DB commit and a NATS publish, the state change is durable in PostgreSQL but the NATS event is never published.

**Affected operations:**
- Incident creation / resolution
- Recommendation creation
- Execution state transitions
- Admission cache invalidation events

**What mitigates this:** Cache TTL (5 minutes) ensures eventual consistency. DB is the source of truth for direct queries.

**What is not mitigated:** In the TTL window, consumers relying on NATS events may have stale state.

---

## 3. No Exactly-Once Execution Guarantee

The Executor does not implement an idempotency key or distributed locking beyond the PostgreSQL-level `SELECT ... FOR UPDATE SKIP LOCKED`. If a second Executor instance concurrently claims the same authorization (which the `SKIP LOCKED` prevents), or if a crash occurs mid-execution, the system does not guarantee that exactly one Docker mutation was performed.

**Current mitigation:** PostgreSQL `FOR UPDATE SKIP LOCKED` ensures only one Executor instance can claim an authorization at a time.

**Known gap:** If the Executor crashes after `EXECUTING` transition but before the Docker call completes, the state is stuck in `EXECUTING`. No automatic abandonment timer exists. Manual intervention is required.

---

## 4. No Autonomous Remediation When Authorization Is Required

ZERAXIS does not execute Docker mutations without an explicit authorization record in `execution_authorizations` with status `APPROVED`. The Correlation Engine generates recommendations; they must be approved before execution.

**This is by design**, not a limitation — it provides a human-controllable authorization gate. However, it means:
- Disruptive actions (stop, quarantine) require approval
- No "auto-approve all" mode exists in the current implementation
- Response time from threat detection to execution is bounded by the approval process

---

## 5. Human Remediation Attestation Is Not Executor Evidence

When an operator submits a `human_remediation_attestation`, this records their declaration that manual remediation was performed. However:
- It does not write to `execution_results`
- It does not set `verification_status` or `verified_at`
- It does not call `ContainerInspect` or verify any postcondition
- It is the operator's word, not a system-verified outcome

The Executor's verification (which does call `ContainerInspect`) and human attestation are distinct and non-interchangeable.

---

## 6. Recovery Recommendation Is Not Automatic Retry

When an execution fails and a recovery recommendation is generated, this does **not** automatically re-attempt the action. A recovery recommendation is a new recommendation that:
- Has a fresh `recommendation_id`
- Requires a fresh authorization and approval
- Goes through the full recommendation → authorization → execution cycle

There is no automatic retry loop.

---

## 7. Verification Only Proves Defined Postconditions

Post-execution verification for `STOP_CONTAINER` checks that the container's status is `exited` or `dead` and that the image digest matches. It does not:
- Verify that no data was exfiltrated before the container stopped
- Verify that no persistence mechanism was established
- Verify that the underlying vulnerability was remediated

Verification proves the defined security postcondition only.

---

## 8. Docker Desktop / macOS / LinuxKit Scope

ZERAXIS was developed and tested on macOS with Docker Desktop (kernel: LinuxKit). Known differences from production Linux:

- Falco with eBPF requires `SYS_ADMIN` capability in the LinuxKit VM; on native Linux this may vary by kernel version
- The Docker socket path (`/var/run/docker.sock`) is the standard macOS/LinuxKit path
- eBPF feature availability depends on the LinuxKit kernel version bundled with your Docker Desktop version
- Runtime tests were performed on Docker Desktop, not on a production Linux host

**Before Linux production deployment:** Review Falco driver compatibility for your kernel version. Test all eBPF-dependent components.

---

## 9. Three Engines Have No Unit Test Coverage

The following engines have implementations but no dedicated unit test files:

| Engine | Implementation | Tests |
|--------|---------------|-------|
| Discovery Engine | ✓ Implemented | ✗ No test files |
| Supply-Chain Engine | ✓ Implemented | ✗ No test files |
| Runtime Engine | ✓ Implemented | ✗ No test files |

These engines are exercised indirectly through integration (Detection, Correlation) and runtime verification, but they do not have isolated unit tests for their internal logic.

---

## 10. Kubernetes Integration Scope

The K8s `ValidatingWebhookConfiguration` is implemented and tested with a Kubernetes fake client. Known limitations:

- TLS certificate provisioning is not included in this repository — production deployment requires cert-manager or equivalent
- The webhook handler does not verify the Kubernetes API server's admission request signature
- Multi-cluster Kubernetes deployment has not been tested with a live multi-cluster environment
- K8s webhook tests use `client-go/kubernetes/fake` — live K8s integration testing requires a real cluster

---

## 11. Multi-Cluster Coherency Limitations

- Cluster identity (`CLUSTER_ID`) is self-reported via environment variable — not cryptographically verified
- NATS Core means missed events during partition are permanently lost from the transport layer
- Cross-cluster coherency has only been tested with in-process NATS connections (unit tests) — not with physically separate NATS servers or a NATS federation/cluster

---

## 12. Break-Glass: Concurrent Approval Race

If two approvers submit their HMAC signatures simultaneously, the `ON CONFLICT DO NOTHING` pattern and PostgreSQL serialization prevent duplicate approvals from the same approver. However, the approval count increment uses a plain `UPDATE` without `SELECT FOR UPDATE`. Under extreme concurrency, there is a theoretical race where the approval count could be incremented twice before status transitions to `APPROVED`. This edge case has not been reproduced in practice but is a known theoretical gap.

---

## 13. No Authentication on HTTP APIs

The internal HTTP APIs (`/v1/admit`, `/v1/k8s/validate`, etc.) do not implement authentication in the current reference implementation. Authentication is assumed to be provided by a reverse proxy, service mesh, or API gateway in front of the engines.

**This is appropriate for the reference lab** (`zeraxis-lab` Docker network is isolated) but must be addressed before any public-facing deployment.

---

## 14. Detection Rules Are Heuristic

The attack chain classifier in the risk engine maps Falco `rule_id` values to attack chain stages (`EXPOSURE`, `EXECUTION`, `NETWORK_ACTIVITY`, `IMPACT`) using string matching. Unknown rule IDs fall back to `EXPOSURE`. This means:

- New Falco rules not in the mapping are conservatively classified as `EXPOSURE`
- The mapping is not exhaustive — operators should review `engines/correlation/internal/risk/risk.go` and `engines/detection/rules/rules.yaml` for their specific threat model

---

## 15. Supply-Chain Scanner: Trivy Dependency

The Supply-Chain Engine depends on Trivy for vulnerability scanning. ZERAXIS does not bundle Trivy — it must be available in the environment. The current implementation shells out to Trivy rather than using the Trivy Go library directly. This means:

- Trivy version compatibility is not pinned
- Trivy database freshness is outside ZERAXIS's control
- SBOM parsing is limited to formats Trivy supports

---

## Summary

| Limitation | Severity | Future Work? |
|-----------|----------|-------------|
| NATS Core non-durable | High | Yes — JetStream |
| DB→NATS non-atomic | Medium | Yes — outbox pattern |
| No exactly-once execution | Medium | Future hardening |
| No auto-approval mode | Low (by design) | By design |
| Human attestation ≠ verification | Low (by design) | By design |
| No auto-retry | Low (by design) | By design |
| Verification proves postconditions only | Low (by design) | By design |
| macOS/Docker Desktop scope | Medium | Production Linux testing |
| 3 engines without unit tests | Medium | Yes |
| K8s TLS not provisioned | High (for production) | Yes — cert-manager guide |
| Multi-cluster tested in-process only | Medium | Yes — live cluster testing |
| Break-glass approval race | Low | Future hardening |
| No HTTP API authentication | High (for production) | Yes — service mesh / proxy |
| Detection rules are heuristic | Low (by design) | Ongoing rule development |
| Trivy not bundled/pinned | Medium | Yes — dependency management |

## 3. Ephemeral Container Identity Resolution

Runtime events for containers that disappear before Discovery observes them may have no resolvable AssetID.

The Discovery Engine uses a polling interval (currently 30 seconds) to snapshot container inventory. If an ephemeral container starts, triggers a Falco rule, and exits within that 30-second window before being observed by Discovery, the Runtime Engine will not be able to resolve its 12-character container ID to a valid `AssetID`. These events are explicitly processed as `UNRESOLVED` to prevent the fabrication of identities or incorrect attribution.
