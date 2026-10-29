#!/usr/bin/env bash
# Resolve through Compose so shell/.env precedence matches the running stack.
# Only print the public URL, never the rest of the resolved configuration.
set -euo pipefail
cd "$(dirname "$0")/.."
docker compose -f compose.test.yaml config --format json \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)["services"]["requestarr"]["environment"]["REQUESTARR_PUBLIC_URL"])'
