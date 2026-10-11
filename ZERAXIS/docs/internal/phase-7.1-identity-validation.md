# Phase 7.1: Identity Validation

## Principle: Stale Target -> Abort Execution

The execution layer must securely prove the target being mutated is the exact same target that generated the recommendation.

### Required Identity Chain
1. `recommendation_id`
2. `asset_id`
3. `container_id` (12-char or 64-char OCI standard)
4. `image_digest` (SHA256)

### Validation Logic
Before mutating, the Executor MUST query Discovery/Docker API:
1. Does `container_id` exist? (If no -> ABORT)
2. Does `container_id` map to `asset_id`? (If no -> ABORT)
3. Is `container_id` running `image_digest`? (If no -> ABORT)

### Scenario Tests
- **D1 -> D1**: Proceed to execution.
- **D1 -> D2**: Container was upgraded/restarted with a new image. ABORT.
- **D1 -> unknown**: Digest unavailable. ABORT.
- **missing digest**: Execution on untrackable images is forbidden. ABORT.
- **C1 stops, C2 replaces**: `container_id` changed. ABORT.
- **C1 restarts**: If container ID is the same and digest is the same, proceed. (Docker preserves container ID on stop/start).
