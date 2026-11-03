#!/usr/bin/env bash
# Deterministic helper checks: no Docker daemon, database or real credentials.
set -euo pipefail
cd "$(dirname "$0")/.."
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
export STACK_HELPER_TEST_DIR="$work"
mkdir "$work/bin"
cat > "$work/bin/docker" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "$STACK_HELPER_TEST_DIR/calls"
if [[ "$*" == *'config --format json'* ]]; then
  printf '{"services":{"requestarr":{"environment":{"REQUESTARR_PUBLIC_URL":"http://localhost:%s"}}}}\n' "${REQUESTARR_STACK_HTTP_PORT:-18080}"
elif [[ "$*" == *'exec -T postgres psql'* ]]; then
  [[ -f "$STACK_HELPER_TEST_DIR/ready" ]] || { echo 'SQL queried before readiness' >&2; exit 1; }
  [[ "${TEST_QUERY_FAIL:-0}" == 0 ]] || { echo 'RAW_DATABASE_SECRET' >&2; exit 1; }
  printf '%s\n' "${TEST_STATE:-empty}"
elif [[ "$*" == *'run --rm --no-deps -T requestarr setup-token'* ]]; then
  [[ "${TEST_ISSUE_FAIL:-0}" == 0 ]] || { echo 'requestarr: could not issue setup token' >&2; exit 1; }
  echo 'fake-test-only-token'
else
  exit 2
fi
MOCK
cat > "$work/bin/curl" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail
if [[ "$*" == *'/readyz'* ]]; then
  printf '%s\n' "$*" >> "$STACK_HELPER_TEST_DIR/calls"
  [[ "$*" == *'--connect-timeout 1 --max-time 2'* ]] || exit 2
  count=0
  if [[ -f "$STACK_HELPER_TEST_DIR/probes" ]]; then read -r count < "$STACK_HELPER_TEST_DIR/probes"; fi
  count=$((count + 1))
  echo "$count" > "$STACK_HELPER_TEST_DIR/probes"
  ((count > ${TEST_READY_FAILURES:-0})) || exit 1
  touch "$STACK_HELPER_TEST_DIR/ready"
  exit 0
fi
if [[ "$*" == *'/realms/master/'* ]]; then
  echo '{"access_token":"fake-keycloak-token"}'
else
  echo '[{"id":"fake-subject"}]'
fi
MOCK
cat > "$work/bin/sleep" <<'MOCK'
#!/usr/bin/env bash
[[ "$*" == 1 ]] || exit 2
echo sleep >> "$STACK_HELPER_TEST_DIR/calls"
MOCK
chmod +x "$work/bin/docker" "$work/bin/curl" "$work/bin/sleep"
export PATH="$work/bin:$PATH"
unset ROTATE TEST_QUERY_FAIL TEST_ISSUE_FAIL TEST_READY_FAILURES REQUESTARR_STACK_HTTP_PORT

TEST_STATE=complete TEST_READY_FAILURES=2 bash scripts/stack-admin-token.sh --startup > "$work/out"
[[ "$(cat "$work/probes")" == 3 ]]
[[ "$(grep -c '^sleep$' "$work/calls")" == 2 ]]
grep -q 'exec -T postgres psql' "$work/calls"
grep -q 'http://localhost:18080/readyz' "$work/calls"

: > "$work/calls"
REQUESTARR_STACK_HTTP_PORT=28080 TEST_STATE=issued bash scripts/stack-admin-token.sh --startup > "$work/out"
grep -q 'http://localhost:28080/readyz' "$work/calls"
if grep -q 'setup-token\|http://localhost:18080/readyz' "$work/calls"; then
  echo 'FAIL: custom readiness port ignored or existing token changed' >&2; exit 1
fi

rm "$work/ready" "$work/probes"
: > "$work/calls"
if TEST_READY_FAILURES=100 bash scripts/stack-admin-token.sh --startup > "$work/out" 2>&1; then
  echo 'FAIL: unavailable readiness was swallowed' >&2; exit 1
fi
[[ "$(cat "$work/probes")" == 30 ]]
[[ "$(grep -c '^sleep$' "$work/calls")" == 29 ]]
if grep -q 'exec -T postgres\|setup-token\|RAW_' "$work/calls" "$work/out"; then
  echo 'FAIL: unavailable readiness queried SQL, issued a token or leaked raw output' >&2; exit 1
fi
grep -q 'readiness timed out' "$work/out"
rm "$work/probes"

for state in complete issued; do
  : > "$work/calls"
  TEST_STATE="$state" bash scripts/stack-admin-token.sh --startup > "$work/out"
  if grep -q 'setup-token' "$work/calls"; then
    echo 'FAIL: startup reissued a token' >&2; exit 1
  fi
done

: > "$work/calls"
TEST_STATE=empty ROTATE=1 bash scripts/stack-admin-token.sh --startup > "$work/out"
grep -q 'setup-token' "$work/calls"
if grep -q -- '--rotate' "$work/calls"; then
  echo 'FAIL: startup implicitly rotated a token' >&2; exit 1
fi
grep -q 'fake-test-only-token' "$work/out"

for mode in query-failure unexpected-state issuance-failure; do
  export TEST_STATE=empty TEST_QUERY_FAIL=0 TEST_ISSUE_FAIL=0
  case "$mode" in
    query-failure) TEST_QUERY_FAIL=1 ;;
    unexpected-state) TEST_STATE=unexpected ;;
    issuance-failure) TEST_ISSUE_FAIL=1 ;;
  esac
  if bash scripts/stack-admin-token.sh --startup > "$work/out" 2>&1; then
    echo "FAIL: $mode was swallowed" >&2; exit 1
  fi
  if grep -q 'RAW_DATABASE_SECRET' "$work/out"; then
    echo 'FAIL: database error leaked raw output' >&2; exit 1
  fi
done
unset TEST_QUERY_FAIL TEST_ISSUE_FAIL TEST_STATE
: > "$work/calls"
ROTATE=1 bash scripts/stack-admin-token.sh > "$work/out"
grep -q -- '--rotate' "$work/calls"
if grep -q 'exec -T postgres\|/readyz' "$work/calls"; then
  echo 'FAIL: explicit issuance used startup preflight' >&2; exit 1
fi
echo 'PASS: bounded readiness, startup skips, fresh issuance, no implicit rotation, real failures and explicit rotation'
