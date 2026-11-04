#!/usr/bin/env bash
# Builds the container image and proves it starts and serves the embedded UI.
# Uses a private throwaway Postgres container and the dev compose Keycloak (the
# app validates OIDC discovery at startup) and runs the image on the host
# network. Also proves the image health check: the container must reach
# "healthy", then turn "unhealthy" once its database is stopped.
# Exit 0 only on success.
set -euo pipefail

cd "$(dirname "$0")/.."
compose=(docker compose -f compose.dev.yaml)
image=requestarr:container-check
name=requestarr-container-check
pgname=requestarr-container-check-pg
port="${REQUESTARR_CONTAINER_CHECK_PORT:-18081}"
pg_port="${REQUESTARR_CONTAINER_CHECK_PG_PORT:-55499}"
work="$(mktemp -d)"

fail() { echo "FAIL: $*" >&2; docker logs "$name" 2>&1 | tail -20 >&2 || true; exit 1; }
cleanup() { docker rm -f "$name" "$pgname" >/dev/null 2>&1 || true; rm -rf "$work"; }
trap cleanup EXIT

command -v docker >/dev/null || fail "docker is required"
command -v curl >/dev/null || fail "curl is required"
if (exec 3<>"/dev/tcp/127.0.0.1/$port") 2>/dev/null; then fail "port $port is in use; set REQUESTARR_CONTAINER_CHECK_PORT"; fi
if (exec 3<>"/dev/tcp/127.0.0.1/$pg_port") 2>/dev/null; then fail "port $pg_port is in use; set REQUESTARR_CONTAINER_CHECK_PG_PORT"; fi

echo "==> building $image"
docker build -q -t "$image" . >/dev/null || fail "docker build failed"

echo "==> starting Postgres and Keycloak"
export REQUESTARR_OIDC_ISSUER=unused REQUESTARR_OIDC_CLIENT_ID=unused REQUESTARR_OIDC_CLIENT_SECRET=unused REQUESTARR_SESSION_HMAC_SECRET=unused
docker rm -f "$pgname" >/dev/null 2>&1 || true
docker run -d --name "$pgname" -p "127.0.0.1:$pg_port:5432" \
  -e POSTGRES_DB=requestarr -e POSTGRES_USER=requestarr -e POSTGRES_PASSWORD=requestarr \
  postgres:17-alpine >/dev/null || fail "could not start Postgres"
for _ in $(seq 1 30); do docker exec "$pgname" pg_isready -U requestarr -d requestarr -h 127.0.0.1 >/dev/null 2>&1 && break; sleep 1; done
docker exec "$pgname" pg_isready -U requestarr -d requestarr -h 127.0.0.1 >/dev/null 2>&1 || fail "Postgres did not become ready"
issuer=http://localhost:8081/realms/requestarr
# Reuse a Keycloak that is already serving the realm on 8081 (any dev stack).
curl -fsS "$issuer/.well-known/openid-configuration" >/dev/null 2>&1 || "${compose[@]}" up -d keycloak >/dev/null || fail "Keycloak did not start"
for _ in $(seq 1 90); do curl -fsS "$issuer/.well-known/openid-configuration" >/dev/null 2>&1 && break; sleep 2; done
curl -fsS "$issuer/.well-known/openid-configuration" >/dev/null 2>&1 || fail "Keycloak realm not reachable"

echo "==> running the container"
docker run -d --name "$name" --network host \
  -e REQUESTARR_ENV=development -e REQUESTARR_HTTP_ADDR="127.0.0.1:$port" -e REQUESTARR_PUBLIC_URL="http://localhost:$port" \
  -e REQUESTARR_DATABASE_URL="postgres://requestarr:requestarr@localhost:$pg_port/requestarr?sslmode=disable" \
  -e REQUESTARR_OIDC_ISSUER="$issuer" -e REQUESTARR_OIDC_CLIENT_ID=requestarr -e REQUESTARR_OIDC_CLIENT_SECRET=requestarr-dev-secret \
  -e REQUESTARR_SESSION_HMAC_SECRET=container-check-session-secret-0123456789 \
  --health-interval=3s --health-start-period=0s \
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

echo "==> checking the image health check"
health() { docker inspect -f '{{.State.Health.Status}}' "$name" 2>/dev/null; }
# The image declares the health check; only the interval is shortened above.
for _ in $(seq 1 60); do [[ "$(health)" == healthy ]] && break; sleep 1; done
[[ "$(health)" == healthy ]] || fail "container never became healthy (status: $(health))"
docker stop "$pgname" >/dev/null || fail "could not stop Postgres"
for _ in $(seq 1 90); do [[ "$(health)" == unhealthy ]] && break; sleep 2; done
[[ "$(health)" == unhealthy ]] || fail "container did not turn unhealthy without its database (status: $(health))"

echo "PASS: container serves the UI ($asset) and API on port $port, and its health check follows the database"
