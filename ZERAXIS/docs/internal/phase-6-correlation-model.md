# ZERAXIS Phase 6 Correlation Model

## Deterministic Grouping Constraints
Incidents are grouped using strict identity and temporal constraints. 

### 1. Primary Constraints (Same Asset)
Findings correlate into a single Incident IF:
- `asset_id` matches EXACTLY.
- `image_digest` matches EXACTLY (for supply chain contexts).
- Arrival falls within an active 1-hour temporal window (sliding window, extended by max 24 hours per incident).

### 2. Tight Progression (Same Container)
Findings spanning `Download -> Execute -> Network` correlate IF:
- `container_id` matches EXACTLY.

### 3. Cross-Asset Correlation
Findings from distinct `asset_id`s correlate IF AND ONLY IF:
1. The Discovery Engine provides a `NETWORK_MEMBERSHIP` active link between Asset A and Asset B.
2. The network egress from Asset A temporally aligns (within 5 seconds) with network ingress/execution on Asset B.
3. Attack chain progression is logically compatible (Asset A: Network Egress -> Asset B: Initial Access).

### 4. False Correlation Protections
- Two unrelated containers on the same host producing `Shell Execution` simultaneously DO NOT correlate unless they share the same `image_digest` (widespread supply chain attack) or network interaction.
- `asset_id`, `network`, `image`, and `timestamp` sharing is insufficient without causal event lineage.
