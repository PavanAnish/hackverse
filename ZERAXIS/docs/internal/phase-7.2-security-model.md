# Phase 7.2: Security Model

## Privilege
The Executor operates with minimal necessary capabilities. It lacks `os/exec`, `bash`, and `docker CLI` binary access. It exclusively leverages the Go Docker SDK to issue explicit API calls. 

## Target Validation (The Invariant)
To prevent TOCTOU flaws and unintended collateral damage, execution enforces the strict invariant:
1. `recommended asset == current asset`
2. `recommended container == current container`
3. `recommended digest == current digest`

If a container restarts with a new image, the digest diverges. If a container is replaced, the ID diverges. Both safely ABORT the execution.

## Null-Target Safety
If any of `target_asset_id`, `target_container_id`, or `target_image_digest` are NULL, execution fails closed. The Executor never infers missing identity from unstructured JSON metadata.

## Atomic Claim
The Executor utilizes a `SELECT FOR UPDATE` transaction to atomically claim an `APPROVED` authorization row, moving it to `EXECUTING`.
