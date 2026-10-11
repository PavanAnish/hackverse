with open("engines/correlation/migrations/013_human_attestations.sql", "r") as f:
    content = f.read()

content = content.replace(
    "ALTER TABLE incidents DROP CONSTRAINT IF EXISTS incidents_fingerprint_key;",
    "ALTER TABLE incidents DROP CONSTRAINT IF EXISTS incidents_fingerprint_key;\nALTER TABLE incident_recommendations DROP CONSTRAINT IF EXISTS incident_recommendations_fingerprint_key;"
)

with open("engines/correlation/migrations/013_human_attestations.sql", "w") as f:
    f.write(content)
