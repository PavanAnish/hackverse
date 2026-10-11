# ZERAXIS Phase 2: Asset Discovery Engine

## Architecture
The Asset Discovery Engine is a standalone Go service that continuously polls the Docker Engine API to maintain an inventory of running infrastructure. 

```
Docker Engine Socket (/var/run/docker.sock)
       │
       ▼ (Read-Only)
┌───────────────────────┐
│  Discovery Engine     │
│  - Scanner            │
│  - Reconciler         │
│  - Postgres Repo      │
│  - HTTP API           │
└─────────┬─────────────┘
          │ (Upserts)
          ▼
     PostgreSQL
```

## Docker API Usage & Security Boundary
The discovery engine interacts with the Docker Engine API via the `github.com/docker/docker/client` Go library. 
It requires mounting the Docker socket (`/var/run/docker.sock:/var/run/docker.sock:ro`) into the container.

**Security Consideration**: Mounting the Docker socket is a highly privileged operation. Even with the `:ro` flag, the Docker API itself does not distinguish between read and write requests over the socket. The read-only enforcement is done entirely at the application code level by the discovery engine's internal `docker` package, which explicitly does not implement any write operations (no stop, rm, create, etc.).

## Asset Model
Assets are identified by a stable UUID derived from the container name. This ensures that when a container is recreated (e.g. `docker compose up --build`), it retains the same `asset_id` and history, rather than being treated as a completely new asset.

The engine collects:
- Container metadata (ID, Image, Status, Labels, Timestamps)
- Port bindings (distinguishing internal container ports from host-published ports)
- Network memberships (IP addresses and gateways within Docker networks)

> [!NOTE]
> **Self-Discovery**: Because the Discovery Engine runs as a container within the Docker environment it is monitoring, it will naturally discover itself and add itself to the inventory as a tracked asset. This is intentional behavior, as the security platform needs visibility into its own components.

## Relationship Model
In Phase 2, the engine discovers `NETWORK_MEMBERSHIP` relationships. These are pairwise connections created between any two assets that share a Docker network. 

**Important Distinction**: Sharing a network only establishes *potential* connectivity. It does NOT indicate that actual application-level communication was observed between the assets. True observed communication relationships belong to the runtime monitoring phase (Phase 5).

## Database Schema
All discovery data is persisted to PostgreSQL. To avoid conflicts with the Phase 1 lab services, all discovery tables are prefixed with `discovery_`.
- `discovery_assets`
- `discovery_asset_ports`
- `discovery_asset_networks`
- `discovery_relationships`
- `discovery_events`

## Discovery Algorithm
The engine runs a loop every `DISCOVERY_INTERVAL` (default 30s):
1. **Scan**: Calls Docker API `ContainerList(All=true)` and `ContainerInspect` for each container.
2. **Reconcile**: Compares the scanned state against the stored state in PostgreSQL.
3. **Persist**: Upserts the current state into the database.
4. **Events**: Generates `ASSET_DISCOVERED`, `ASSET_UPDATED`, `ASSET_REMOVED`, `NETWORK_RELATIONSHIP_CREATED`, and `PORT_DISCOVERED` events for any changes detected during reconciliation.

## macOS / Docker Desktop Notes
On macOS, Docker Desktop provides the Linux VM and Docker daemon. The discovery engine container runs within this VM and accesses the socket locally. No TCP socket exposure (e.g., `tcp://0.0.0.0:2375`) is required or recommended.

## Known Limitations
- NATS Event Bus is not yet implemented (Phase 4). Events are currently just JSON logs and DB rows.
- No vulnerability scanning or SBOM parsing yet (Phase 3).
- HTTP API is unauthenticated and meant for local development/internal access only. Port 8090 must not be exposed to untrusted networks.
