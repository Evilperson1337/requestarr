SHELL := /usr/bin/env bash
APP := requestarr
VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

.PHONY: help fmt test vet build run clean web-install web-dev web-build web-check dev-up dev-down migrate-up snapshot release-check references

help:
	@printf '%s\n' \
	  'make fmt            Format Go code' \
	  'make test           Run Go tests' \
	  'make vet            Run go vet' \
	  'make build          Build Requestarr' \
	  'make run            Run Requestarr API scaffold' \
	  'make web-install    Install web dependencies' \
	  'make web-dev        Run Vite development server' \
	  'make web-build      Build React UI' \
	  'make web-check      Typecheck/lint frontend' \
	  'make dev-up         Start dev dependencies with Docker Compose' \
	  'make dev-down       Stop dev dependencies' \
	  'make snapshot       Build GoReleaser snapshot' \
	  'make release-check  Validate GoReleaser config' \
	  'make references     Fetch reference source trees'

fmt:
	gofmt -w $$(find cmd internal -name '*.go' -type f)

test:
	go test ./...

vet:
	go vet ./...

build:
	mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(APP) ./cmd/requestarr

run:
	go run ./cmd/requestarr

clean:
	rm -rf bin dist coverage web/dist web/coverage

web-install:
	cd web && npm install

web-dev:
	cd web && npm run dev

web-build:
	cd web && npm run build

web-check:
	cd web && npm run check

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
