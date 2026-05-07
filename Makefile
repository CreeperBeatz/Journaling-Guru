.PHONY: help dev up down ps logs psql migrate-up migrate-down migrate-status fmt vet tidy build-api build-worker build-migrate frontend-install frontend-build vapid

help:
	@awk 'BEGIN{FS=":.*##"; printf "Targets:\n"} /^[a-zA-Z_-]+:.*##/ { printf "  %-22s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

dev: ## Start the dev stack with hot reload (postgres + api + worker + web)
	./start.sh

up: ## Start backing services only (postgres)
	docker compose up -d postgres

down: ## Stop and remove backing services
	docker compose down

ps: ## Show docker compose status
	docker compose ps

logs: ## Tail postgres logs
	docker compose logs -f postgres

psql: ## Open psql against the dev database
	docker compose exec postgres psql -U journai -d journai

migrate-up: ## Apply all pending migrations
	@set -a && . ./.env && set +a && cd backend && go run ./cmd/migrate up

migrate-down: ## Roll back one migration
	@set -a && . ./.env && set +a && cd backend && go run ./cmd/migrate down

migrate-status: ## Show migration status
	@set -a && . ./.env && set +a && cd backend && go run ./cmd/migrate status

fmt: ## gofmt the backend
	cd backend && gofmt -w .

vet: ## go vet the backend
	cd backend && go vet ./...

tidy: ## go mod tidy
	cd backend && go mod tidy

build-api:
	cd backend && go build -o bin/api ./cmd/api

build-worker:
	cd backend && go build -o bin/worker ./cmd/worker

build-migrate:
	cd backend && go build -o bin/migrate ./cmd/migrate

frontend-install:
	cd frontend && pnpm install

frontend-build:
	cd frontend && pnpm build

vapid: ## Print a fresh VAPID keypair (paste into .env)
	cd backend && go run ./cmd/vapid
