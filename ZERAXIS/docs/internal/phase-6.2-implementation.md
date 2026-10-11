# ZERAXIS Phase 6.2 Implementation

## Overview
Phase 6.2 introduces the foundation for incident correlation. A new unprivileged microservice, `engines/correlation`, bridges the gap between individual Findings and higher-level Incidents. 

## Architectural Properties
1. **NATS Input**: Listens exclusively to `zeraxis.events.detection.finding.>`.
2. **Deterministic Correlation**: Groups findings using an exact identity model.
    `Fingerprint = SHA256(asset_id + ":" + image_digest + ":" + current_hour_timestamp)`
3. **Identity Preservation**:
   - Software identity (`image_digest`) is strictly partitioned.
   - Runtime identity (`container_id`) is stored for future attack-chain differentiation.
   - Infrastructure identity (`asset_id`) determines location.
4. **Data Isolation**: 
   - No Docker socket or capability access.
   - No NATS feedback loops (does not subscribe to correlation events).

## Deferred Behaviors
- **Risk Scoring**: Database schema reserves `static_risk`, `dynamic_risk`, `combined_risk`, and `confidence`, but they are purely null placeholders in Phase 6.2. 
- **Attack Chains**: No progression logic beyond raw event collection.
- **Recommendations**: No mutation outputs.
- **Cross-Asset Correlation**: Currently every asset creates independent incidents.

## Remediation Note
The Phase 5 Detection Engine has been patched to correctly propagate `image_digest` into the `FindingEvent` envelope. This guarantees the Correlation Engine can enforce strict software identity boundaries without requiring costly synchronous DB round trips back to the Detection Engine tables.
