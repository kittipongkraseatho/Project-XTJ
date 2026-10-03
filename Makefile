# Trading Journal — root Makefile
# Run `make help` for available targets.

SHELL := powershell.exe
.SHELLFLAGS := -NoProfile -Command

# Resolve paths relative to repo root
ROOT := D:\bot\Trading Journal
API  := $(ROOT)\apps\api
WEB  := $(ROOT)\apps\web
INFRA:= $(ROOT)\infra

.DEFAULT_GOAL := help

.PHONY: help
help:
	@powershell -NoProfile -Command "Write-Host ''; Write-Host 'Trading Journal — Phase 0 Make targets:' -ForegroundColor Cyan; Write-Host ''; Get-Content '$(ROOT)\Makefile' | Select-String -Pattern '^[a-zA-Z][a-zA-Z0-9_-]+:.*?#\s' | ForEach-Object { $$line = $$_.Line; $$name = ($$line -split ':')[0]; $$desc = ($$line -split '# ', 2)[1]; Write-Host ($$name.PadRight(20) + $$desc) -ForegroundColor White }; Write-Host ''"

# ──────────────────────────────────────────────────────────
# Tooling
# ──────────────────────────────────────────────────────────

.PHONY: deps
deps: ## Install Go tools (sqlc, golangci-lint, air)
	go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
	go install github.com/air-verse/air@latest
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# ──────────────────────────────────────────────────────────
# Infra (docker-compose)
# ──────────────────────────────────────────────────────────

.PHONY: up down logs
up: ## Start postgres + redis (docker-compose up -d)
	cd $(INFRA); docker compose up -d

down: ## Stop postgres + redis
	cd $(INFRA); docker compose down

logs: ## Tail docker-compose logs
	cd $(INFRA); docker compose logs -f

# ──────────────────────────────────────────────────────────
# Database — golang-migrate
# ──────────────────────────────────────────────────────────

DB_URL ?= postgres://tj:devpass@localhost:5432/tj?sslmode=disable

.PHONY: migrate-up migrate-down migrate-status migrate-new
migrate-up: ## Apply all up migrations
	cd $(API); migrate -path migrations -database "$(DB_URL)" up

migrate-down: ## Rollback last migration
	cd $(API); migrate -path migrations -database "$(DB_URL)" down 1

migrate-status: ## Show migration status
	cd $(API); migrate -path migrations -database "$(DB_URL)" version

migrate-new: ## Create a new migration pair (usage: make migrate-new NAME=add_foo)
	@if "$(NAME)" == "" (echo "NAME required, e.g. make migrate-new NAME=add_foo" && exit 1)
	cd $(API); migrate create -ext sql -dir migrations -seq $(NAME)

# ──────────────────────────────────────────────────────────
# Backend (apps/api)
# ──────────────────────────────────────────────────────────

.PHONY: sqlc test-api lint-api build-api dev-api
sqlc: ## Regenerate sqlc code (apps/api/internal/store/sqlc/)
	cd $(API); sqlc generate

test-api: ## Run Go tests with race + cover
	cd $(API); go test -race -coverprofile=cover.out ./...

lint-api: ## Run golangci-lint
	cd $(API); golangci-lint run ./...

build-api: ## Build API binary
	cd $(API); go build -o bin/api ./cmd/api

dev-api: ## Run API with hot reload (air)
	cd $(API); air -c .air.toml

# ──────────────────────────────────────────────────────────
# Frontend (apps/web)
# ──────────────────────────────────────────────────────────

.PHONY: install-web test-web lint-web typecheck-web build-web dev-web
install-web: ## Install FE deps
	cd $(WEB); pnpm install

test-web: ## Run FE unit tests (Vitest)
	cd $(WEB); pnpm test:unit

lint-web: ## Run FE lint
	cd $(WEB); pnpm lint

typecheck-web: ## Run FE typecheck
	cd $(WEB); pnpm typecheck

build-web: ## Build FE for production
	cd $(WEB); pnpm build

dev-web: ## Run FE dev server (Nuxt HMR)
	cd $(WEB); pnpm dev

# ──────────────────────────────────────────────────────────
# Composite
# ──────────────────────────────────────────────────────────

.PHONY: lint test build
lint: lint-api lint-web ## Lint everything

test: test-api test-web ## Run all tests

build: build-api build-web ## Build everything