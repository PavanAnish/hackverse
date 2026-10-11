# Security Policy

## Project Status

ZERAXIS is an **engineering and security research project** — a reference implementation of a closed-loop security automation platform. It is not a commercially supported product. Before deploying in any environment, review all security considerations in this document and in [`docs/THREAT_MODEL.md`](docs/THREAT_MODEL.md).

---

## Reporting a Vulnerability

If you discover a security vulnerability in ZERAXIS, please report it through **GitHub Security Advisories** rather than opening a public issue.

**How to report:**

1. Navigate to the repository on GitHub.
2. Click **Security** → **Advisories** → **Report a vulnerability**.
3. Provide a clear description, reproduction steps, and potential impact.

Maintainers will acknowledge the report within **72 hours** and provide an initial assessment within **7 days**. Critical vulnerabilities will be prioritised.

**Please do not** disclose vulnerability details publicly until a fix has been coordinated.

There is no formal bug bounty programme for this project.

---

## Supported Versions

| Version | Supported |
|---------|-----------|
| `main` (latest) | ✅ Active |
| Earlier commits | ❌ No backport support |

This project is at **v0.1.0** (pre-production). There is no stable release branch yet. All fixes land on `main`.

---

## Known Security Limitations

The following are intentional design limitations or known gaps. They are documented here so operators can make informed risk decisions.

### 1. Docker Socket Access

The Discovery Engine and Executor Engine mount the Docker socket (`/var/run/docker.sock`). Any process with access to this socket has effective root on the Docker host. This is a well-known Docker architecture constraint.

**Mitigation:** These engines are isolated from external networks. They run inside the `zeraxis-lab` Docker network with no exposed ports to untrusted networks. The Discovery Engine uses `:ro` (read-only socket mount). The Executor Engine requires write access to execute remediation actions.

### 2. Falco Privileged Sensor

Falco requires elevated Linux capabilities (`SYS_ADMIN`, `BPF`, `PERFMON`, `SYS_RESOURCE`, `SYS_PTRACE`) to attach eBPF programs to the kernel. ZERAXIS does **not** use `privileged: true` — it uses explicit capability grants only. However, the combination of these capabilities provides significant host access. Review Falco's security documentation before deployment.

### 3. NATS Core — No Authentication by Default

The reference `docker-compose.yml` runs NATS without authentication. This is intentional for local lab use only. Before any multi-host or production deployment, configure NATS with NKey authentication or TLS mutual authentication.

### 4. NATS Core — Non-Durable Delivery

ZERAXIS uses NATS Core (not JetStream). Events published during subscriber downtime are permanently lost from the transport path. The database is the durable source of truth, but the NATS-to-DB gap is not atomic. See [`docs/FAILURE_SEMANTICS.md`](docs/FAILURE_SEMANTICS.md).

### 5. PostgreSQL Credentials

The reference `docker-compose.yml` and test configuration use `postgres:postgres` credentials. These are for local development only. Any deployment must use strong, rotated credentials, and the database must not be exposed outside the internal network.

### 6. HMAC Secret for Break-Glass

The break-glass override system uses HMAC-SHA256 to validate approver signatures. The shared secret (`SHIELD_HMAC_SECRET`) must be stored in a secrets manager (Vault, AWS Secrets Manager, K8s Secret) and rotated regularly. Compromise of this secret allows construction of valid forged signatures.

### 7. No External Gateway Authentication

The API Gateway in the `services/` directory is a demo reverse proxy for the target microservices. It does not enforce strong authentication on inbound requests. This is intentional — ZERAXIS monitors these services; it is not responsible for their authentication.

### 8. Kubernetes Webhook Scope

The K8s `ValidatingWebhookConfiguration` is cluster-wide. TLS between the API server and the webhook service must be configured correctly. The reference configuration in this repository does not include TLS certificate generation. Production deployments must use cert-manager or equivalent.

### 9. Docker Desktop / macOS Scope

Falco with eBPF requires a Linux kernel. On macOS with Docker Desktop, Falco runs inside the LinuxKit VM (kernel `5.15+` or `6.x` depending on Docker Desktop version). Some eBPF features behave differently in this environment. The runtime engine has only been tested in this context. Production Linux deployment may require additional Falco driver configuration.

### 10. No Replay / Catchup After Partition

If a cluster's Shield engine is disconnected from NATS and reconnects, it will not receive resolution events that were published during the disconnection. Manual cache reconciliation is required. See the operations manual for the reconciliation procedure.

---

## Trust Boundary Summary

| Boundary | Trusted? | Notes |
|----------|----------|-------|
| PostgreSQL | Trusted | Must not be exposed externally |
| NATS (internal) | Trusted | No auth in reference config; add NKey for any external exposure |
| Docker socket | Trusted | Treat as root access |
| OCI registry | Partially trusted | Digest is content-addressed; tag resolution is untrusted |
| Kubernetes API server | Trusted | Webhook must use TLS |
| Inbound HTTP (admission) | Untrusted | All inputs validated; OCI digest is authoritative identity |

---

## Security-Sensitive Code Areas

If contributing to ZERAXIS, treat the following areas with extra care:

- `engines/executor/` — Docker SDK execution logic and identity revalidation
- `engines/shield/internal/breakglass/` — HMAC validation and approval threshold logic
- `engines/shield/internal/admission/` — admission decision logic
- `engines/shield/internal/evaluator/` — OPA policy loading and evaluation
- `engines/correlation/internal/` — incident lifecycle and authorization gating
- `engines/correlation/migrations/` — schema changes affecting audit ledger immutability
- `pkg/events/` — NATS subject routing logic
