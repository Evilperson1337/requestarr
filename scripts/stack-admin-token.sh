#!/usr/bin/env bash
# Prints the values the first-admin setup form asks for when using the
# `make stack-up` test stack: OIDC issuer, the dev admin's Keycloak subject,
# and a fresh one-time setup token. Dev credentials only; the token goes to
# stdout, which is acceptable for this throwaway stack.
set -euo pipefail

cd "$(dirname "$0")/.."
compose=(docker compose -f compose.test.yaml)
kc="http://localhost:8081"
issuer="$kc/realms/requestarr"
user="${REQUESTARR_STACK_ADMIN_USER:-requestarr-admin}"

fail() { echo "FAIL: $*" >&2; exit 1; }
startup=false
if [[ "${1:-}" == --startup && $# == 1 ]]; then
  startup=true
elif [[ $# != 0 ]]; then
  fail 'usage: stack-admin-token.sh [--startup]'
fi
for tool in docker curl python3; do command -v "$tool" >/dev/null || fail "$tool is required"; done

# Startup is repeatable without rotating or reading any token/hash/identity.
# The CLI intentionally reports all issuance failures identically; do not catch
# that generic error and mistake a database/configuration failure for success.
if "$startup"; then
  public_url="$(bash scripts/stack-http-url.sh)" || fail 'could not resolve the stack public URL'
  # Compose --wait cannot wait for this distroless app's migrations: the image
  # has no healthcheck. Bound both each probe and the total number of probes.
  ready=false
  for ((attempt=1; attempt<=30; attempt++)); do
    if curl --fail --silent --output /dev/null --connect-timeout 1 --max-time 2 "$public_url/readyz"; then
      ready=true
      break
    fi
    if ((attempt < 30)); then sleep 1; fi
  done
  "$ready" || fail 'Requestarr readiness timed out; bootstrap state was not queried'
  state="$("${compose[@]}" exec -T postgres psql -X -U requestarr -d requestarr -At -v ON_ERROR_STOP=1 \
    -c "SELECT CASE WHEN complete THEN 'complete' WHEN token_hash IS NOT NULL THEN 'issued' ELSE 'empty' END FROM setup_state WHERE singleton=TRUE" 2>/dev/null)" \
    || fail 'could not inspect first-admin bootstrap state'
  case "$state" in
    complete) echo 'First-admin bootstrap is already complete; no setup token issued.'; exit 0 ;;
    issued) echo 'A setup token was already issued; keeping it unchanged. Use the original token, or make stack-admin-token ROTATE=1 after expiry.'; exit 0 ;;
    empty) ;;
    *) fail 'unexpected first-admin bootstrap state' ;;
  esac
fi

admin_token="$(curl -fsS -X POST "$kc/realms/master/protocol/openid-connect/token" \
  -d grant_type=password -d client_id=admin-cli -d username=admin -d password=admin \
  | python3 -c 'import sys,json; print(json.load(sys.stdin)["access_token"])')" \
  || fail "could not reach Keycloak at $kc; is the stack up (make stack-up)?"

subject="$(curl -fsS -H "Authorization: Bearer $admin_token" "$kc/admin/realms/requestarr/users?username=$user&exact=true" \
  | python3 -c 'import sys,json; u=json.load(sys.stdin); print(u[0]["id"] if u else "")')"
[[ -n "$subject" ]] || fail "user $user not found in the requestarr realm"

args=(setup-token --issuer "$issuer" --subject "$subject" --stdout)
if ! "$startup" && [[ -n "${ROTATE:-}" ]]; then args+=(--rotate); fi
err="$(mktemp)"; trap 'rm -f "$err"' EXIT
token="$("${compose[@]}" run --rm --no-deps -T requestarr "${args[@]}" 2>"$err")" \
  || { grep '^requestarr:' "$err" >&2 || true
       fail "setup-token failed: a live token (issued in the last 30 minutes) or a completed setup cannot be replaced; ROTATE=1 replaces an expired token"; }

printf 'Sign in as %s / %s first, then enter:\n' "$user" "$user"
printf '  OIDC issuer:  %s\n  Subject:      %s\n  Setup token:  %s\n(token expires in 30 minutes)\n' "$issuer" "$subject" "$token"
