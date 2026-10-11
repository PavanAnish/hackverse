# ZERAXIS Hackathon Demo Guide

This guide describes how to run the interactive hackathon demonstration of the ZERAXIS pipeline. The demo runs against the real ZERAXIS stack without fabricating events or modifying core behavior.

## Overview

The demonstration exercises the full ZERAXIS lifecycle:
1. **Target Creation**: Provisions an isolated `payment-service-demo` container.
2. **Discovery**: Waits for the ZERAXIS Discovery engine to inventory the asset.
3. **Execution**: Simulates an attacker modifying critical system files (`/etc/hack.txt`) multiple times.
4. **Pipeline Execution**:
   - Falco detects the file writes (`ZERAXIS_FILE_WRITE`).
   - Runtime Normalizer standardizes the events.
   - Detection Engine evaluates CEL rules and flags `rule-system-file-write`.
   - Correlation Engine correlates the findings into an incident.
   - Risk Model calculates a `CRITICAL` dynamic risk score due to recurring violations.
   - Recommendation Engine prescribes `QUARANTINE_ASSET`.
5. **Authorization**: Submits an approval to the ZERAXIS Gateway API.
6. **Execution & Verification**: Executor disconnects the container from the network. The system verifies the container network state and marks the incident `RESOLVED`.

## Setup Requirements

Ensure that ZERAXIS is already running. You must run the demo from a machine with access to the Docker daemon.

```bash
cd infrastructure/docker
# Start the full stack (if not already running)
docker compose up -d
```

## Running the Demo

Execute the demo harness. The script will inject the necessary NATS configuration specifically for the `executor-engine` to allow `QUARANTINE_ASSET` to function properly, wait for discovery, inject suspicious behavior, and display a progressive real-time terminal UI as the actual engines process the event.

```bash
./scripts/demo/run_demo.sh
```

## Cleanup

The cleanup script strictly removes only the `payment-service-demo` container and restores the executor configuration to its default state.

```bash
./scripts/demo/cleanup_demo.sh
```

## How It Works Under The Hood

- **No Faked Events**: The demo script strictly queries the actual PostgreSQL database to read the state of Findings, Incidents, Recommendations, and Executions.
- **Dynamic Authorization**: The demo harness programmatically calls the `Gateway` endpoint `POST /api/v1/recommendations/{id}/approve` with HTTP Basic Auth.
- **Container Quarantine**: To satisfy the executor's `QUARANTINE_ASSET` requirement, a temporary override (`scripts/demo/docker-compose.demo.yml`) grants the executor authorization to isolate containers from the `zeraxis-lab` network during the demo.
