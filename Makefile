COMPOSE := docker compose -f infra/docker-compose.yml --env-file .env

.PHONY: env db up down reset api web seed test sqlc

env: ## Create .env from the example (only if missing)
	@test -f .env && echo ".env already exists" || cp .env.example .env

db: env ## Start Postgres and Redis only
	$(COMPOSE) up -d postgres redis

up: env ## Run everything in Docker (API on :8180, web on :3100)
	$(COMPOSE) --profile full up -d --build

down: ## Stop containers (keeps data)
	$(COMPOSE) --profile full down

reset: ## Stop containers and delete the database
	$(COMPOSE) --profile full down -v

api: ## Run the API locally (applies migrations on start)
	set -a; . ./.env; set +a; cd backend && go run ./cmd/api

web: ## Run the web app locally with live reload
	cd web && npm run dev

seed: ## Fill an empty database with the demo company
	set -a; . ./.env; set +a; cd backend && go run ./cmd/seed

test: ## Go tests and the TypeScript check
	cd backend && go test ./...
	cd web && npm run typecheck

sqlc: ## Regenerate database code after editing backend/queries
	cd backend && sqlc generate
