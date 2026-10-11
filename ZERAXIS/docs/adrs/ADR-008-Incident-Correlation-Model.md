# ADR-008: Incident Correlation Model

## Status
Proposed

## Context
Phase 5 deduplicates identical findings into `UPDATED` events via fingerprints. Phase 6 must group non-identical findings into logical Incidents (e.g. `Shell Execution` + `Network Connection` + `File Write`). 

## Decision
Incidents will be correlated deterministically using a hierarchical key matrix:
`Primary Key`: `asset_id` + Time Window (e.g., 1 hour).
`Secondary Keys`: `container_id` (for tight temporal chains) and `image_digest` (for supply chain scope).
Findings on the same `asset_id` within a time window that share overlapping `container_id` or network egress relationships will be mapped to a single `IncidentID`.

## Rationale
- Using `asset_id` ensures that if an attacker kills a container and spins up a new one on the same host, the findings are grouped into the same Incident.
- Cross-asset correlation requires explicit network interaction evidence between the assets (which discovery engine maps via ports).
