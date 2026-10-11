# ADR-010: Historical Risk Consistency

## Status
Proposed

## Context
Vulnerability contexts change over time (e.g., a CVE is patched in a new image digest, or a new CVE is published for an old image). We must ensure past incidents do not retroactively change their severity in confusing ways.

## Decision
Risk Scores and Incident Contexts are strictly immutable snapshots tied to the `image_digest` and active Findings at the time of calculation (`calculated_at`). The Risk Engine will emit explicitly versioned events (`RISK_CALCULATED`).

## Rationale
- If `Image D1` had a 95 Risk Score on Tuesday, and `Image D2` is deployed on Wednesday fixing the CVE, Tuesday's incident must still record that a 95 Risk event occurred.
