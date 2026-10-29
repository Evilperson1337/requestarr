#!/usr/bin/env bash
# Creates a persistent local-only master key for the disposable test stack.
set -euo pipefail

cd "$(dirname "$0")/.."
command -v openssl >/dev/null || { echo 'FAIL: openssl is required to generate REQUESTARR_SECRETS_KEY' >&2; exit 1; }

if [[ -f .env ]] && grep -Eq '^REQUESTARR_SECRETS_KEY=.+$' .env; then
  exit 0
fi

key="$(openssl rand -hex 32)"
touch .env
chmod 600 .env
if grep -q '^REQUESTARR_SECRETS_KEY=' .env; then
  sed -i "s|^REQUESTARR_SECRETS_KEY=.*|REQUESTARR_SECRETS_KEY=$key|" .env
else
  printf '\nREQUESTARR_SECRETS_KEY=%s\n' "$key" >> .env
fi
echo 'Created REQUESTARR_SECRETS_KEY in .env for the local test stack.'
