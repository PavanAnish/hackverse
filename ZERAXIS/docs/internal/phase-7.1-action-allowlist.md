# Phase 7.1: Action Allowlist

## Strict Allowlist Philosophy
Phase 7 will execute ONLY explicitly typed actions. There will be NO `EXECUTE_COMMAND`, `DOCKER_EXEC`, `RAW_SHELL`, or `BASH_SCRIPT` capabilities.

### 1. QUARANTINE_ASSET
- **Purpose:** Disconnect a compromised container from the network.
- **Required Identity:** Asset ID, Container ID, Image Digest.
- **Required Approval:** Human Analyst.
- **Required Confidence:** >= 90 (HIGH).
- **Required Risk Level:** HIGH or CRITICAL.
- **Target Type:** container.
- **Allowed Docker Operation:** `docker network disconnect` API equivalent.
- **Forbidden Parameters:** Arbitrary commands.
- **Rollback:** `docker network connect` API equivalent.
- **Failure Behavior:** Fail closed. Retry with backoff if network partition; abort if container missing.

### 2. STOP_CONTAINER
- **Purpose:** Halt a container actively executing an attack chain.
- **Required Identity:** Asset ID, Container ID, Image Digest.
- **Required Approval:** Human Analyst.
- **Required Confidence:** >= 90.
- **Target Type:** container.
- **Allowed Docker Operation:** Container Stop API.
- **Rollback:** Container Start API (note: ephemeral state is lost).

*(Other actions like ISOLATE_NETWORK may be added via this strict definition template)*
