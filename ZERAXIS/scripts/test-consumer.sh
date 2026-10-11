#!/usr/bin/env bash
set -e

# Run the consumer inside a throwaway container on the zeraxis-lab network
# This avoids needing to expose port 4222 to the host.

echo "Starting NATS consumer test script inside docker network..."

docker run --rm \
  --network docker_zeraxis-lab \
  -v "$(pwd)":/workspace \
  -w /workspace/scripts \
  -e NATS_URL="nats://nats:4222" \
  golang:1.27-alpine \
  go run test-consumer.go "$@"
