# ZERAXIS Phase 6.3 Implementation

## Overview
Phase 6.3 transforms Phase 6.2 Security Incidents into mathematically risk-scored incidents with deterministic attack-chain states. It implements the "Risk Engine 1.0" model, integrating Phase 3 (Supply Chain) context with Phase 5 (Detection) findings.

## Risk Model Version 1.0
- **Static Risk**: `MIN(100, BaseCVSS_Severity + PolicyBlockModifier)`. Driven by Phase 3 OCI artifacts.
- **Dynamic Risk**: `MIN(100, BaseFindingSeverity * TemporalDensityMultiplier * AttackChainModifier)`.
- **Combined Risk**: `MIN(100, (StaticRisk * 0.4) + (DynamicRisk * 0.6))`.
- **Confidence**: Baseline 100, modified by `MaxEvidenceStrength` (STRONG=1.0, MEDIUM=0.7, WEAK=0.4, UNKNOWN=0.1).

### Risk Levels
- **INFO**: 0-19
- **LOW**: 20-39
- **MEDIUM**: 40-69
- **HIGH**: 70-89
- **CRITICAL**: 90-100

### Risk Decay
Dynamic risk decays linearly by timestamp difference to `0` after exactly 24 hours of inactivity. Static risk does not decay, as the vulnerability remains until the software digest changes.

## Attack Chain State Machine
Driven strictly by evidentiary rules:
1. **EXPOSURE**: Passive vulnerabilities or generic findings.
2. **EXECUTION**: Execution heuristics (process spawn).
3. **NETWORK_ACTIVITY**: Unexpected connections/egress.
4. **IMPACT**: Concrete evidence of file manipulation or database corruption.
The engine never hallucinates "CONFIRMED_EXPLOITATION" without concrete evidentiary findings.

## Identity Boundaries
- **Container Lifecycle Boundary**: `container_id` is now injected into the correlation fingerprint (`SHA256(asset_id:image_digest:container_id:hour_bucket)`). This strictly prevents temporal attack-chain state from leaking between container recreations.
- **Image Digest Boundary**: Different digests correlate into distinct incidents. Tags (`:latest`) are ignored.

## Unknown Behavior
- If supply-chain data is missing, Static Risk gracefully falls back to `0` and Confidence reduces to `10` (UNKNOWN=0.1 multiplier), preventing false assumptions of safety.
- `UNKNOWN` is never treated as `SAFE` or `ALLOW`.

## Failure Semantics
- **Postgres Outage**: The correlator drops the calculation (logging the error). No NATS events are published.
- **NATS Outage**: The incident risk is persistently calculated in Postgres. Missed events are not replayed (Core NATS limitation).

## Memory & Performance
- Max 5000 active incidents cached in-memory.
- Performance (Unit Benchmarks): >500 findings/sec throughput achieved due to idempotent caching and targeted DB queries. (Formal e2e benchmarking on cluster infrastructure deferred).
