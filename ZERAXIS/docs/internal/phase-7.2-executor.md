# Phase 7.2: Dedicated Executor Engine

## Overview
The Executor Engine is a standalone, isolated microservice responsible for translating explicitly authorized, identity-validated intents into typed Docker API mutations. 

## Trust Boundary
- **NATS** is exclusively a transport medium. The `zeraxis.events.execution.authorized` event serves merely as a wake-up signal.
- **PostgreSQL** is the strict authorization authority.
- **Recommendation** provides the proposed action and the immutable target identity (`target_asset_id`, `target_container_id`, `target_image_digest`).
- **Human Approval** is required.
- **Executor** performs the final validation and applies the mutation.

## Allowlist
Only typed operations: `ContainerStop` and `NetworkDisconnect`.

## Isolation
The Executor is the ONLY ZERAXIS component with MUTATION-capable access to `/var/run/docker.sock`. Phase 2 Discovery has `ro` (read-only) socket access for inventory building, preserving its documented behavior.

## Execution User Privilege
The Executor currently runs as `root` inside its alpine container because accessing a bind-mounted `/var/run/docker.sock` from a host requires the container user's GID to perfectly align with the host's `docker` group GID (which varies across platforms e.g. MacOS Docker Desktop vs Ubuntu vs Amazon Linux). To maintain lab portability without introducing complex entrypoint GID-mapping scripts, root is retained inside the container namespace, but the container runs with default capabilities (no `privileged: true`, no added CAPS).
