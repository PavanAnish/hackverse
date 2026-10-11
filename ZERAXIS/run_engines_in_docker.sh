#!/bin/bash
set -e

# Run a golang container on the zeraxis-lab network, mounting the ZERAXIS dir
docker run -d --name zeraxis-audit-runner --network docker_zeraxis-lab -v $(pwd):/app -w /app golang:1.22-alpine tail -f /dev/null

# Install dependencies in the container if needed (alpine)
docker exec zeraxis-audit-runner apk add --no-cache bash make gcc musl-dev

# Build and start Detection
docker exec -d -e DATABASE_URL="postgres://postgres:postgres@postgres:5432/zeraxis?sslmode=disable" -e NATS_URL="nats://nats:4222" zeraxis-audit-runner bash -c "cd engines/detection && go build -o detection-engine . && ./detection-engine > ../../detection.log 2>&1"

# Build and start Correlation
docker exec -d -e DATABASE_URL="postgres://postgres:postgres@postgres:5432/zeraxis?sslmode=disable" -e NATS_URL="nats://nats:4222" -e MIGRATIONS_DIR="./migrations" -e API_PORT="8094" zeraxis-audit-runner bash -c "cd engines/correlation && go build -o correlation-engine ./cmd/correlation && ./correlation-engine > ../../correlation.log 2>&1"

echo "Started engines inside zeraxis-audit-runner"
