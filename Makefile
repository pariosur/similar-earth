.PHONY: dev dev-infra dev-stop

# Start everything for local development
dev: dev-infra
	@echo "Starting all services..."
	@trap 'kill 0' EXIT; \
	cd python && uvicorn main:app --host 0.0.0.0 --port 8001 --workers 4 & \
	sleep 2 && \
	go run ./cmd/server & \
	cd frontend && npm run dev & \
	wait

# Start infrastructure (Postgres) via Docker
dev-infra:
	@docker compose up postgres -d

# Stop all background services
dev-stop:
	@docker compose down
	@-lsof -ti :8080 | xargs kill 2>/dev/null
	@-lsof -ti :8001 | xargs kill 2>/dev/null
	@-lsof -ti :3000 | xargs kill 2>/dev/null
	@echo "All services stopped"
