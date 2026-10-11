# ZERAXIS Threat Model

**Version:** 1.0 (Phase 9)  
**Status:** Engineering reference document

This document describes the trust boundaries, attack surfaces, and threat mitigations implemented in ZERAXIS. It uses precise language: "mitigates," "detects," "rejects," or "fails closed" rather than "prevents" or "eliminates."

---

## 1. Assets Under Protection

| Asset | Description |
|-------|-------------|
| **AssetID** | Logical ZERAXIS identity for a discovered container. Derived deterministically as `UUID(SHA-256("zeraxis:asset:" + containerName))`. Stable across container recreation. |
| **ContainerID** | Docker-assigned runtime identity (12-char prefix of container ID). Changes on every recreation. Used for lifecycle-specific correlation boundaries. |
| **ImageDigest** | Authoritative supply-chain identity. OCI manifest digest in the form `sha256:<64 lowercase hex>`. Content-addressed and immutable. |
| PostgreSQL database | Persistent state store for all incidents, authorizations, audit ledgers, and policy state |
| NATS event backbone | Intra-platform event transport |
| Shield admission cache | In-process materialized view of blocked fingerprints |
| OPA policy engine | Rego-based admission decision engine |
| K8s API server | Target of ValidatingWebhook enforcement |

> **Identity note:** AssetID ≠ ContainerID ≠ ImageDigest. These are distinct, serve different purposes, and must not be substituted for one another in security decisions.

---

## 2. Trust Boundaries

```
┌───────────────────────────────────────────────────────┐
│  ZERAXIS Internal Network (zeraxis-lab Docker network) │
│                                                        │
│  PostgreSQL ← trusted, no external exposure           │
│  NATS       ← trusted, no auth in reference config    │
│                                                        │
│  Discovery Engine (reads Docker socket, read-only)    │
│  Supply-Chain Engine (reads OCI registry)             │
│  Runtime Engine (reads Falco output)                  │
│  Detection Engine (reads from DB + NATS)              │
│  Correlation Engine (authoritative state machine)     │
│  Executor Engine (writes Docker socket, authorized)   │
│  Shield Engine (reads Docker socket, write-via-authz) │
└──────────────────┬────────────────────────────────────┘
                   │
          (HTTP APIs on specific ports)
                   │
┌──────────────────▼────────────────────────────────────┐
│  External / Untrusted Boundary                         │
│                                                        │
│  Docker daemon (enforced via AuthZ plugin)            │
│  Kubernetes API server (enforced via Webhook)         │
│  OCI registry (digests trusted, tags untrusted)       │
│  Inbound container images (identity per digest only)  │
└───────────────────────────────────────────────────────┘
```

---

## 3. Attack Surfaces

### 3.1 Docker Daemon Boundary

**What ZERAXIS controls:** The Docker AuthZ plugin intercepts every `docker run` (and other container lifecycle requests) before the daemon processes them. Shield evaluates the image digest against the OPA policy and admission cache.

**What is trusted:** OCI image digest (content-addressed, cannot be forged by registry metadata)  
**What is untrusted:** Image tags, image names, container labels — these are mutable metadata  
**Fail behaviour:** If evaluation fails for any reason (timeout, DB error, OPA error), the plugin **blocks** the request

**Does not currently protect against:**
- Images already running before the AuthZ plugin was installed
- Container runtime escapes (out of scope)
- Docker daemon API access granted to other authorized processes

### 3.2 Falco Privileged Sensor Boundary

**What Falco accesses:** Linux kernel ring buffer via eBPF. Observes syscalls for all processes on the host.

**What is trusted:** Falco kernel driver output (trusted as accurate but not authenticated)  
**What is untrusted:** Falco output if the Falco process itself were compromised  
**Capabilities required:** `SYS_ADMIN`, `BPF`, `PERFMON`, `SYS_RESOURCE`, `SYS_PTRACE`

**Mitigations:**
- Falco does **not** use `privileged: true` — explicit minimum capabilities only
- Falco output is written to a shared volume, not to the network
- Runtime Engine reads from this volume — any corruption causes event parsing errors, not silent acceptance

**Does not currently protect against:**
- Kernel-level tampering that silences eBPF probes
- Rootkit-level evasion of Falco rules

### 3.3 Runtime → Detection Boundary

**Data flow:** Falco JSON events → Runtime Engine normalization → PostgreSQL + NATS

**What is validated:**
- Container identity resolved via Discovery API (AssetID lookup by ContainerID)
- Events with unknown container identity are still persisted with `asset_id=UNKNOWN`
- Malformed JSON from Falco is rejected at parse time

**What is not authenticated:** Falco output is not cryptographically signed  
**Fail behaviour:** Event parsing failure → event is dropped and logged; does not crash the engine

### 3.4 Detection → Correlation Boundary

**Data flow:** Findings from Detection Engine → NATS → Correlation Engine

**What is validated:**
- `fingerprint` is deterministically derived from `(rule_id, rule_version, asset_id, image_digest, time_bucket)` — callers cannot inject an arbitrary fingerprint to group into an existing incident
- Malformed finding events → logged and discarded
- Oversized payloads → rejected (bounded ingestion)

**What is not authenticated:** NATS subjects are unauthenticated in the reference configuration  
**Fail behaviour (NATS down):** Findings are persisted to PostgreSQL before NATS publish; NATS publish failure does not roll back the DB write

### 3.5 Correlation → Executor Boundary (Authorization Gate)

This is the most security-critical boundary in the reactive path.

**What is validated before any Docker mutation:**
1. Execution authorization record exists in `execution_authorizations` with status `APPROVED`
2. Authorization is not expired (> 1 hour since approval)
3. Incident is still open (not `RESOLVED` or `CLOSED`) — closed incident → `FAILED`, no Docker action
4. Target `asset_id` and `container_id` match current live container (prevents targeting wrong container)
5. Target `image_digest` is present in the live container's `RepoDigests` — `ImageID` is explicitly rejected as a substitute

**What is trusted:** PostgreSQL state (authorizations, incidents)  
**What is untrusted:** Any externally supplied target identity — the executor re-derives it from live Docker inspect

**Fail behaviour:** Any validation failure → transition to `FAILED`, no Docker mutation, ledger entry written

### 3.6 Human Authorization Gateway

Certain recommendations enter `MANUAL_INVESTIGATION_REQUIRED` status. Human operators:
- Submit an attestation via the approvals API specifying `operator_id`, `outcome`, and the target identity fields
- The attestation records `target_asset_id`, `target_container_id`, `target_image_digest` immutably
- **A human attestation is not Executor evidence** — it does not update `execution_results` or `verified_at`

**What is authenticated:** Operator identity is recorded but not cryptographically verified in the current implementation. Authentication is assumed to be handled by the API gateway / upstream identity provider.

**What is not currently protected against:**
- Operator identity spoofing if no upstream authentication exists
- Operator submitting false attestation outcomes

### 3.7 NATS Event Backbone

**Trust level:** Internal transport, trusted within the `zeraxis-lab` network  
**Reference config:** No authentication, no TLS  
**Production requirement:** NKey authentication or TLS mutual auth must be configured before any multi-host deployment

**What ZERAXIS guarantees:**
- Database is written **before** NATS publish for all state-changing events
- If NATS publish fails after a DB commit, the state is durable in PostgreSQL
- Consumers can replay from DB on restart

**What ZERAXIS does NOT guarantee:**
- Events published while a subscriber is offline are permanently lost from the NATS transport path
- There is no message deduplication guarantee at the NATS layer
- Exactly-once delivery is not claimed

### 3.8 PostgreSQL

**Trust level:** Fully trusted; treated as the authoritative source of truth  
**Reference config:** `postgres:postgres` credentials, port 5433 exposed for local debugging only  
**Production requirement:** Strong credentials, no external exposure, TLS connections

**Immutability enforcement:**
- `execution_state_transitions` — `BEFORE UPDATE OR DELETE` trigger raises exception (append-only)
- `policy_audit_trail` — same pattern (append-only)
- `override_approvals` — same pattern with the exception that `revoked_at` may be updated once (from `NULL` to a timestamp) when an approval is revoked before the threshold is met. This is the only permitted update.

### 3.9 OCI Registry

**Trust level:** Partially trusted  
**What is trusted:** Image digest (`sha256:<hash>`) — content-addressed, cannot be forged  
**What is untrusted:** Image tags — mutable, can be reassigned to different content

**ZERAXIS enforcement:**
- Supply-Chain Engine records images by `image_digest`, not tag
- Migration `002_cleanup_mutable_identities.sql` removes any tag-based identity records
- Executor revalidates digest from live container `RepoDigests` before any action

**Does not currently protect against:**
- Malicious content in a valid digest (that's what vulnerability scanning is for)
- Registry compromise that replaces content behind an existing digest (content-addressing makes this extremely difficult but not impossible at the registry implementation level)

### 3.10 K8s ValidatingWebhook

**Scope:** All Pod `CREATE` and `UPDATE` operations, cluster-wide  
**Failure policy:** `Fail` (fail-closed) — if the webhook is unreachable or times out, pod scheduling is blocked  
**Timeout:** 2 seconds

**What is validated:** Each container image in `spec.containers[]`, `spec.initContainers[]`, `spec.ephemeralContainers[]`  
**Identity used:** OCI digest extracted from image reference — tags and names are not authoritative

**What is not currently implemented:**
- TLS certificate provisioning (must be handled externally by cert-manager or similar)
- Authentication of webhook requests (Kubernetes API server signs admission requests, but signature verification is not implemented in the current webhook handler)

### 3.11 Break-Glass Override System

**Purpose:** Emergency mechanism to temporarily allow a blocked image when no other resolution path is available.

**Threat mitigations:**
- Requires M-of-2 HMAC-SHA256 signatures from distinct approvers
- Approval expires after 1 hour if not executed
- All transitions are written to immutable `policy_audit_trail`
- A user submitting ≥3 override requests within 1 hour triggers a `POLICY_ABUSE` incident and is automatically blocked

**What is trusted:** HMAC secret (must be stored in a secrets manager)  
**What is vulnerable to:** Compromise of `SHIELD_HMAC_SECRET` — allows construction of valid forged signatures without possession of actual approver credentials

**Does not protect against:**
- Insider threat where two legitimate approvers collude to approve an unauthorized override
- Approver credential compromise — the HMAC secret must be rotated if compromise is suspected

### 3.12 Multi-Cluster Communication

**What is used:** NATS Core with wildcard subject subscriptions  
**Subject pattern:** `zeraxis.events.*.correlation.incident.resolved`

**What is validated:** Each cluster filters out events where `event.ClusterID == localClusterID` to prevent self-invalidation loops

**What is NOT guaranteed:**
- Events published during a network partition are permanently lost from the transport path
- There is no replay or catchup mechanism (JetStream would provide this)
- Cluster identity (`CLUSTER_ID`) is self-reported and not cryptographically verified

---

## 4. Threat Actor Categories

| Actor | Capability | ZERAXIS Response |
|-------|-----------|-----------------|
| Untrusted container image | Executes malicious code inside container | Falco detects anomalous syscalls; correlation raises incident; executor quarantines/stops container |
| Compromised registry tag | Redirects tag to malicious image | Mitigated: digest is authoritative, tag changes are ignored by supply-chain identity model |
| Unauthorized `docker run` | Attempts to start blocked image | Docker AuthZ plugin rejects at daemon level before container starts |
| K8s user deploying blocked pod | Attempts to schedule pod with blocked digest | ValidatingWebhook rejects pod at admission |
| Operator requesting excessive overrides | Attempts to bypass policy via break-glass abuse | 3-strikes elevation detector creates POLICY_ABUSE incident and blocks further requests |
| NATS impersonation (internal) | Publishes forged events to NATS | Mitigated in production by NKey auth; not mitigated in reference config |
| DB compromise | Direct manipulation of authorization tables | Append-only triggers prevent retroactive modification of ledger; PG audit can detect attempts |

---

## 5. What ZERAXIS Does Not Protect Against

- Container runtime escapes (out of scope)
- Host OS compromise
- Kernel exploits that silence eBPF probes
- Two legitimate approvers colluding on an unauthorized override
- Attacks that occur before discovery/supply-chain scanning is complete
- Network attacks within a shared Docker network (ZERAXIS observes, does not prevent network-layer attacks between containers)
- Supply-chain attacks that produce a clean vulnerability scan (zero-day with no CVE)
- Persistent threats that establish persistence before ZERAXIS is deployed
