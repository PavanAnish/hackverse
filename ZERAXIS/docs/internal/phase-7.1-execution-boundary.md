# Phase 7.1: Execution Boundary Architecture

## 1. Boundary Definition
Phase 7 execution represents a strict boundary between analytical components (Phase 6) and infrastructure mutation (Phase 7.2+). The Execution Engine will be an entirely isolated, dedicated microservice acting as a restricted proxy to the Docker API.

## 2. Interaction Model
Correlation Engine -> PostgreSQL -> Analyst API -> (Human Approval) -> PostgreSQL -> Executor
- **Correlation Engine** NEVER talks to Docker.
- **Correlation Engine** NEVER talks to Executor directly.
- **Executor** NEVER subscribes to raw findings or runtime events.

The Executor strictly subscribes to a heavily governed, authorized intent stream.

## 3. Human Approval Boundary
The execution layer will NOT interpret arbitrary text. Execution must be authorized via explicit, structured state transitions within a new `execution_authorizations` database construct (or by extending `incident_recommendations`).
