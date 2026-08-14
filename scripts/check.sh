#!/usr/bin/env bash
set -euo pipefail
make fmt
make test
make vet
if [[ -d web/node_modules ]]; then make web-check; else echo 'web/node_modules absent; skipping frontend typecheck'; fi
