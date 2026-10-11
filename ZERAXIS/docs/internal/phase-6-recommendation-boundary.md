# ZERAXIS Phase 6 Recommendation Boundary

## Objective
Phase 6 produces Recommendations derived from Incidents. It does NOT execute them.

## Recommendation Model
```json
{
  "recommendation_id": "uuid",
  "incident_id": "uuid",
  "action_type": "QUARANTINE_ASSET",
  "reason": "CRITICAL combined risk (92) on payment-service with observed Execution -> Network chain.",
  "risk_score": 92,
  "confidence": 85,
  "expected_benefit": "Terminates active C2 exfiltration.",
  "potential_impact": "Service disruption for active payment gateway transactions.",
  "required_authorization": "HUMAN_APPROVAL",
  "status": "RECOMMENDATION_CREATED"
}
```

## Safety Enforcement
- Phase 6 runs unprivileged.
- No Docker socket (`/var/run/docker.sock`) is mounted.
- No network capabilities (`NET_ADMIN`) are granted.
- The engine simply publishes `zeraxis.events.correlation.recommendation.created` to NATS.
- Phase 7 (Human Approval / Response Engine) will consume these at a later time.
