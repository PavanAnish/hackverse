# ADR-006: Phase 6 Service Boundary

## Status
Proposed

## Context
Phase 5 introduced a Detection Engine that evaluates runtime events, correlates them temporally (Stage 2B), and enriches them contextually (Stage 2C) to produce Findings. Phase 6 must now take these Findings and aggregate them into Incidents, calculate Risk, identify Attack Chains, and produce Recommendations. The question is whether to build this into the Detection Engine or create a dedicated Correlation/Risk Engine.

## Decision
**Option A: New dedicated Risk/Incident Engine (Correlation Engine).**
We will build a separate `engines/correlation` microservice. It will subscribe to `zeraxis.events.detection.finding.>` via NATS.

## Rationale
- **Separation of Concerns:** Detection focuses on high-throughput (500/sec) stateless and localized temporal evaluation. Risk/Incident evaluation operates on a lower-frequency stream (Findings) but requires heavy state management (Incident lifecycle, Attack Chain state, Recommendations).
- **Fault Isolation:** If the Risk Engine fails, the Detection Engine must continue generating Findings and persisting them to Postgres.
- **Memory Boundaries:** Keeping the massive state of active Incidents separate prevents OOMs in the high-speed Detection Engine.
- **Topology:** The Correlation Engine will publish `INCIDENT_CREATED/UPDATED`, `RISK_CALCULATED`, and `RECOMMENDATION_CREATED` back to the Unified Event bus, providing a clean Phase 7 entry point.
