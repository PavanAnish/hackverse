# ADR 002: Docker Engine API vs CLI Subprocesses for Discovery

## Status
Accepted

## Context
Phase 2 requires an Asset Discovery Engine to detect running containers, ports, networks, and metadata in the infrastructure. We needed to choose the mechanism by which the Go-based discovery engine retrieves this information from the Docker daemon.

The primary alternatives were:
1. Shell out to the `docker` CLI (e.g., `exec.Command("docker", "ps", "--format", "{{json .}}")`).
2. Use the official Docker Engine API client library for Go (`github.com/docker/docker/client`).

## Decision
We will use the **Docker Engine API client library**.

## Reasoning
- **Type Safety**: The API client provides strongly typed Go structures for container metadata, eliminating brittle JSON/text parsing of CLI output.
- **Performance**: Communicating directly over the Unix socket (`/var/run/docker.sock`) via HTTP avoids the overhead of spawning hundreds of subprocesses in a continuous discovery loop.
- **Granularity**: The API provides deeper metadata (like `NetworkSettings.Networks` and `HostConfig.PortBindings`) that is difficult to reliably extract and correlate using only `docker ps` or `docker inspect` via CLI.
- **Error Handling**: The API client provides structured error returns (e.g., distinguishing between a network error and a missing container) rather than relying on exit codes and stderr parsing.

## Trade-offs & Security Implications
- **Security Boundary**: Providing the discovery engine with the Docker socket grants it implicit root-level access to the host machine. While we mount the socket with the `:ro` flag, this only prevents the socket file from being deleted; it does *not* restrict the commands sent to the Docker API. 
- **Enforcement**: To mitigate this, the read-only restriction is enforced strictly at the code level. Our `internal/docker` package deliberately wraps only read operations (`ContainerList`, `ContainerInspect`) and exposes no write operations (`ContainerStop`, `ContainerKill`, etc.).
- **Dependency size**: Including the Docker SDK pulls in several transitive dependencies, increasing the binary size slightly, which is an acceptable trade-off for robustness.
