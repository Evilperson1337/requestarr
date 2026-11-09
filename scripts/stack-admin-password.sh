#!/usr/bin/env bash
# Prints the local administrator's one-time password from the test stack's log, when the
# stack created the account on this start (D040). Prints nothing when there is none: the
# database already had users, or the administrator already chose a password and the log
# line is gone. Dev stack only; the password is the one the container already logs.
set -euo pipefail
cd "$(dirname "$0")/.."
docker compose -f compose.test.yaml logs --no-color requestarr 2>/dev/null | python3 -c '
import ast, re, sys
found = None
for line in sys.stdin:
    m = re.search(r"one_time_password=(\"(?:[^\"\\]|\\.)*\"|\S+)", line)
    if m:
        v = m.group(1)
        found = ast.literal_eval(v) if v.startswith("\"") else v
if found:
    print(found)
'
