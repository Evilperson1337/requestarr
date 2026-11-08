#!/usr/bin/env bash
# Probes a live RomM server with an API token the caller never sees, to learn
# why Requestarr's connection to it fails (a 401 versus a 403, a missing scope,
# a proxy in front). It makes only GET requests and prints only what is safe:
# the HTTP status, a few response headers, and the SHAPE of a JSON answer (field
# names and types, never values). Any occurrence of the token is replaced.
#
# Setup, once, by the person who owns the token (not in the repository).
# Run these lines in bash (`bash`, then paste them): `read -rs -p` is bash-only, and fish and zsh differ:
#   mkdir -p ~/.config/requestarr-probe && chmod 700 ~/.config/requestarr-probe
#   printf '%s\n' 'https://romm.example' > ~/.config/requestarr-probe/romm.url
#   read -rs -p 'RomM token: ' t && printf '%s\n' "$t" > ~/.config/requestarr-probe/romm.token; unset t
#   chmod 600 ~/.config/requestarr-probe/romm.token
# Override the directory with REQUESTARR_PROBE_DIR. The script refuses a token
# file that anyone else can read.
#
# Run:  scripts/agent/romm-probe.sh
set -euo pipefail

dir="${REQUESTARR_PROBE_DIR:-$HOME/.config/requestarr-probe}"
url_file="$dir/romm.url"
token_file="$dir/romm.token"
[[ -r "$url_file" && -r "$token_file" ]] || { echo "missing $url_file or $token_file; see the setup in this script's header" >&2; exit 2; }
perms="$(stat -c '%a' "$token_file")"
[[ "$perms" == 600 || "$perms" == 400 ]] || { echo "refusing: $token_file must be mode 600 (it is $perms)" >&2; exit 2; }
base="$(tr -d '[:space:]' < "$url_file")"
base="${base%/}"
[[ "$base" =~ ^https?://[^/?#@]+$ ]] || { echo "romm.url must be a bare http(s) origin such as https://romm.example" >&2; exit 2; }
[[ "$base" != *your-romm-address* ]] || { echo "romm.url still holds the example address; replace it with your RomM server's address ($url_file)" >&2; exit 2; }
# The value goes into a curl config file, so a quote, backslash, whitespace or control character
# (which could end the header line and add another) is refused.
romm_token="$(<"$token_file")"
unsafe='[[:space:][:cntrl:]"\\]'
if [[ -z "$romm_token" || "$romm_token" =~ $unsafe ]]; then
  echo "refusing: $token_file must hold only the RomM token, with no quotes, backslashes, spaces or other lines" >&2; exit 2
fi
unset romm_token
for tool in curl python3; do command -v "$tool" >/dev/null || { echo "$tool is required" >&2; exit 2; }; done

requestarr_ua='Requestarr/1.0 (+https://github.com/evilperson1337/requestarr)'
browser_ua='Mozilla/5.0 (X11; Linux x86_64; rv:130.0) Gecko/20100101 Firefox/130.0'

# call LABEL PATH USER_AGENT AUTH(yes|no): one GET. The token goes to curl on a
# private file descriptor, never on a command line, so it is in no process list.
call() {
  local label="$1" path="$2" ua="$3" auth="$4" body headers code
  body="$(mktemp)"; headers="$(mktemp)"
  local config='silent
max-time = 20
max-redirs = 0'
  [[ "$auth" == yes ]] && config+=$'\n'"header = \"Authorization: Bearer $(<"$token_file")\""
  code="$(curl -K <(printf '%s\n' "$config") -A "$ua" -H 'Accept: application/json' -D "$headers" -o "$body" -w '%{http_code} %{time_total}s' "$base$path" 2>/dev/null)" || code="000 (no response: network, DNS or TLS failure)"
  TOKEN_FILE="$token_file" LABEL="$label" PATH_="$path" CODE="$code" UA_NAME="${ua%%/*}" AUTH="$auth" python3 -I -c '
import json, os, sys
token = open(os.environ["TOKEN_FILE"]).read().strip()
def clean(text):
    return text.replace(token, "[token]") if token else text
headers_file, body_file = sys.argv[1], sys.argv[2]
raw = open(headers_file, errors="replace").read()
blocks = [b for b in raw.split("\r\n\r\n") if b.strip()]
lines = blocks[-1].split("\r\n") if blocks else []
shown = {"content-type", "server", "via", "www-authenticate", "location", "cf-ray", "x-powered-by", "retry-after", "x-request-id"}
print("%s  [%s, auth=%s]" % (os.environ["LABEL"], os.environ["UA_NAME"], os.environ["AUTH"]))
print("  GET %s -> %s" % (os.environ["PATH_"], os.environ["CODE"]))
for line in lines[1:]:
    name, _, value = line.partition(":")
    if name.strip().lower() in shown:
        print("  %s: %s" % (name.strip().lower(), clean(value.strip())[:120]))
data = open(body_file, "rb").read()
def shape(v, depth=0):
    if isinstance(v, dict):
        if depth >= 2: return "object"
        return "{" + ", ".join("%s: %s" % (k, shape(x, depth + 1)) for k, x in list(v.items())[:25]) + (", ..." if len(v) > 25 else "") + "}"
    if isinstance(v, list):
        return "list(%d)%s" % (len(v), (" of " + shape(v[0], depth + 1)) if v and depth < 2 else "")
    return type(v).__name__ if v is not None else "null"
try:
    doc = json.loads(data)
except Exception:
    print("  body: %d bytes, not JSON" % len(data))
else:
    print("  json shape: " + clean(shape(doc))[:600])
    if isinstance(doc, dict):
        for key in ("total", "limit", "offset"):
            if isinstance(doc.get(key), int): print("  %s = %d" % (key, doc[key]))
        # RomM errors carry a short fixed phrase such as "Forbidden" or "Not authenticated".
        detail = doc.get("detail")
        if isinstance(detail, str) and len(detail) <= 80: print("  detail: " + clean(detail))
' "$headers" "$body"
  rm -f "$body" "$headers"
  echo
}

echo "RomM probe against $base"
echo
call "1 heartbeat (needs no login)"           /api/heartbeat                    "$requestarr_ua" no
call "2 platforms WITHOUT a token"            /api/platforms                    "$requestarr_ua" no
call "3 platforms with the token (platforms.read)" /api/platforms               "$requestarr_ua" yes
call "4 games with the token (roms.read)"     '/api/roms?limit=1&offset=0&with_char_index=false&with_filter_values=false' "$requestarr_ua" yes
call "5 current user with the token (me.read)" /api/users/me                    "$requestarr_ua" yes
call "6 platforms with the token, browser User-Agent" /api/platforms           "$browser_ua" yes

cat <<'EOF'
How to read this:
  2 answers 401 "Not authenticated": RomM itself answers; a 403 here would point to a proxy or firewall.
  3 is 200 and 4 is 403 (or the reverse): the token lacks the scope of the failing call
    (3 needs platforms.read, 4 needs roms.read). Scopes are fixed when a token is created.
  3 is 401: RomM does not recognise the token (wrong, expired or revoked).
  3 is 403 but 6 is 200: something in front of RomM filters on the User-Agent.
EOF
