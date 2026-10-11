# Phase 7.1: Testing Matrix

## Adversarial & Functional Test Plan

| ID | Name | Condition | Expected Result (Fail-Closed) |
|---|---|---|---|
| T1 | Valid approval | All identity checks pass, TTL valid. | EXECUTION SUCCEEDS |
| T2 | Missing approval | `approved_at` is null. | ABORT |
| T3 | Expired recommendation | Recommendation age > 24h. | ABORT |
| T4 | Revoked approval | State transitioned to REVOKED before pickup. | ABORT |
| T5 | Wrong asset | Target asset ID mismatch. | ABORT |
| T6 | Wrong container | Target container ID mismatch. | ABORT |
| T7 | Wrong image digest | `image_digest` mismatch. | ABORT |
| T8 | Container replacement| New container spawned with same name/image. | ABORT (Container ID differs) |
| T9 | Concurrent execution | Two executor workers fetch same job. | ONE executes, ONE aborts (DB lock) |
| T10| Duplicate execution | Replay NATS approval event. | ABORT (Idempotency key exists) |
| T11| Docker unavailable | API socket down. | FAIL / RETRY |
| T12| PostgreSQL down | DB connection lost. | ABORT (Cannot verify state) |
| T13| NATS unavailable | Event bus down. | ABORT (Cannot broadcast audit) |
| T14| Malformed payload | Missing fields in NATS message. | ABORT |
| T15| Forged recommendation| `recommendation_id` not in DB. | ABORT |
| T16| Forged approval | DB shows PROPOSED, NATS says APPROVED. | ABORT |
| T17| Shell injection | Metadata contains `$(rm -rf /)`. | IGNORED (Typed API only) |
| T18| SQL injection | Payload contains `'; DROP--`. | IGNORED (Parameterized SQL) |
| T19| Recommendation replay| Re-sending old executed rec. | ABORT |
| T20| Executor restart | Crash mid-execution. | FAIL / MANUAL RECOVERY |
| T21| Partial execution | Half of a multi-step action fails. | FAIL / MANUAL RECOVERY |
| T22| Rollback failure | `docker network connect` fails. | ALERT ANALYST |
