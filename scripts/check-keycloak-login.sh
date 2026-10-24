#!/usr/bin/env bash
# Proves OIDC login end to end against the dev compose Keycloak: starts
# Postgres and Keycloak, runs Requestarr on the host against a throwaway
# database, signs in as the realm's dev user with curl, and checks /auth/me.
# Exit 0 only if the whole login round trip succeeds.
set -euo pipefail

cd "$(dirname "$0")/.."
compose=(docker compose -f compose.dev.yaml)
port="${REQUESTARR_LOGIN_CHECK_PORT:-18080}"
kc="${REQUESTARR_KEYCLOAK_URL:-http://localhost:8081}"
issuer="$kc/realms/requestarr"
base="http://localhost:$port"
database=requestarr_login_check
work="$(mktemp -d)"
server_pid=""

fail() { echo "FAIL: $*" >&2; exit 1; }
cleanup() {
  [[ -n "$server_pid" ]] && kill "$server_pid" 2>/dev/null || true
  [[ -n "$server_pid" ]] && wait "$server_pid" 2>/dev/null || true
  rm -rf "$work"
}
trap cleanup EXIT

case "$kc" in http://localhost:*|http://127.0.0.1:*) ;; *) fail "REQUESTARR_KEYCLOAK_URL must be a local http://localhost or http://127.0.0.1 URL (dev credentials are sent to it)";; esac

for tool in docker curl go; do command -v "$tool" >/dev/null || fail "$tool is required"; done
if (exec 3<>"/dev/tcp/127.0.0.1/$port") 2>/dev/null; then fail "port $port is in use; set REQUESTARR_LOGIN_CHECK_PORT"; fi

echo "==> starting Postgres and Keycloak (Keycloak is recreated so the realm import is current)"
export REQUESTARR_OIDC_ISSUER=unused REQUESTARR_OIDC_CLIENT_ID=unused REQUESTARR_OIDC_CLIENT_SECRET=unused REQUESTARR_SESSION_HMAC_SECRET=unused
"${compose[@]}" up -d --wait postgres >/dev/null || fail "Postgres did not become healthy"
"${compose[@]}" up -d --force-recreate keycloak >/dev/null || fail "Keycloak did not start"
"${compose[@]}" exec -T postgres psql -U requestarr -d requestarr -c "DROP DATABASE IF EXISTS $database" >/dev/null || fail "could not reset the $database database"
"${compose[@]}" exec -T postgres psql -U requestarr -d requestarr -c "CREATE DATABASE $database" >/dev/null || fail "could not create the $database database"

echo "==> waiting for the realm at $issuer"
for _ in $(seq 1 90); do
  curl -fsS "$issuer/.well-known/openid-configuration" >/dev/null 2>&1 && break
  sleep 2
done
curl -fsS "$issuer/.well-known/openid-configuration" >/dev/null 2>&1 || fail "Keycloak realm not reachable at $issuer"

echo "==> building and starting Requestarr on $base"
go build -o "$work/requestarr" ./cmd/requestarr || fail "build failed"
pg_port="$("${compose[@]}" port postgres 5432 | sed 's/.*://')"
REQUESTARR_ENV=development REQUESTARR_HTTP_ADDR="127.0.0.1:$port" REQUESTARR_PUBLIC_URL="$base" \
  REQUESTARR_DATABASE_URL="postgres://requestarr:requestarr@localhost:$pg_port/$database?sslmode=disable" \
  REQUESTARR_OIDC_ISSUER="$issuer" REQUESTARR_OIDC_CLIENT_ID=requestarr REQUESTARR_OIDC_CLIENT_SECRET=requestarr-dev-secret \
  REQUESTARR_SESSION_HMAC_SECRET="login-check-session-secret-0123456789abcdef" \
  "$work/requestarr" >"$work/server.log" 2>&1 &
server_pid=$!
for _ in $(seq 1 120); do
  curl -fsS "$base/readyz" >/dev/null 2>&1 && break
  kill -0 "$server_pid" 2>/dev/null || { cat "$work/server.log" >&2; fail "Requestarr exited during startup"; }
  sleep 1
done
curl -fsS "$base/readyz" >/dev/null 2>&1 || { cat "$work/server.log" >&2; fail "Requestarr did not become ready"; }

jar="$work/cookies"
echo "==> starting login"
auth_url="$(curl -sS -o /dev/null -c "$jar" -b "$jar" -w '%{redirect_url}' "$base/api/v1/auth/login")"
[[ "$auth_url" == "$issuer/"* ]] || fail "login did not redirect to Keycloak (got: ${auth_url:-nothing})"

echo "==> submitting credentials as the dev user"
curl -sS -c "$jar" -b "$jar" -o "$work/form.html" "$auth_url"
action="$(grep -o 'action="[^"]*"' "$work/form.html" | head -1 | sed 's/action="//; s/"$//; s/&amp;/\&/g')"
[[ -n "$action" ]] || fail "Keycloak login form not found"
callback="$(curl -sS -c "$jar" -b "$jar" -o /dev/null -w '%{redirect_url}' \
  --data-urlencode username=requestarr-admin --data-urlencode password=requestarr-admin "$action")"
[[ "$callback" == "$base/api/v1/auth/callback"* ]] || fail "Keycloak did not redirect back to Requestarr (got: ${callback:-nothing})"

echo "==> completing the callback"
status="$(curl -sS -c "$jar" -b "$jar" -o "$work/callback.body" -w '%{http_code}' "$callback")"
[[ "$status" == 302 || "$status" == 303 ]] || { cat "$work/callback.body" >&2; echo >&2; tail -20 "$work/server.log" >&2; fail "callback returned HTTP $status"; }

echo "==> checking the session"
me_status="$(curl -sS -c "$jar" -b "$jar" -o "$work/me.json" -w '%{http_code}' "$base/api/v1/auth/me")"
[[ "$me_status" == 200 ]] || fail "/auth/me returned HTTP $me_status"
grep -q '"csrfToken"' "$work/me.json" && grep -q 'admin@requestarr.local' "$work/me.json" || fail "/auth/me did not return the signed-in user"

echo "PASS: OIDC login against $issuer succeeded for admin@requestarr.local"
