#!/usr/bin/env bash
# Reads a running Requestarr with a diagnostics token (D043): its recent server log, operational
# status, sync runs, library connections and where each credential comes from. It makes only GET
# requests to the five routes a token may read, prints one readable line per fact, and replaces any
# occurrence of the token in what it prints. The token is passed to curl on a private file
# descriptor, never on a command line.
#
# Setup, once, by the superuser who created the token in Settings, Logs (not in the repository):
#   mkdir -p ~/.config/requestarr-probe && chmod 700 ~/.config/requestarr-probe
#   printf '%s\n' 'https://requestarr.example' > ~/.config/requestarr-probe/diag.url
#   read -rs -p 'Diagnostics token: ' t && printf '%s\n' "$t" > ~/.config/requestarr-probe/diag.token; unset t
#   chmod 600 ~/.config/requestarr-probe/diag.token
# Override the directory with REQUESTARR_PROBE_DIR. The script refuses a token file or a directory
# that anyone else can read. The full guide is docs/DIAGNOSTICS_TOKEN.md.
#
# Run:
#   scripts/agent/requestarr-diag.sh status
#   scripts/agent/requestarr-diag.sh logs [--level debug|info|warn|error] [--after SEQ]
#   scripts/agent/requestarr-diag.sh runs
#   scripts/agent/requestarr-diag.sh connections
#   scripts/agent/requestarr-diag.sh secrets
set -euo pipefail

usage() { echo "usage: requestarr-diag.sh status | logs [--level debug|info|warn|error] [--after SEQ] | runs | connections | secrets" >&2; exit 2; }
[[ $# -ge 1 ]] || usage
cmd="$1"; shift
path=""; query=""
case "$cmd" in
  status) path=/admin/status ;;
  runs) path=/admin/job-runs ;;
  connections) path=/admin/library-connections ;;
  secrets) path=/admin/secrets ;;
  logs)
    path=/admin/logs
    while [[ $# -gt 0 ]]; do
      case "$1" in
        --level) [[ $# -ge 2 && "$2" =~ ^(debug|info|warn|error)$ ]] || usage; query+="${query:+&}level=$2"; shift 2 ;;
        --after) [[ $# -ge 2 && "$2" =~ ^[0-9]+$ ]] || usage; query+="${query:+&}after=$2"; shift 2 ;;
        *) usage ;;
      esac
    done ;;
  *) usage ;;
esac
[[ "$cmd" == logs || $# -eq 0 ]] || usage

dir="${REQUESTARR_PROBE_DIR:-$HOME/.config/requestarr-probe}"
url_file="$dir/diag.url"
token_file="$dir/diag.token"
[[ -r "$url_file" && -r "$token_file" ]] || { echo "missing $url_file or $token_file; see the setup in this script's header" >&2; exit 2; }
perms="$(stat -c '%a' "$token_file")"
[[ "$perms" == 600 || "$perms" == 400 ]] || { echo "refusing: $token_file must be mode 600 (it is $perms)" >&2; exit 2; }
dir_perms="$(stat -c '%a' "$dir")"
(( (8#$dir_perms & 8#077) == 0 )) || { echo "refusing: $dir must be mode 700 (it is $dir_perms)" >&2; exit 2; }
base="$(tr -d '[:space:]' < "$url_file")"
base="${base%/}"
[[ "$base" =~ ^https?://[^/?#@]+$ ]] || { echo "diag.url must be a bare http(s) origin such as https://requestarr.example" >&2; exit 2; }
token="$(tr -d '[:space:]' < "$token_file")"
[[ -n "$token" ]] || { echo "$token_file is empty" >&2; exit 2; }
for tool in curl python3; do command -v "$tool" >/dev/null || { echo "$tool is required" >&2; exit 2; }; done

body="$(mktemp)"
trap 'rm -f "$body"' EXIT
config='silent
max-time = 20
max-redirs = 0'
config+=$'\n'"header = \"Authorization: Bearer $token\""
url="$base/api/v1$path${query:+?$query}"
code="$(curl -K <(printf '%s\n' "$config") -H 'Accept: application/json' -o "$body" -w '%{http_code}' "$url" 2>/dev/null)" || code=000
unset config token

TOKEN_FILE="$token_file" CMD="$cmd" CODE="$code" python3 -I -c '
import json, os, sys
token = open(os.environ["TOKEN_FILE"]).read().strip()
def clean(v):
    s = "" if v is None else str(v)
    s = s.replace(token, "[token]") if token else s
    return " ".join(s.split())
def out(*parts): print(clean(" ".join(str(p) for p in parts)))
code = os.environ["CODE"]
raw = open(sys.argv[1], "rb").read()
try:
    doc = json.loads(raw)
except Exception:
    doc = None
if code != "200":
    if code == "000":
        out("no response: network, DNS or TLS failure")
    else:
        err = doc.get("error", {}) if isinstance(doc, dict) else {}
        out("HTTP", code, clean(err.get("code", "")), clean(err.get("message", "")) if err else "(not a Requestarr answer)")
    sys.exit(1)
if not isinstance(doc, (dict, list)):
    out("HTTP 200 but the body is not JSON"); sys.exit(1)
def run(r):
    if not r: return "no run"
    s = "%s %s started %s" % (r.get("trigger", ""), r.get("status", ""), r.get("startedAt", ""))
    if r.get("finishedAt"): s += " finished %s" % r["finishedAt"]
    if r.get("error"): s += " error: %s" % r["error"]
    return s
cmd = os.environ["CMD"]
if cmd == "logs":
    for e in doc.get("entries", []):
        attrs = "".join(" %s=%s" % (a.get("key"), a.get("value")) for a in e.get("attrs", []))
        out(e.get("seq"), e.get("time"), e.get("level", ""), "%s%s" % (e.get("message", ""), attrs))
elif cmd == "status":
    for p in doc.get("providers", []):
        out("provider", p.get("key"), "configured" if p.get("configured") else "not configured", p.get("note", ""))
    for s in doc.get("sources", []):
        out("source", s.get("slug"), "configured" if s.get("configured") else "not configured", "|", run(s.get("lastRun")),
            "| last success", s.get("lastSuccessAt") or "never", "| last full snapshot", s.get("lastSnapshotAt") or "never",
            ("| error: " + s["lastError"]) if s.get("lastError") else "", ("| note: " + s["note"]) if s.get("note") else "")
    for e in doc.get("recentErrors", []):
        out("recent error", e.get("source"), run(e.get("run")))
elif cmd == "runs":
    for s in doc.get("sources", []):
        for r in s.get("runs", []):
            out(s.get("name") or s.get("sourceId"), run(r))
elif cmd == "connections":
    for c in doc.get("connections", []):
        creds = ", ".join("%s %s%s" % (x.get("secret"), x.get("source"), " (unreadable)" if x.get("unreadable") else "") for x in c.get("credentials", []))
        out(c.get("slug"), "|", "url", c.get("url") or "none", "(environment)" if c.get("urlFromEnvironment") else "(saved)" if c.get("saved") else "", "|",
            "active" if c.get("active") else "not active", "| missing:", ", ".join(c.get("missing", [])) or "nothing", "| credentials:", creds or "none")
elif cmd == "secrets":
    out("stored secrets enabled (master key valid):", "yes" if doc.get("enabled") else "no")
    for s in doc.get("secrets", []):
        out(s.get("name"), "|", "configured" if s.get("configured") else "not configured", "| source", s.get("source"), "| env", s.get("envVar"),
            "| pending restart" if s.get("pendingRestart") else "")
' "$body"
