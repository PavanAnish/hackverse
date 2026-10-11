import re

with open("engines/correlation/internal/repository/recommendations.go", "r") as f:
    content = f.read()

content = content.replace("ON CONFLICT (fingerprint) DO UPDATE SET", "ON CONFLICT (fingerprint) WHERE status IN ('PROPOSED', 'ACKNOWLEDGED', 'DISMISSED', 'APPROVED', 'MANUAL_INVESTIGATION_REQUIRED') DO UPDATE SET")
content = content.replace("ON CONFLICT (fingerprint) DO NOTHING", "ON CONFLICT (fingerprint) WHERE status IN ('PROPOSED', 'ACKNOWLEDGED', 'DISMISSED', 'APPROVED', 'MANUAL_INVESTIGATION_REQUIRED') DO NOTHING")

with open("engines/correlation/internal/repository/recommendations.go", "w") as f:
    f.write(content)

with open("engines/correlation/migrations/013_human_attestations.sql", "r") as f:
    content = f.read()

content = content.replace(
    "ALTER TABLE incident_recommendations DROP CONSTRAINT IF EXISTS incident_recommendations_fingerprint_key;",
    "ALTER TABLE incident_recommendations DROP CONSTRAINT IF EXISTS incident_recommendations_fingerprint_key;\nCREATE UNIQUE INDEX IF NOT EXISTS idx_recs_fingerprint_active ON incident_recommendations(fingerprint) WHERE status IN ('PROPOSED', 'ACKNOWLEDGED', 'DISMISSED', 'APPROVED', 'MANUAL_INVESTIGATION_REQUIRED');"
)

with open("engines/correlation/migrations/013_human_attestations.sql", "w") as f:
    f.write(content)

