.PHONY: up down build status test-health test-discovery test-runtime logs discovery-logs runtime-logs clean

COMPOSE := docker compose -f infrastructure/docker/docker-compose.yml

up:
	$(COMPOSE) up -d --build

down:
	$(COMPOSE) down

build:
	$(COMPOSE) build

status:
	$(COMPOSE) ps

test-health:
	./scripts/test-health.sh

test-discovery:
	./scripts/test-discovery.sh

test-runtime:
	./scripts/test-runtime.sh

logs:
	$(COMPOSE) logs -f

discovery-logs:
	$(COMPOSE) logs -f discovery-engine

runtime-logs:
	$(COMPOSE) logs -f runtime-engine falco

clean:
	$(COMPOSE) down -v
