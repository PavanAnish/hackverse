#!/bin/bash
set -e

GREEN='\033[0;32m'
RED='\033[0;31m'
NC='\033[0m'

SUPPLY_URL="http://localhost:8091"

echo "========================================"
echo "  ZERAXIS Supply Chain Engine Tests"
echo "========================================"

echo "Waiting for Supply Chain API..."
for i in {1..30}; do
    if curl -s "$SUPPLY_URL/health" | grep -q "healthy"; then
        echo -e "${GREEN}API is up${NC}"
        break
    fi
    sleep 2
done

echo "Waiting for scans to complete (checking for images)..."
for i in {1..60}; do
    IMG_COUNT=$(curl -s "$SUPPLY_URL/api/v1/images" | python3 -c "import sys,json; print(len(json.load(sys.stdin)))" 2>/dev/null || echo "0")
    if [ "$IMG_COUNT" -gt 0 ]; then
        echo -e "${GREEN}Images found: $IMG_COUNT${NC}"
        break
    fi
    sleep 5
done

if [ "$IMG_COUNT" -eq 0 ]; then
    echo -e "${RED}No images found after 5 minutes. Check logs.${NC}"
    exit 1
fi

echo -e "${GREEN}All checks passed!${NC}"
