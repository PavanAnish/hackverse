# ADR-009: Attack-Chain Representation

## Status
Proposed

## Context
We need a way to visualize the progression of an incident without hallucinating unproven MITRE ATT&CK techniques.

## Decision
The Attack Chain will be an explicit finite state machine consisting of deterministic stages:
`EXPOSURE -> RECON -> INITIAL_ACCESS -> EXECUTION -> PERSISTENCE -> NETWORK_ACTIVITY -> IMPACT`

Each Stage must explicitly declare its Confidence: `OBSERVED`, `INFERRED`, `POSSIBLE`, `UNKNOWN`.
- `OBSERVED`: Supported by a STRONG Finding.
- `INFERRED`: Supported by a MEDIUM/WEAK Finding or temporal gap filling.

## Rationale
- Honesty in telemetry: We cannot claim "CONFIRMED EXPLOITATION" without kernel payload inspection, but we can claim "OBSERVED EXECUTION" and "OBSERVED NETWORK_ACTIVITY".
