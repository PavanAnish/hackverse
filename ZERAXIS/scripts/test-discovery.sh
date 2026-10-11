#!/bin/bash
set -e

GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m'

DISCOVERY_URL="http://localhost:8090"
PASS=0
FAIL=0

check() {
    local description="$1"
    local url="$2"
    local expected="$3"

    echo -n "  $description... "
    RESPONSE=$(curl -sf "$url" 2>/dev/null) || { echo -e "${RED}❌ FAILED (connection error)${NC}"; FAIL=$((FAIL+1)); return 1; }

    if echo "$RESPONSE" | grep -q "$expected"; then
        echo -e "${GREEN}✅ OK${NC}"
        PASS=$((PASS+1))
    else
        echo -e "${RED}❌ FAILED (expected '$expected')${NC}"
        echo "    Response: $RESPONSE" | head -c 200
        echo
        FAIL=$((FAIL+1))
        return 1
    fi
}

echo "========================================"
echo "  ZERAXIS Discovery Engine Tests"
echo "========================================"
echo

# Wait for discovery engine to be ready
echo -e "${YELLOW}Waiting for discovery engine to start...${NC}"
for i in $(seq 1 30); do
    if curl -sf "$DISCOVERY_URL/health" > /dev/null 2>&1; then
        echo -e "${GREEN}Discovery engine is ready.${NC}"
        break
    fi
    if [ "$i" -eq 30 ]; then
        echo -e "${RED}Discovery engine failed to start after 30s${NC}"
        exit 1
    fi
    sleep 1
done

# Wait for at least one discovery scan to complete
echo -e "${YELLOW}Waiting for first discovery scan...${NC}"
for i in $(seq 1 30); do
    ASSET_COUNT=$(curl -sf "$DISCOVERY_URL/api/v1/assets" 2>/dev/null | python3 -c "import sys,json; print(len(json.load(sys.stdin)))" 2>/dev/null || echo "0")
    if [ "$ASSET_COUNT" -gt 0 ]; then
        echo -e "${GREEN}First scan complete. $ASSET_COUNT assets found.${NC}"
        break
    fi
    if [ "$i" -eq 30 ]; then
        echo -e "${RED}No assets discovered after 30s${NC}"
        exit 1
    fi
    sleep 1
done

echo
echo "--- Health Check ---"
check "Discovery engine health" "$DISCOVERY_URL/health" "healthy"

echo
echo "--- Asset Discovery ---"
ASSETS=$(curl -sf "$DISCOVERY_URL/api/v1/assets")
ASSET_COUNT=$(echo "$ASSETS" | python3 -c "import sys,json; print(len(json.load(sys.stdin)))")
echo "  Total assets discovered: $ASSET_COUNT"

# Check for each expected lab service (and the discovery engine itself)
for SERVICE in gateway auth-service product-service payment-service postgres discovery-engine; do
    check "Found $SERVICE" "$DISCOVERY_URL/api/v1/assets" "$SERVICE"
done

echo
echo "--- Port Discovery ---"
# Gateway should have host-published port 8080
GATEWAY_PORTS=$(echo "$ASSETS" | python3 -c "
import sys, json
assets = json.load(sys.stdin)
for a in assets:
    if 'gateway' in a['name']:
        for p in a.get('ports', []):
            if p.get('host_port', 0) > 0:
                print(f\"  {a['name']}: container_port={p['container_port']} host_port={p['host_port']}\")
" 2>/dev/null)
echo "  Host-published ports:"
echo "$GATEWAY_PORTS"
if echo "$GATEWAY_PORTS" | grep -q "host_port=8080"; then
    echo -e "  ${GREEN}✅ Gateway port 8080 correctly published${NC}"
    PASS=$((PASS+1))
else
    echo -e "  ${RED}❌ Gateway port 8080 not found as published${NC}"
    FAIL=$((FAIL+1))
fi

echo
echo "--- Network Discovery ---"
check "Network relationships exist" "$DISCOVERY_URL/api/v1/relationships" "NETWORK_MEMBERSHIP"

RELS=$(curl -sf "$DISCOVERY_URL/api/v1/relationships")
REL_COUNT=$(echo "$RELS" | python3 -c "import sys,json; print(len(json.load(sys.stdin)))" 2>/dev/null || echo "0")
echo "  Total NETWORK_MEMBERSHIP relationships: $REL_COUNT"

echo
echo "--- Discovery Events ---"
check "Events recorded" "$DISCOVERY_URL/api/v1/events" "ASSET_DISCOVERED"

EVENT_COUNT=$(curl -sf "$DISCOVERY_URL/api/v1/events" | python3 -c "import sys,json; print(len(json.load(sys.stdin)))" 2>/dev/null || echo "0")
echo "  Total discovery events: $EVENT_COUNT"

echo
echo "========================================"
echo -e "  Results: ${GREEN}$PASS passed${NC}, ${RED}$FAIL failed${NC}"
echo "========================================"

if [ "$FAIL" -gt 0 ]; then
    exit 1
fi
