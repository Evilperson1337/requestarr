#!/usr/bin/env bash
set -euo pipefail
scripts/check-node-version.sh
make fmt
make test
make vet
if docker info >/dev/null 2>&1; then scripts/deploy-files-test.sh; else echo 'no Docker; skipping deployment-file check'; fi
scripts/export-public-test.sh
# make test-integration starts the dev compose Postgres itself, so Docker is enough.
if [[ -n "${REQUESTARR_TEST_DATABASE_URL:-}" ]] || docker info >/dev/null 2>&1; then make test-integration; else echo 'no REQUESTARR_TEST_DATABASE_URL and no Docker; skipping PostgreSQL integration tests'; fi
if [[ -d web/node_modules ]]; then make web-check web-test; else echo 'web/node_modules absent; skipping frontend typecheck and browser tests'; fi
