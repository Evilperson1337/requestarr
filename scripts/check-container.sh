#!/usr/bin/env bash
# Builds the container image and proves it starts and serves the embedded UI.
# Uses the dev compose Postgres and Keycloak (the app validates OIDC discovery
# at startup) and runs the image on the host network. Exit 0 only on success.
set -euo pipefail

cd "$(dirname "$0")/.."
compose=(docker compose -f compose.dev.yaml)
image=requestarr:container-check
name=requestarr-container-check
port="${REQUESTARR_CONTAINER_CHECK_PORT:-18081}"
database=requestarr_container_check
work="$(mktemp -d)"

fail() { echo "FAIL: $*" >&2; docker logs "$name" 2>&1 | tail -20 >&2 || true; exit 1; }
cleanup() { docker rm -f "$name" >/dev/null 2>&1 || true; rm -rf "$work"; }
trap cleanup EXIT

command -v docker >/dev/null || fail "docker is required"
command -v curl >/dev/null || fail "curl is required"
if (exec 3<>"/dev/tcp/127.0.0.1/$port") 2>/dev/null; then fail "port $port is in use; set REQUESTARR_CONTAINER_CHECK_PORT"; fi

echo "==> building $image"
docker build -q -t "$image" . >/dev/null || fail "docker build failed"

echo "==> starting Postgres and Keycloak"
export REQUESTARR_OIDC_ISSUER=unused REQUESTARR_OIDC_CLIENT_ID=unused REQUESTARR_OIDC_CLIENT_SECRET=unused REQUESTARR_SESSION_HMAC_SECRET=unused
"${compose[@]}" up -d --wait postgres >/dev/null || fail "Postgres did not become healthy"
"${compose[@]}" up -d keycloak >/dev/null || fail "Keycloak did not start"
"${compose[@]}" exec -T postgres psql -U requestarr -d requestarr -c "DROP DATABASE IF EXISTS $database" >/dev/null || fail "could not reset $database"
"${compose[@]}" exec -T postgres psql -U requestarr -d requestarr -c "CREATE DATABASE $database" >/dev/null || fail "could not create $database"
issuer=http://localhost:8081/realms/requestarr
for _ in $(seq 1 90); do curl -fsS "$issuer/.well-known/openid-configuration" >/dev/null 2>&1 && break; sleep 2; done
curl -fsS "$issuer/.well-known/openid-configuration" >/dev/null 2>&1 || fail "Keycloak realm not reachable"
pg_port="$("${compose[@]}" port postgres 5432 | sed 's/.*://')"

echo "==> running the container"
docker run -d --name "$name" --network host \
  -e REQUESTARR_ENV=development -e REQUESTARR_HTTP_ADDR="127.0.0.1:$port" -e REQUESTARR_PUBLIC_URL="http://localhost:$port" \
  -e REQUESTARR_DATABASE_URL="postgres://requestarr:requestarr@localhost:$pg_port/$database?sslmode=disable" \
  -e REQUESTARR_OIDC_ISSUER="$issuer" -e REQUESTARR_OIDC_CLIENT_ID=requestarr -e REQUESTARR_OIDC_CLIENT_SECRET=requestarr-dev-secret \
  -e REQUESTARR_SESSION_HMAC_SECRET=container-check-session-secret-0123456789 \
  "$image" >/dev/null || fail "container did not start"
for _ in $(seq 1 60); do curl -fsS "http://127.0.0.1:$port/readyz" >/dev/null 2>&1 && break; sleep 1; done
curl -fsS "http://127.0.0.1:$port/readyz" >/dev/null 2>&1 || fail "container never became ready"

echo "==> checking the served UI"
curl -fsS "http://127.0.0.1:$port/" -o "$work/index.html" || fail "GET / failed"
asset="$(grep -o '/assets/[^"]*\.js' "$work/index.html" | head -1)"
[[ -n "$asset" ]] || fail "index.html has no built /assets script reference"
curl -fsS -o /dev/null "http://127.0.0.1:$port$asset" || fail "built asset $asset not served"
curl -fsS "http://127.0.0.1:$port/requests" | grep -q '/assets/' || fail "SPA route /requests did not return the app shell"
curl -fsS "http://127.0.0.1:$port/api/v1/version" >/dev/null || fail "API not reachable"

echo "PASS: container serves the UI ($asset) and API on port $port"
