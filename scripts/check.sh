#!/usr/bin/env bash
set -euo pipefail
scripts/check-node-version.sh
make fmt
make test
make vet
scripts/check-tickets.sh
if docker info >/dev/null 2>&1; then scripts/deploy-files-test.sh; else echo 'no Docker; skipping deployment-file check'; fi
scripts/export-public-test.sh
scripts/agent/requestarr-diag-test.sh
# make test-integration starts the dev compose Postgres itself, so Docker is enough.
if [[ -n "${REQUESTARR_TEST_DATABASE_URL:-}" ]] || docker info >/dev/null 2>&1; then make test-integration; else echo 'no REQUESTARR_TEST_DATABASE_URL and no Docker; skipping PostgreSQL integration tests'; fi
if [[ -d web/node_modules ]]; then
  make web-check
  if [[ -n "${REQUESTARR_SKIP_BROWSER_TESTS:-}" ]]; then echo 'REQUESTARR_SKIP_BROWSER_TESTS set; skipping browser tests'; else make web-test; fi
else
  echo 'web/node_modules absent; skipping frontend typecheck and browser tests (make bootstrap installs them)'
fi
