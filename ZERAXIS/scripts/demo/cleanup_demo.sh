#!/bin/bash
set -euo pipefail

cd "$(dirname "$0")/../.."

echo "Cleaning up ZERAXIS demo resources..."

# Remove the correct demo container (payment-demo-v17).
# The old name (payment-service-demo) was wrong; this is what run_demo.sh creates.
docker rm -f payment-demo-v17 >/dev/null 2>&1 || true

PG_CONTAINER=$(docker ps --format '{{.Names}}' | grep postgres | head -n 1 || echo "")

if [ -n "$PG_CONTAINER" ]; then
    # Check if the demo asset exists
    DEMO_ASSET_ID=$(docker exec "$PG_CONTAINER" psql -U postgres -d zeraxis -tAc \
        "SELECT asset_id FROM discovery_assets WHERE name = 'payment-demo-v17' LIMIT 1" 2>/dev/null | tr -d '[:space:]')

    if [ -n "$DEMO_ASSET_ID" ]; then
        echo "  Purging demo DB state for asset $DEMO_ASSET_ID..."

        # Delete in dependency order: results → transitions → authorizations → recommendations → incident_findings → incidents → findings → asset
        docker exec "$PG_CONTAINER" psql -U postgres -d zeraxis -c \
            "DELETE FROM execution_results WHERE execution_id IN (SELECT ea.execution_id FROM execution_authorizations ea JOIN incident_recommendations ir ON ir.recommendation_id = ea.recommendation_id JOIN incidents i ON i.incident_id = ir.incident_id JOIN incident_findings inf ON inf.incident_id = i.incident_id JOIN findings f ON f.finding_id = inf.finding_id WHERE f.asset_id = '$DEMO_ASSET_ID')" \
            >/dev/null 2>&1 || true

        docker exec "$PG_CONTAINER" psql -U postgres -d zeraxis -c \
            "DELETE FROM execution_state_transitions WHERE execution_id IN (SELECT ea.execution_id FROM execution_authorizations ea JOIN incident_recommendations ir ON ir.recommendation_id = ea.recommendation_id JOIN incidents i ON i.incident_id = ir.incident_id JOIN incident_findings inf ON inf.incident_id = i.incident_id JOIN findings f ON f.finding_id = inf.finding_id WHERE f.asset_id = '$DEMO_ASSET_ID')" \
            >/dev/null 2>&1 || true

        docker exec "$PG_CONTAINER" psql -U postgres -d zeraxis -c \
            "DELETE FROM execution_authorizations WHERE recommendation_id IN (SELECT ir.recommendation_id FROM incident_recommendations ir JOIN incidents i ON i.incident_id = ir.incident_id JOIN incident_findings inf ON inf.incident_id = i.incident_id JOIN findings f ON f.finding_id = inf.finding_id WHERE f.asset_id = '$DEMO_ASSET_ID')" \
            >/dev/null 2>&1 || true

        docker exec "$PG_CONTAINER" psql -U postgres -d zeraxis -c \
            "DELETE FROM incident_recommendations WHERE incident_id IN (SELECT DISTINCT i.incident_id FROM incidents i JOIN incident_findings inf ON inf.incident_id = i.incident_id JOIN findings f ON f.finding_id = inf.finding_id WHERE f.asset_id = '$DEMO_ASSET_ID')" \
            >/dev/null 2>&1 || true

        docker exec "$PG_CONTAINER" psql -U postgres -d zeraxis -c \
            "DELETE FROM incident_findings WHERE finding_id IN (SELECT finding_id FROM findings WHERE asset_id = '$DEMO_ASSET_ID')" \
            >/dev/null 2>&1 || true

        docker exec "$PG_CONTAINER" psql -U postgres -d zeraxis -c \
            "DELETE FROM incidents WHERE NOT EXISTS (SELECT 1 FROM incident_findings inf WHERE inf.incident_id = incidents.incident_id)" \
            >/dev/null 2>&1 || true

        docker exec "$PG_CONTAINER" psql -U postgres -d zeraxis -c \
            "DELETE FROM findings WHERE asset_id = '$DEMO_ASSET_ID'" \
            >/dev/null 2>&1 || true

        docker exec "$PG_CONTAINER" psql -U postgres -d zeraxis -c \
            "DELETE FROM discovery_assets WHERE asset_id = '$DEMO_ASSET_ID'" \
            >/dev/null 2>&1 || true

        echo "  DB state purged."
    else
        echo "  No prior demo state in DB."
    fi
fi

export POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-postgres}"
export API_INTERNAL_SECRET="${API_INTERNAL_SECRET:-secret123}"
docker compose -f infrastructure/docker/docker-compose.yml -f scripts/demo/docker-compose.demo.yml up -d executor-engine >/dev/null 2>&1 || true

echo "✅ Cleanup complete."
