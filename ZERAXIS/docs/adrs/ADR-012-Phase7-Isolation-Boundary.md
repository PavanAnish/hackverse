# ADR-012: Phase 7 Isolation Boundary

## Status
Proposed

## Context
Future Phase 7 will perform autonomous response. We must design Phase 6 to cleanly interface with it without overlapping scopes.

## Decision
Phase 6 acts solely as a NATS Publisher of `RECOMMENDATION_CREATED` events. Phase 7 will be a distinct physical service (`engines/response`) that subscribes to recommendations, handles human/policy authorization, and executes infrastructure changes via the Docker socket.

## Rationale
- Strict separation of Assessment (Phase 6) and Enforcement (Phase 7).
