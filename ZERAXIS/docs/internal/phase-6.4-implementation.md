# Phase 6.4 Implementation

Phase 6.4 implements the ZERAXIS Recommendation Engine and Analyst API within the Correlation Engine.

## Overview
Phase 6.4 extends the Phase 6.3 Risk Engine by automatically translating risk and attack-chain states into concrete, human-actionable recommendations.

**Crucially, Phase 6.4 generates recommendations ONLY. It does not execute remediation.**

## Architecture
- **Recommender Model:** Deterministic policy-based matrix evaluating incident Risk Level and Confidence.
- **Data Persistence:** Recommendations are durable in PostgreSQL (`incident_recommendations`) with UPSERT semantics based on deterministic fingerprints.
- **NATS Publication:** `RECOMMENDATION_CREATED` and `RECOMMENDATION_UPDATED` events are emitted using the Unified Event Envelope only *after* successful durable persistence.
- **API Endpoints:** Extended the existing REST API to include `/recommendations`, `/attack-chain`, and a correlated `/timeline`.

## Security Boundary
The Recommendation Engine maintains a strict analytical separation. Recommendations are purely structured data representations of suggested actions (e.g., `QUARANTINE_ASSET`). No infrastructure mutation paths, Docker socket bindings, or shell executions exist within this layer.
