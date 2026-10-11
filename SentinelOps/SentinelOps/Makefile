.PHONY: up down chaos-db-lock chaos-memory-leak chaos-service-drop logs

up:
	docker-compose up -d --build

down:
	docker-compose down

chaos-db-lock:
	bash ./chaos/chaos-db-lock.sh

chaos-memory-leak:
	bash ./chaos/chaos-memory-leak.sh

chaos-service-drop:
	bash ./chaos/chaos-service-drop.sh

logs:
	docker-compose logs -f
