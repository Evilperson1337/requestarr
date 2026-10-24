SHELL := /usr/bin/env bash
APP := requestarr
VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

.PHONY: help fmt test test-integration check-login vet build run clean web-install web-dev web-build web-check web-test dev-up dev-down migrate-up snapshot release-check references

# Load .env into the recipe's environment when present, without requiring it
# to be sourced into the caller's shell first.
LOAD_ENV := if [ -f .env ]; then set -a; . ./.env; set +a; fi;

help:
	@printf '%s\n' \
	  'make fmt            Format Go code' \
	  'make test           Run Go tests' \
	  'make test-integration  Run PostgreSQL integration tests against the dev compose database' \
	  'make check-login       Prove OIDC login against the dev compose Keycloak' \
	  'make vet            Run go vet' \
	  'make build          Build Requestarr' \
	  'make run            Run Requestarr API scaffold (requires a reachable PostgreSQL, see make dev-up)' \
	  'make migrate-up     Apply pending database migrations and exit' \
	  'make web-install    Install web dependencies' \
	  'make web-dev        Run Vite development server' \
	  'make web-build      Build React UI' \
	  'make web-check      Typecheck/lint frontend' \
	  'make web-test       Run focused browser tests (Chromium required)' \
	  'make dev-up         Start dev dependencies with Docker Compose' \
	  'make dev-down       Stop dev dependencies' \
	  'make snapshot       Build GoReleaser snapshot' \
	  'make release-check  Validate GoReleaser config' \
	  'make references     Fetch reference source trees'

fmt:
	gofmt -w $$(find cmd internal db -name '*.go' -type f)

test:
	go test ./...

# Integration tests use a dedicated requestarr_test database on the dev compose
# Postgres so they never touch the application's own database. Override
# REQUESTARR_TEST_DATABASE_URL to target another disposable database instead.
# Set REQUESTARR_POSTGRES_PORT when host port 5432 is already taken.
REQUESTARR_POSTGRES_PORT ?= 5432
export REQUESTARR_POSTGRES_PORT
TEST_DB_URL ?= postgres://requestarr:requestarr@localhost:$(REQUESTARR_POSTGRES_PORT)/requestarr_test?sslmode=disable

test-integration:
	@set -e; \
	if [ -z "$$REQUESTARR_TEST_DATABASE_URL" ]; then \
	  export REQUESTARR_OIDC_ISSUER=unused REQUESTARR_OIDC_CLIENT_ID=unused REQUESTARR_OIDC_CLIENT_SECRET=unused REQUESTARR_SESSION_HMAC_SECRET=unused; \
	  docker compose -f compose.dev.yaml up -d --wait postgres; \
	  docker compose -f compose.dev.yaml exec -T postgres psql -U requestarr -d requestarr -tc "select 1 from pg_database where datname='requestarr_test'" | grep -q 1 \
	    || docker compose -f compose.dev.yaml exec -T postgres psql -U requestarr -d requestarr -c 'create database requestarr_test'; \
	fi; \
	REQUESTARR_TEST_DATABASE_URL="$${REQUESTARR_TEST_DATABASE_URL:-$(TEST_DB_URL)}" go test -tags integration -count=1 ./internal/store/postgres/...

check-login:
	scripts/check-keycloak-login.sh

vet:
	go vet ./...

build: web-build
	mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(APP) ./cmd/requestarr

run:
	@$(LOAD_ENV) go run ./cmd/requestarr

migrate-up:
	@$(LOAD_ENV) go run ./cmd/requestarr -migrate-only

clean:
	rm -rf bin dist coverage web/dist web/coverage

web-install:
	cd web && npm install

web-dev:
	cd web && npm run dev

web-build:
	cd web && npm run build
	rm -rf internal/webui/dist
	mkdir -p internal/webui/dist
	cp -R web/dist/. internal/webui/dist/

web-check:
	cd web && npm run check

web-test:
	cd web && npm test

dev-up:
	docker compose -f compose.dev.yaml up -d

dev-down:
	docker compose -f compose.dev.yaml down

snapshot:
	goreleaser release --snapshot --clean

release-check:
	goreleaser check

references:
	./scripts/fetch-references.sh
