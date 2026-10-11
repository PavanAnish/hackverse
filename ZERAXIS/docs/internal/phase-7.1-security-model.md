# Phase 7.1: Security Model

## Future Executor Architecture
The Executor will be a standalone, hardened microservice. 
- It is the ONLY service permitted to mount `/var/run/docker.sock` in Phase 7.
- The Correlation, Detection, Runtime, and Discovery engines remain unprivileged.
- The Executor acts as a heavily restricted API proxy. It only binds minimal Go Docker SDK functions (e.g., `ContainerStop`, `NetworkDisconnect`).

## Command Execution Boundary
Recommendations are purely structured data. The Executor performs typed API operations. NO recommendation metadata, reason string, or identity field will ever be passed to `os/exec`, `bash -c`, or `docker exec`.

## NATS Topology
- Subscribe: `zeraxis.events.execution.authorized`
- Publish: `zeraxis.events.execution.started`, `zeraxis.events.execution.completed`, `zeraxis.events.execution.failed`
- Loop prevention: The Executor never subscribes to its own output. Correlation engine listens for execution results to append to incident timelines.

## Failure Semantics (Fail Closed)
- **PostgreSQL unavailable:** Abort/Pause execution (cannot verify authorization).
- **NATS unavailable:** Execute and buffer logs, or abort if strict audit is required.
- **Docker unavailable:** Fail and retry with backoff.
- **Target missing/changed:** Abort and mark `FAILED_STALE_TARGET`.
- **Approval revoked mid-flight:** Abort if not yet interacting with Docker API.

## Idempotency
- **Idempotency Key:** `execution_id` or `SHA256(recommendation_id + action + target_container_id)`.
- Concurrent workers will rely on PostgreSQL row-level locks (`SELECT FOR UPDATE`) or optimistic concurrency control on the `executions` table to prevent double-execution.
