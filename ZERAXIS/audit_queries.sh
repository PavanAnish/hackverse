#!/bin/bash
docker exec docker-postgres-1 psql -U postgres -d zeraxis -c "
SELECT event_id, event_type, metadata->>'container_id' as container_id, asset_id, image_digest 
FROM events 
WHERE event_type IN ('PROCESS_EXECUTION', 'NETWORK_CONNECTION', 'FILE_ACTIVITY')
ORDER BY timestamp DESC LIMIT 5;"

docker exec docker-postgres-1 psql -U postgres -d zeraxis -c "
SELECT finding_id, rule_id, severity, asset_id, image_digest, status 
FROM findings 
ORDER BY created_at DESC LIMIT 5;"

docker exec docker-postgres-1 psql -U postgres -d zeraxis -c "
SELECT incident_id, risk_level, static_risk, dynamic_risk, attack_chain_state 
FROM incidents 
ORDER BY created_at DESC LIMIT 5;"
