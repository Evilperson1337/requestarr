#!/usr/bin/env bash
# Tests scripts/agent/requestarr-diag.sh against a local fake server, as the script is meant to be used:
# a canary token that the server echoes back, a token file others can read, every subcommand.
set -euo pipefail
cd "$(dirname "$0")/../.."
for tool in curl python3; do command -v "$tool" >/dev/null || { echo "SKIP: $tool not found; requestarr-diag test not run"; exit 0; }; done
work="$(mktemp -d)"
server_pid=""
trap '[[ -n "$server_pid" ]] && kill "$server_pid" 2>/dev/null || true; rm -rf "$work"' EXIT
fail() { echo "FAIL: $*" >&2; exit 1; }

canary='rqd_CANARYcanaryCANARYcanary0123456789_-abcdefg'
cat > "$work/server.py" <<'PY'
import json, sys
from http.server import BaseHTTPRequestHandler, HTTPServer
from urllib.parse import urlparse, parse_qs
canary, log_path, port_path = sys.argv[1:4]
class H(BaseHTTPRequestHandler):
    def log_message(self, *a): pass
    def send(self, code, doc):
        body = json.dumps(doc).encode()
        self.send_response(code); self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body))); self.end_headers(); self.wfile.write(body)
    def do_GET(self):
        u = urlparse(self.path); q = parse_qs(u.query)
        with open(log_path, "a") as f: f.write(self.path + "\n")
        if self.headers.get("Authorization") != "Bearer " + canary:
            return self.send(401, {"error": {"code": "authentication_required", "message": "bad token", "requestId": "r1"}})
        p = u.path
        if p == "/api/v1/admin/logs":
            entries = [
                {"seq": 1, "time": "2026-11-08T10:00:00Z", "level": "INFO", "message": "started", "attrs": []},
                {"seq": 2, "time": "2026-11-08T10:00:01Z", "level": "WARN", "message": "echo " + canary, "attrs": [{"key": "step", "value": "listing games at offset 400"}, {"key": "tok", "value": canary}]},
                {"seq": 3, "time": "2026-11-08T10:00:02Z", "level": "ERROR", "message": "sync failed\nsecond line", "attrs": []},
            ]
            rank = {"debug": 0, "info": 1, "warn": 2, "error": 3}
            lv = q.get("level", ["debug"])[0]
            after = int(q.get("after", ["0"])[0])
            entries = [e for e in entries if rank[e["level"].lower()] >= rank[lv] and e["seq"] > after]
            return self.send(200, {"entries": entries, "capacity": 1000})
        if p == "/api/v1/admin/status":
            return self.send(200, {"providers": [{"key": "tmdb", "name": "TMDB", "configured": True}],
                "sources": [{"slug": "romm", "name": "RomM", "configured": True, "lastRun": {"id": "a", "sourceId": "b", "trigger": "manual", "status": "failed", "startedAt": "2026-11-08T10:00:00Z", "finishedAt": "2026-11-08T10:00:05Z", "error": "boom " + canary}, "lastSuccessAt": None, "lastSnapshotAt": None, "lastError": "boom", "note": "stored secret unreadable"}],
                "recentErrors": []})
        if p == "/api/v1/admin/job-runs":
            return self.send(200, {"sources": [{"sourceId": "b", "name": "RomM", "runs": [{"id": "a", "sourceId": "b", "trigger": "schedule", "status": "succeeded", "startedAt": "2026-11-08T09:00:00Z", "finishedAt": "2026-11-08T09:00:09Z"}]}]})
        if p == "/api/v1/admin/library-connections":
            return self.send(200, {"connections": [{"slug": "romm", "name": "RomM", "url": "https://romm.example", "urlFromEnvironment": False, "urlEnv": "REQUESTARR_ROMM_URL", "saved": True, "takesUsername": False, "username": "", "active": False, "missing": ["token"], "credentials": [{"secret": "romm_token", "label": "API token", "configured": False, "source": "none", "unreadable": False}]}]})
        if p == "/api/v1/admin/secrets":
            return self.send(200, {"enabled": True, "secrets": [{"name": "romm_token", "label": "RomM token", "envVar": "REQUESTARR_ROMM_TOKEN", "configured": False, "source": "none", "unreadable": False, "pendingRestart": False}]})
        return self.send(401, {"error": {"code": "authentication_required", "message": "route not readable with " + canary, "requestId": "r2"}})
srv = HTTPServer(("127.0.0.1", 0), H)
open(port_path, "w").write(str(srv.server_address[1]))
srv.serve_forever()
PY
python3 -I "$work/server.py" "$canary" "$work/requests.log" "$work/port" &
server_pid=$!
for _ in $(seq 100); do [[ -s "$work/port" ]] && break; sleep 0.05; done
[[ -s "$work/port" ]] || fail "fake server did not start"
touch "$work/requests.log"

# A curl that records its argument list, to show the token is never on a command line.
mkdir "$work/bin"
real_curl="$(command -v curl)"
printf '#!/bin/sh\nprintf "%%s\\n" "$@" >> "%s/argv.log"\nexec "%s" "$@"\n' "$work" "$real_curl" > "$work/bin/curl"
chmod +x "$work/bin/curl"

dir="$work/probe"; mkdir -m 700 "$dir"
port="$(cat "$work/port")"
printf 'http://127.0.0.1:%s\n' "$port" > "$dir/diag.url"
printf '%s\n' "$canary" > "$dir/diag.token"; chmod 600 "$dir/diag.token"
diag() { REQUESTARR_PROBE_DIR="$dir" PATH="$work/bin:$PATH" scripts/agent/requestarr-diag.sh "$@"; }
no_canary() { if grep -q "CANARY" <<<"$1"; then fail "$2 printed the token: $1"; fi; }

# Each subcommand prints readable output, and the echoed token is replaced.
out="$(diag status 2>&1)"; no_canary "$out" status
grep -q 'romm' <<<"$out" && grep -q 'failed' <<<"$out" && grep -q 'stored secret unreadable' <<<"$out" || fail "status output unreadable: $out"
grep -q '\[token\]' <<<"$out" || fail "status did not redact the echoed token: $out"
out="$(diag runs 2>&1)"; no_canary "$out" runs
grep -q 'succeeded' <<<"$out" && grep -q 'RomM' <<<"$out" || fail "runs output unreadable: $out"
out="$(diag connections 2>&1)"; no_canary "$out" connections
grep -q 'romm' <<<"$out" && grep -q 'missing: token' <<<"$out" || fail "connections output unreadable: $out"
out="$(diag secrets 2>&1)"; no_canary "$out" secrets
grep -q 'romm_token' <<<"$out" && grep -q 'REQUESTARR_ROMM_TOKEN' <<<"$out" || fail "secrets output unreadable: $out"

# logs: one line per record, filters sent to the server, token redacted, newline folded.
out="$(diag logs 2>&1)"; no_canary "$out" logs
[[ "$(wc -l <<<"$out")" == 3 ]] || fail "logs must print one line per record: $out"
grep -q '^2 2026-11-08T10:00:01Z WARN echo \[token\] step=listing games at offset 400 tok=\[token\]$' <<<"$out" || fail "log line format changed: $out"
out="$(diag logs --level warn 2>&1)"
[[ "$(wc -l <<<"$out")" == 2 ]] || fail "--level warn must drop info: $out"
out="$(diag logs --level error --after 2 2>&1)"
[[ "$(wc -l <<<"$out")" == 1 ]] && grep -q 'sync failed second line' <<<"$out" || fail "--level/--after: $out"
grep -q 'level=error' "$work/requests.log" && grep -q 'after=2' "$work/requests.log" || fail "the filters were not sent to the server"
if diag logs --level loud >/dev/null 2>&1; then fail "an invalid level must be refused"; fi
if diag logs --after x >/dev/null 2>&1; then fail "a non-numeric --after must be refused"; fi
if diag nonsense >/dev/null 2>&1; then fail "an unknown subcommand must be refused"; fi
if diag >/dev/null 2>&1; then fail "no subcommand must be refused"; fi

# A wrong token or a refused route prints the status and the server's code, never the token, and fails.
printf 'rqd_wrong\n' > "$dir/diag.token"
if out="$(diag status 2>&1)"; then fail "a rejected token must exit non-zero"; fi
grep -q '401' <<<"$out" && grep -q 'authentication_required' <<<"$out" || fail "rejection not explained: $out"
printf '%s\n' "$canary" > "$dir/diag.token"

# The token never appeared on a command line.
[[ -s "$work/argv.log" ]] || fail "the curl shim saw nothing"
if grep -q CANARY "$work/argv.log"; then fail "the token was passed on a command line"; fi

# A token that is not rqd_ plus URL-safe characters is refused with a message before any request, so it
# can never break out of the curl config it is written into.
before="$(wc -l < "$work/requests.log")"
for bad in 'rqd_ab"cd' 'rqd_ab\cd' 'rqd_ab cd' 'rqd_' 'abc_notadiagnosticstoken' 'rqd_ab"
header = "X-Evil: 1'; do
  printf '%s\n' "$bad" > "$dir/diag.token"
  if out="$(diag status 2>&1)"; then fail "a malformed token must be refused: $bad"; fi
  grep -q 'rqd_' <<<"$out" || fail "the refusal does not say what a token looks like: $out"
done
printf '%s\n' "$canary" > "$dir/diag.token"
[[ "$(wc -l < "$work/requests.log")" == "$before" ]] || fail "a request was made with a malformed token"

# scripts/agent/romm-probe.sh builds its curl config the same way: a token with a double quote, a
# backslash, whitespace or a control character is refused before any request.
printf 'http://127.0.0.1:%s\n' "$port" > "$dir/romm.url"
romm() { REQUESTARR_PROBE_DIR="$dir" PATH="$work/bin:$PATH" scripts/agent/romm-probe.sh "$@"; }
for bad in 'tok"en' 'tok\en' 'tok en' $'tok\ten' $'tok\ren'; do
  printf '%s\n' "$bad" > "$dir/romm.token"; chmod 600 "$dir/romm.token"
  if out="$(romm 2>&1)"; then fail "romm-probe must refuse a token with a quote, backslash or whitespace: $bad"; fi
  grep -q 'token' <<<"$out" && ! grep -q 'HTTP\|Authorization' <<<"$out" || fail "romm-probe refusal unclear: $out"
done
[[ "$(wc -l < "$work/requests.log")" == "$before" ]] || fail "romm-probe made a request with a malformed token"
rm "$dir/romm.token" "$dir/romm.url"

# A token file others can read, a directory others can enter, or an address with a path or credentials
# is refused before any request.
before="$(wc -l < "$work/requests.log")"
chmod 644 "$dir/diag.token"
if out="$(diag status 2>&1)"; then fail "a world-readable token file must be refused"; fi
grep -q 'mode 600' <<<"$out" || fail "refusal does not say why: $out"
no_canary "$out" refusal
chmod 640 "$dir/diag.token"
if diag status >/dev/null 2>&1; then fail "a group-readable token file must be refused"; fi
chmod 600 "$dir/diag.token"
chmod 755 "$dir"
if diag status >/dev/null 2>&1; then fail "a directory others can enter must be refused"; fi
chmod 700 "$dir"
printf 'http://user:pw@127.0.0.1:%s\n' "$port" > "$dir/diag.url"
if diag status >/dev/null 2>&1; then fail "an address with credentials must be refused"; fi
printf 'http://127.0.0.1:%s/x\n' "$port" > "$dir/diag.url"
if diag status >/dev/null 2>&1; then fail "an address with a path must be refused"; fi
[[ "$(wc -l < "$work/requests.log")" == "$before" ]] || fail "a request was made despite a refusal"
rm "$dir/diag.token"
if diag status >/dev/null 2>&1; then fail "a missing token file must be refused"; fi
echo "ok: requestarr-diag.sh behaves against the fake server"
