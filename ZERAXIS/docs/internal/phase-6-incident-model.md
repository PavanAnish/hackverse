# ZERAXIS Phase 6 Incident Model

## Data Contracts

### 1. Incident Identity
`incident_fingerprint = SHA256(asset_id + image_digest + primary_correlation_class + hour_bucket)`
The fingerprint guarantees idempotency if the NATS consumer crashes and replays findings.

### 2. Incident Lifecycle
1. **OPEN**: Initial finding mapped to new incident.
2. **ACTIVE**: Additional findings continually append within the active window.
3. **RESOLVED**: Operator marks as addressed.
4. **CLOSED**: Incident archived.
*Reopening*: If a RESOLVED incident receives new findings mapped to its fingerprint within 24 hours, it transitions to ACTIVE. Outside 24 hours, a NEW incident is generated.

### 3. Database Representation (Proposed)
```sql
CREATE TABLE incidents (
    incident_id TEXT PRIMARY KEY,
    fingerprint TEXT UNIQUE NOT NULL,
    asset_id TEXT NOT NULL,
    image_digest TEXT,
    status TEXT NOT NULL,
    static_risk INTEGER NOT NULL,
    dynamic_risk INTEGER NOT NULL,
    combined_risk INTEGER NOT NULL,
    confidence INTEGER NOT NULL,
    risk_model_version TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE incident_findings (
    incident_id TEXT REFERENCES incidents(incident_id),
    finding_id TEXT REFERENCES findings(finding_id),
    PRIMARY KEY(incident_id, finding_id)
);
```
