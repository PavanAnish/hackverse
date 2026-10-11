# ZERAXIS Phase 3: Supply Chain Security Engine

## Architecture: The Registry-Based Approach

In Phase 3, we introduce a dedicated Supply Chain Security Engine. To maintain a strict security boundary, this engine **does not have access to the Docker socket**. 

A supply-chain scanner should not require root-level host daemon access merely to perform static vulnerability analysis on filesystem layers.

To achieve this, ZERAXIS uses a standard enterprise workflow via a local OCI Registry:

```
[Host/Dev Machine]
  1. Build Image
  2. Push to Registry (127.0.0.1:5000)
       │
       ▼
[Local Registry:2 Container]
       │
       ▼
[Supply Chain Engine]
  3. Pull Image via OCI API
  4. Generate SBOM (Syft)
  5. Scan Vulnerabilities (Trivy)
  6. Evaluate Policy
  7. Persist to PostgreSQL
```

### Why this is more secure
1. **No Daemon Mutability**: The Supply Chain Engine cannot stop, delete, or exec into running lab containers.
2. **Blast Radius**: If the scanner engine is compromised via a maliciously crafted image (e.g., a vulnerability in Trivy/Syft parsing), the attacker only gains access to an isolated, unprivileged alpine container—not the host's Docker daemon.
3. **Reproducibility**: Images scanned are exactly the immutable digests stored in the registry, preventing "time-of-check to time-of-use" (TOCTOU) race conditions associated with local daemon cache rewriting.

## Domain Model & Graph

The engine establishes a strict graph mapping infrastructure to software dependencies:

```text
Asset (e.g., gateway container)
  │
  └──► Image (registry:5000/zeraxis/gateway@sha256:...)
         │
         ├──► SBOM (CycloneDX)
         │      │
         │      └──► Package (e.g., net/http 1.21.0)
         │              │
         │              └──► Vulnerability (CVE-XXXX-XXXX)
         │
         └──► Policy Decision (ALLOW/WARN/BLOCK)
```

- **Image Identity**: The primary identifier is the immutable SHA256 digest, not the mutable tag.
- **Deduplication**: If 5 assets run `gateway:latest`, there is only **one** Image record and **one** SBOM generated. 
- **Idempotency**: Scanning the same image digest twice updates the existing graph instead of duplicating it.

## Database Schema

We use PostgreSQL for persistence, carefully normalized to avoid duplication:
- `supply_chain_images`
- `supply_chain_asset_images` (mapping Assets to their active Image)
- `supply_chain_sboms`
- `supply_chain_packages`
- `supply_chain_sbom_packages` (many-to-many join)
- `supply_chain_vulnerabilities`
- `supply_chain_policy_decisions`
- `supply_chain_scans` (audit log of scan execution)

## Tooling & Policy

- **Syft**: Generates CycloneDX JSON SBOMs directly from the OCI registry interface.
- **Trivy**: Scans images for vulnerabilities directly from the OCI registry interface.
- **Policy Engine**: Deterministic policy evaluation. Example: CRITICAL = BLOCK, HIGH = WARN, MEDIUM = RECORD. The engine currently only calculates the decision; actual enforcement belongs to a future Response Engine.
