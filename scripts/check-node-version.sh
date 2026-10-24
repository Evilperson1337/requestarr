#!/usr/bin/env bash
# Fails when the Node major version differs between the Dockerfile and the CI/release workflows.
set -euo pipefail
cd "$(dirname "$0")/.."
docker_major="$(sed -n 's/^FROM node:\([0-9]*\).*/\1/p' Dockerfile | head -1)"
[[ -n "$docker_major" ]] || { echo "FAIL: no node image in Dockerfile" >&2; exit 1; }
status=0
found=0
for workflow in .github/workflows/*.yml; do
  while read -r major; do
    found=$((found + 1))
    if [[ "$major" != "$docker_major" ]]; then echo "FAIL: $workflow uses Node $major, Dockerfile uses $docker_major" >&2; status=1; fi
  done < <(sed -n "s/.*node-version: '\([0-9]*\)'.*/\1/p" "$workflow")
done
[[ $found -gt 0 ]] || { echo "FAIL: no node-version found in workflows" >&2; exit 1; }
[[ $status == 0 ]] && echo "ok: Node $docker_major in Dockerfile and workflows"
exit $status
