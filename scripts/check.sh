#!/usr/bin/env bash
set -euo pipefail
scripts/check-node-version.sh
make fmt
make test
make vet
if [[ -n "${REQUESTARR_TEST_DATABASE_URL:-}" ]]; then make test-integration; else echo 'REQUESTARR_TEST_DATABASE_URL unset; skipping PostgreSQL integration tests'; fi
if [[ -d web/node_modules ]]; then
  make web-check
  if [[ -n "${REQUESTARR_SKIP_BROWSER_TESTS:-}" ]]; then echo 'REQUESTARR_SKIP_BROWSER_TESTS set; skipping browser tests'; else make web-test; fi
else
  echo 'web/node_modules absent; skipping frontend typecheck and browser tests (make bootstrap installs them)'
fi
