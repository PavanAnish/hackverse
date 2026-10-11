# Phase 7.2: Execution Flow

## 1. Request
NATS event `zeraxis.events.execution.authorized` signals the Executor.

## 2. Authorization (PostgreSQL)
Executor establishes a transaction: `SELECT FOR UPDATE` on `execution_authorizations`.
It validates the status is `APPROVED` and has not expired (> 24h).

## 3. Incident State Validation
Executor checks `incidents` table to ensure the incident remains `OPEN` or `ACTIVE` and risk is above threshold.

## 4. Incomplete Target Check
Checks that `target_asset_id`, `target_container_id`, and `target_image_digest` are NOT NULL.

## 5. Current State Validation (Docker)
Executor inspects the running container via Docker API to assert:
`Current Container ID == Target Container ID`
`Current Image Digest == Target Image Digest`

## 6. Execution Claim
Status transitions to `EXECUTING`. Transaction commits.

## 7. Typed Mutation
Executor invokes `ContainerStop` or `NetworkDisconnect`.

## 8. Audit Event
Result is written to PostgreSQL (`EXECUTED` or `FAILED`), and a NATS event (`EXECUTION_SUCCEEDED` / `EXECUTION_FAILED`) is broadcasted for timeline aggregation.
