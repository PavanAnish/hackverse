# ZERAXIS Phase 6 Attack Chain Model

## Deterministic Stages
The engine strictly uses the following explicit stages to avoid hallucinating unproven MITRE techniques.

1. **EXPOSURE**: Passive vulnerabilities or policy blocks.
2. **EXECUTION**: Unexpected processes, shell drops, or known vulnerable binaries executing.
3. **NETWORK_ACTIVITY**: Unauthorized egress/ingress from execution contexts.
4. **IMPACT**: File manipulation, database corruption, or system modification.

## Stage Evidence Qualification
Every active stage MUST specify an evidence confidence level:
- **OBSERVED**: Backed by a STRONG finding (e.g. `FILE_ACTIVITY` event).
- **INFERRED**: Heuristically derived (e.g. gap between Execution and Network Egress without direct parent-child process tracing).
- **POSSIBLE**: Based on WEAK supply chain mappings.
- **UNKNOWN**: Data missing.

Exploitation vocabulary is strictly bounded. The engine NEVER outputs `CONFIRMED_EXPLOITATION`. It outputs `EXPOSED`, `SUSPICIOUS`, `BEHAVIORALLY_RELEVANT`, and `POTENTIALLY_COMPROMISED`.
