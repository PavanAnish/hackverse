#!/bin/bash
set -e

GREEN='\033[0;32m'
NC='\033[0m'

COMPOSE="docker compose -f infrastructure/docker/docker-compose.yml"

echo -e "${GREEN}Building Phase 1 local images...${NC}"
$COMPOSE build gateway auth-service product-service payment-service

echo -e "${GREEN}Starting local registry...${NC}"
$COMPOSE up -d registry
sleep 3

echo -e "${GREEN}Pushing images to local registry...${NC}"
$COMPOSE push gateway auth-service product-service payment-service

echo -e "${GREEN}Restarting lab to use registry images...${NC}"
$COMPOSE up -d gateway auth-service product-service payment-service

echo -e "${GREEN}Done! Lab is now running images from the local registry.${NC}"
