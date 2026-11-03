SHELL := /usr/bin/env bash
APP := requestarr
VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

.PHONY: hooks help fmt test test-integration check-login check-container vet build run clean web-install web-dev web-build web-check web-test dev-up dev-down stack-up stack-up-dev stack-start stack-down stack-logs stack-admin-token migrate-up snapshot release-check references

# Load .env into the recipe's environment when present, without requiring it
# to be sourced into the caller's shell first.
LOAD_ENV := if [ -f .env ]; then set -a; . ./.env; set +a; fi;

help:
	@printf '%s\n' \
	  'make fmt            Format Go code' \
	  'make test           Run Go tests' \
	  'make test-integration  Run PostgreSQL integration tests against the dev compose database' \
	  'make check-login       Prove OIDC login against the dev compose Keycloak' \
	  'make check-container   Build the image and prove it serves the UI' \
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
	  'make stack-up       Build/run the test stack and print first-admin setup details' \
	  'make stack-up-dev   Same stack, with development-only connection diagnostics in Setup/Settings' \
	  'make stack-down     Stop the test stack (STACK_PURGE=1 also deletes its data)' \
	  'make stack-logs     Follow the test stack logs' \
	  'make stack-admin-token  Print the issuer, subject and one-time token for the first-admin claim (ROTATE=1 replaces an expired token)' \
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
	  mapped="$$(docker compose -f compose.dev.yaml port postgres 5432 2>/dev/null || true)"; \
	  if [ "$${mapped##*:}" != "$(REQUESTARR_POSTGRES_PORT)" ] && (exec 3<>/dev/tcp/127.0.0.1/$(REQUESTARR_POSTGRES_PORT)) 2>/dev/null; then \
	    echo "Port $(REQUESTARR_POSTGRES_PORT) is taken by something other than the dev compose Postgres." >&2; \
	    echo "Rerun with a free port, e.g. REQUESTARR_POSTGRES_PORT=55432 make test-integration" >&2; \
	    exit 1; \
	  fi; \
	  docker compose -f compose.dev.yaml up -d --wait postgres; \
	  docker compose -f compose.dev.yaml exec -T postgres psql -U requestarr -d requestarr -tc "select 1 from pg_database where datname='requestarr_test'" | grep -q 1 \
	    || docker compose -f compose.dev.yaml exec -T postgres psql -U requestarr -d requestarr -c 'create database requestarr_test'; \
	fi; \
	REQUESTARR_TEST_DATABASE_URL="$${REQUESTARR_TEST_DATABASE_URL:-$(TEST_DB_URL)}" go test -tags integration -count=1 ./internal/store/postgres/...

check-container:
	scripts/check-container.sh

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
	mkdir -p internal/webui/dist
	find internal/webui/dist -mindepth 1 ! -name .gitkeep -delete
	cp -R web/dist/. internal/webui/dist/

web-check:
	cd web && npm run check

web-test:
	cd web && npm test

dev-up:
	docker compose -f compose.dev.yaml up -d

dev-down:
	docker compose -f compose.dev.yaml down

stack-up: override STACK_DEV_LOGS := false
stack-up-dev: override STACK_DEV_LOGS := true
stack-up stack-up-dev: stack-start

# Export explicit Make overrides, but leave an unset variable absent so Compose
# can still read REQUESTARR_STACK_HTTP_PORT from .env.
ifneq ($(origin REQUESTARR_STACK_HTTP_PORT),undefined)
export REQUESTARR_STACK_HTTP_PORT
endif

stack-start:
	@scripts/stack-secrets-key.sh
	REQUESTARR_DEV_LOGS=$(or $(STACK_DEV_LOGS),false) docker compose -f compose.test.yaml up -d --build --wait
	@url="$$(bash scripts/stack-http-url.sh)" && printf '\n%s\n' \
	  "Requestarr:  $$url  (sign in as requestarr-admin / requestarr-admin)" \
	  'Keycloak:    http://localhost:8081  (admin / admin)'
	@REQUESTARR_DEV_LOGS=$(or $(STACK_DEV_LOGS),false) scripts/stack-admin-token.sh --startup

stack-down:
	docker compose -f compose.test.yaml down $(if $(STACK_PURGE),-v)

stack-logs:
	docker compose -f compose.test.yaml logs -f

stack-admin-token:
	@scripts/stack-admin-token.sh

snapshot:
	goreleaser release --snapshot --clean

release-check:
	goreleaser check

references:
	./scripts/fetch-references.sh

# Install the repository's git hooks (.githooks/pre-commit) for this clone.
hooks:
	git config core.hooksPath .githooks
