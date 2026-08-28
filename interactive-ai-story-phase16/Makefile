SHELL := /usr/bin/env bash

.PHONY: dev db-up db-down migrate-up migrate-down backend-dev frontend-dev sqlc-generate backend-test frontend-test test lint build health

dev:
	docker compose -f deploy/docker-compose.yml up --build

db-up:
	docker compose -f deploy/docker-compose.yml up -d postgres

db-down:
	docker compose -f deploy/docker-compose.yml down

migrate-up:
	cd backend && go run github.com/pressly/goose/v3/cmd/goose@v3.24.2 -dir db/migrations postgres "$${HOST_DATABASE_URL:-postgres://story:story@127.0.0.1:5432/story?sslmode=disable}" up

backend-dev:
	cd backend && DATABASE_URL="$${HOST_DATABASE_URL:-postgres://story:story@127.0.0.1:5432/story?sslmode=disable}" STORY_LLM_BASE_URL="$${HOST_STORY_LLM_BASE_URL:-http://127.0.0.1:8081}" HTTP_ADDR="$${HOST_HTTP_ADDR:-127.0.0.1:8787}" MEDIA_ROOT="$${HOST_MEDIA_ROOT:-../data/generated}" MEDIA_BASE_URL=/media SECRET_STORE_PATH="$${HOST_SECRET_STORE_PATH:-../data/secrets/provider-keys.json}" go run ./cmd/server

frontend-dev:
	cd frontend && npm run dev -- --host 0.0.0.0

migrate-down:
	cd backend && go run github.com/pressly/goose/v3/cmd/goose@v3.24.2 -dir db/migrations postgres "$$DATABASE_URL" down

sqlc-generate:
	cd backend && go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.29.0 generate

backend-test:
	cd backend && go test ./...

frontend-test:
	cd frontend && npm test -- --run

test: backend-test frontend-test

lint:
	cd backend && test -z "$$(gofmt -l .)" && go vet ./...
	cd frontend && npm run typecheck && npm run lint

build:
	cd backend && mkdir -p bin && go build -o bin/server ./cmd/server
	cd frontend && npm run build

health:
	curl --fail --silent http://127.0.0.1:$${BACKEND_PORT:-8787}/health/live
	curl --fail --silent http://127.0.0.1:$${BACKEND_PORT:-8787}/health/ready
