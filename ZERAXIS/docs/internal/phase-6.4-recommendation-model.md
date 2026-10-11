# Recommendation Model

The Recommendation Model translates abstract risk into actionable advice deterministically.

## Policy Matrix
* **CRITICAL** (Confidence >= 90): Recommends `QUARANTINE_ASSET`, `REBUILD_IMAGE`, and `ESCALATE_INCIDENT`.
* **HIGH** (Confidence >= 70): Recommends `REDEPLOY_CLEAN_IMAGE` and `INVESTIGATE_PROCESS`.
* **MEDIUM**: Recommends `INVESTIGATE_PROCESS` and `REVIEW_VULNERABILITY`.
* **LOW/INFO**: Recommends `REVIEW_VULNERABILITY`.

## Confidence Gating
Recommendations are strictly suppressed if incident confidence drops below 40%. Disruptive recommendations (e.g., quarantine) require very high confidence.

## Fingerprinting & Idempotency
Recommendations use a deterministic SHA256 fingerprint: `incident_id + action + target_type + target_id + model_version`. When incident risk is recalculated, existing recommendations are updated (idempotent) rather than duplicated.
