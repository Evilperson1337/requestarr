SHELL := /usr/bin/env bash
APP := requestarr
VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

.PHONY: hooks help bootstrap fmt test test-integration check-login check-container vet build run clean web-install web-dev web-build web-check web-test dev-up dev-down stack-up stack-up-dev stack-start stack-down stack-logs migrate-up snapshot snapshot-image release-check references

# Load .env into the recipe's environment when present, without requiring it
# to be sourced into the caller's shell first.
LOAD_ENV := if [ -f .env ]; then set -a; . ./.env; set +a; fi;

help:
	@printf '%s\n' \
	  'make bootstrap      Prepare a fresh checkout or worktree: install web dependencies and build the embedded UI' \
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
	  'make snapshot       Build GoReleaser snapshot' \
	  'make release-check  Validate GoReleaser config' \
	  'make references     Fetch reference source trees'

# A fresh clone or worktree has no web/node_modules and no embedded UI
# (internal/webui/dist is build output), so the Go tests that serve the SPA fail
# until this has run.
bootstrap:
	@if [ -L web/node_modules ]; then echo "web/node_modules is a symlink: npm ci would empty the directory it points to. Remove the link and run make bootstrap again." >&2; exit 1; fi
	cd web && npm ci
	$(MAKE) web-build

fmt:
	gofmt -w $$(find cmd internal db -name '*.go' -type f)

test:
	go test ./...

# Integration tests run in a database made for the run alone (requestarr_test_<pid>) on
# the dev compose Postgres, so they never touch the application's own database, fixtures
# from an earlier run cannot leak in, and two runs at once (two worktrees, two agents) do
# not collide. It is dropped when the run ends. Override REQUESTARR_TEST_DATABASE_URL to
# target another disposable database instead; that database is never dropped for you.
# The host port: REQUESTARR_POSTGRES_PORT when set; otherwise the port the dev compose
# Postgres already listens on; otherwise 5432, or the first free port from 55432 when
# 5432 is taken by something else. So a machine with 5432 in use needs no setting.
test-integration:
	@set -e; \
	if [ -z "$$REQUESTARR_TEST_DATABASE_URL" ]; then \
	  export REQUESTARR_OIDC_ISSUER=unused REQUESTARR_OIDC_CLIENT_ID=unused REQUESTARR_OIDC_CLIENT_SECRET=unused REQUESTARR_SESSION_HMAC_SECRET=unused; \
	  free() { ! (exec 3<>/dev/tcp/127.0.0.1/$$1) 2>/dev/null; }; \
	  mapped="$$(docker compose -f compose.dev.yaml port postgres 5432 2>/dev/null || true)"; mapped="$${mapped##*:}"; \
	  port="$${REQUESTARR_POSTGRES_PORT:-}"; \
	  if [ -z "$$port" ]; then \
	    if [ -n "$$mapped" ]; then port="$$mapped"; \
	    elif free 5432; then port=5432; \
	    else port=55432; while ! free "$$port"; do port=$$((port + 1)); done; \
	      echo "Port 5432 is taken; the dev compose Postgres will listen on $$port." >&2; fi; \
	  elif [ "$$mapped" != "$$port" ] && ! free "$$port"; then \
	    echo "Port $$port is taken by something other than the dev compose Postgres; unset REQUESTARR_POSTGRES_PORT to pick a free one." >&2; \
	    exit 1; \
	  fi; \
	  export REQUESTARR_POSTGRES_PORT="$$port"; \
	  docker compose -f compose.dev.yaml up -d --wait postgres; \
	  db="requestarr_test_$$$$"; \
	  psql() { docker compose -f compose.dev.yaml exec -T postgres psql -U requestarr -d requestarr -v ON_ERROR_STOP=1 "$$@"; }; \
	  psql -c "create database $$db"; \
	  trap 'psql -c "drop database if exists '"$$db"' with (force)" >/dev/null' EXIT; \
	  REQUESTARR_TEST_DATABASE_URL="postgres://requestarr:requestarr@localhost:$$port/$$db?sslmode=disable"; export REQUESTARR_TEST_DATABASE_URL; \
	fi; \
	go test -tags integration -count=1 ./internal/store/postgres/...

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
	  "Requestarr:  $$url  (administrator sign-in: /login)" \
	  'Local administrator: admin. Read the initial one-time password in the container log, then choose your own password.' \
	  'Recovery: docker compose -f compose.test.yaml exec requestarr /requestarr reset-admin-password' \
	  'Keycloak:    http://localhost:8081  (admin / admin)'

stack-down:
	docker compose -f compose.test.yaml down $(if $(STACK_PURGE),-v)

stack-logs:
	docker compose -f compose.test.yaml logs -f

# Local snapshot: archives (no SBOM, since Syft may be absent; CI generates it)
# and the multi-arch image, built from the repository Dockerfile and never pushed.
snapshot: snapshot-image
	goreleaser release --snapshot --clean --skip=sbom

# Needs a Docker engine that can hold multi-platform images (containerd image
# store), or a buildx builder; the image is not pushed anywhere.
snapshot-image:
	docker buildx build --platform linux/amd64,linux/arm64 -t requestarr:snapshot .

release-check:
	goreleaser check
	scripts/check-release-config.sh

references:
	./scripts/fetch-references.sh

# Install the repository's git hooks (.githooks/pre-commit) for this clone.
hooks:
	git config core.hooksPath .githooks
