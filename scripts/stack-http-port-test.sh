#!/usr/bin/env bash
# Real Compose resolution and Make recipes in an isolated fixture. No containers
# are started, and resolved secrets are never printed.
set -euo pipefail
cd "$(dirname "$0")/.."
export STACK_REAL_DOCKER="$(command -v docker)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/scripts" "$work/bin"
cp Makefile compose.test.yaml "$work/"
cp scripts/stack-http-url.sh "$work/scripts/"
printf '#!/usr/bin/env bash\nexit 0\n' > "$work/scripts/stack-secrets-key.sh"
cat > "$work/scripts/stack-admin-token.sh" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail
[[ "$(bash scripts/stack-http-url.sh)" == "http://localhost:$EXPECTED_PORT" ]]
MOCK
cat > "$work/bin/docker" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail
if [[ "$*" == *' up '* ]]; then
  # Validate the actual Compose model instead of starting it.
  "$STACK_REAL_DOCKER" compose -f compose.test.yaml config --format json | python3 -c '
import json, os, sys
env = json.load(sys.stdin)["services"]["requestarr"]["environment"]
port = os.environ["EXPECTED_PORT"]
assert env["REQUESTARR_HTTP_ADDR"] == "0.0.0.0:" + port
assert env["REQUESTARR_PUBLIC_URL"] == "http://localhost:" + port
'
else
  exec "$STACK_REAL_DOCKER" "$@"
fi
MOCK
chmod +x "$work/bin/docker" "$work/scripts/"*.sh
export PATH="$work/bin:$PATH"
# Avoid inheriting real secrets or unrelated Compose project overrides.
unset REQUESTARR_STACK_HTTP_PORT COMPOSE_FILE COMPOSE_ENV_FILES COMPOSE_DISABLE_ENV_FILE
export REQUESTARR_SECRETS_KEY=test-only-placeholder
cd "$work"
check() {
  make --no-print-directory stack-start "$@" > output
  grep -q "Requestarr:  http://localhost:$EXPECTED_PORT  " output
  if grep -q 'test-only-placeholder\|REQUESTARR_SESSION_HMAC_SECRET\|REQUESTARR_OIDC_CLIENT_SECRET' output; then
    echo 'FAIL: resolved configuration leaked into output' >&2; exit 1
  fi
  "$STACK_REAL_DOCKER" compose -f compose.test.yaml config --quiet
}
export EXPECTED_PORT=18080
check
printf 'REQUESTARR_STACK_HTTP_PORT=28080\n' > .env
export EXPECTED_PORT=28080
check
export REQUESTARR_STACK_HTTP_PORT=38080 EXPECTED_PORT=38080
check
export EXPECTED_PORT=48080
check REQUESTARR_STACK_HTTP_PORT=48080
export EXPECTED_PORT=18080
check REQUESTARR_STACK_HTTP_PORT=
make --no-print-directory -n stack-up REQUESTARR_STACK_HTTP_PORT=48080 > /dev/null
echo 'PASS: Compose listen/public URL, Make printed/bootstrap URL, default, .env, shell, Make override and empty fallback (no containers started)'
