# ADR-011: Response Recommendation Safety Boundary

## Status
Proposed

## Context
Phase 6 identifies critical risks but must NOT execute remediation (e.g. `docker kill`). 

## Decision
Phase 6 will output non-executing `RECOMMENDATION_CREATED` events containing `recommendation_id`, `reason`, `expected_benefit`, and `required_authorization`. 
It will physically lack the Docker socket mounts and RBAC permissions required to enforce the actions.

## Rationale
- Maintains the "Human Approval" boundary. Phase 6 serves as the Security Analyst, not the enforcer.
