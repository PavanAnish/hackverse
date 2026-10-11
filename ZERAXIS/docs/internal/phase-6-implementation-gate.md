# ZERAXIS Phase 6 Implementation Gate Report

## 1. Executive Summary
Phase 6.1 defines the rigid deterministic contracts for transitioning Phase 5 findings into Phase 6 incidents, evaluating their risk without opaque math, establishing an evidence-backed Attack Chain, and generating human-actionable non-executing recommendations. All models have been finalized and documented prior to execution.

## 2. Actual Repository Assessment
- **Phase 5**: Evaluates correctly via `runtime-engine`. Finds and emits `zeraxis.events.detection.finding.>`.
- **Phase 3**: Contains authoritative schema in `supply_chain_%` tables.
- **NATS**: Core NATS is employed. Replay is strictly ephemeral. Thus, a correlation engine restart may lose in-flight correlation context for missed NATS messages.

## 3. Architecture Decision
- Option A: Dedicated `engines/correlation` service chosen. Keeps stateful, memory-heavy windowing out of the high-speed Detection Engine.

## 4. Service Boundary
- **Consumes**: `zeraxis.events.detection.finding.>`
- **Publishes**: `zeraxis.events.correlation.incident.>`
- **Publishes**: `zeraxis.events.correlation.recommendation.created`

## 5. Correlation Model
- Dictated by `asset_id`, `image_digest`, temporal proximity (1-hour windows), and Discovery `NETWORK_MEMBERSHIP`. Documented in `docs/phase-6-correlation-model.md`.

## 6. Incident Model
- Fingerprinted by SHA256 hashes of temporal and asset properties. Stateful lifecycle (OPEN -> ACTIVE -> RESOLVED). Documented in `docs/phase-6-incident-model.md`.

## 7. Attack Chain
- Explicit, bounded vocabulary (EXPOSURE, EXECUTION, NETWORK, IMPACT) mapped to OBSERVED/INFERRED confidence. Documented in `docs/phase-6-attack-chain-model.md`.

## 8. Risk Model
- Evaluated as `MIN(100, (Static * 0.4) + (Dynamic * 0.6))`. Documented in `docs/phase-6-risk-model.md`.

## 9. Confidence Model
- Ranges 0-100. Derived directly from Evidence Strength (STRONG=100%, MEDIUM=70%, WEAK=40%, UNKNOWN=10%).

## 10. Historical Model
- Static Risk and Incident representations are tied to the exact `image_digest`. `D1` contexts never leak to `D2`. Risk scores contain a `risk_model_version` and `calculated_at` stamp to freeze historical accuracy.

## 11. State Management
- **Ephemeral**: Sliding correlation windows, deduplication buffers.
- **Durable**: Postgres tables (`incidents`, `incident_findings`).

## 12. NATS Model
- Subscribes exclusively to Phase 5 outputs. Publishes Phase 6 outputs. Feedback loops structurally prevented. Missing Core NATS persistence necessitates accepting partial temporal gap loss if the engine crashes.

## 13. Database Model
- No migrations created yet. Schema proposed in incident model document.

## 14. API Model
- Read-only endpoints: `GET /api/v1/incidents`, `/incidents/{id}`, `/incidents/{id}/timeline`, `/incidents/{id}/risk`, `/incidents/{id}/attack-chain`, `/incidents/{id}/recommendations`.

## 15. Recommendation Model & 16. Human Approval Boundary
- Non-executing JSON objects proposing actions. Strictly bounded. Documented in `docs/phase-6-recommendation-boundary.md`.

## 17. Phase 7 Boundary
- Phase 7 holds the exclusive mandate for infrastructure mutation via Docker sockets.

## 18. Security Model
- No privileged capabilities. Read-only DB contexts. Data never executed as instructions.

## 19. Threat Model
- Event Flooding mitigated by 5,000 active incident memory caps.
- NATS spoofing mitigated by implicit network boundaries.

## 20. Performance & 21. Memory Safety
- Correlation Latency < 100ms.
- 5,000 active incidents cached in an LRU eviction map to prevent OOMs.

## 22. Failure Semantics
- **NATS Down**: Degrades to Phase 4 semantics.
- **Supply Chain Down**: Context defaults to UNKNOWN. Does not hallucinate SAFE.
- **Restart**: Reconstructs state from Postgres, dropping ephemeral NATS events missed during downtime.

## 23. Testing Strategy & 24. Adversarial Testing
- Explicit matrix tests checking missing SBOMs, duplicated events (1M finding blast), and cross-asset hallucination limits.

## 25. Observability
- Metrics: `incident_creation_total`, `risk_calculation_latency`, `state_evictions`.

## 26. ADRs
- All 7 architectural decision records are generated and immutable.

## 27. Implementation Order
1. Phase 6.1 Database Models and Migrations.
2. Phase 6.2 Correlation Engine Skeleton & NATS Bindings.
3. Phase 6.3 Risk Math and Attack Chain State Machine.
4. Phase 6.4 API Layer.
5. Phase 6.5 Full Regression suite.

## 28. Known Limitations
- Core NATS lacks durable replay for offline subscribers, resulting in possible correlation window breakage on restarts.

## 29. MUST FIX BEFORE IMPLEMENTATION
- None.

## 30. SHOULD FIX LATER
- Introduce NATS JetStream for durable subscriber replays.

## 31. Final Feasibility Decision
**READY FOR PHASE 6 IMPLEMENTATION**
