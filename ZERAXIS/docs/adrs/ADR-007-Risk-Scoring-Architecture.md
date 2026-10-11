# ADR-007: Risk Scoring Architecture

## Status
Proposed

## Context
We need a deterministic model to quantify the security relevance of an incident without opaque ML/AI algorithms. Relying solely on CVSS scores is insufficient because it ignores runtime behavior (Exposure).

## Decision
Implement a segmented risk model: `CombinedRisk = (StaticRisk * StaticWeight) + (DynamicRisk * DynamicWeight)` capped at 100.
- **Static Risk:** Base CVE score + Asset Criticality + Policy Block status.
- **Dynamic Risk:** Derived from the Findings (e.g., Temporal sequence matches + Contextual matches + Confidence levels).
- **Decay:** Dynamic Risk naturally decays (or scales down) if no new findings are attached to the incident within 24 hours, whereas Static Risk persists as long as the vulnerability exists.

## Rationale
- Mathematical transparency is required for SecOps teams. 
- Distinguishing Static from Dynamic ensures that a critical CVE on a quiet container isn't ignored (high static), but a critical CVE actively being exploited triggers a CRITICAL incident (high static + high dynamic).
