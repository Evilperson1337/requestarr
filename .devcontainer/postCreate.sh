#!/usr/bin/env bash
set -euo pipefail
cd /workspaces/requestarr
printf 'Requestarr dev container ready.\n'
printf 'Go: '; go version
printf 'Node: '; node --version || true
printf 'Run `make help` for common tasks.\n'
