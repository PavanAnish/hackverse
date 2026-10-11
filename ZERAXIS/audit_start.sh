#!/bin/bash
set -e

echo "Starting ZERAXIS Docker environment..."
make up
sleep 15

export DATABASE_URL="postgres://postgres:postgres@localhost:5432/zeraxis?sslmode=disable"
export NATS_URL="nats://localhost:4222"
export MIGRATIONS_DIR="./engines/correlation/migrations"
export API_PORT="8094"

echo "Starting Detection Engine..."
cd engines/detection
go build -o detection-engine .
./detection-engine > ../../detection.log 2>&1 &
DETECTION_PID=$!
cd ../..

echo "Starting Correlation Engine..."
cd engines/correlation
go build -o correlation-engine ./cmd/correlation
./correlation-engine > ../../correlation.log 2>&1 &
CORRELATION_PID=$!
cd ../..

echo "Engines started. PIDs: Detection=$DETECTION_PID, Correlation=$CORRELATION_PID"
sleep 5
