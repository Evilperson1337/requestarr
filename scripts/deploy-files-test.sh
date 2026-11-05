#!/usr/bin/env bash
# Deployment-file check (seam 3 of .scratch/unraid-deployment/spec.md). Resolves
# deploy/unraid/compose.yaml with `docker compose config` against a fixture
# environment file; no containers are started and resolved secrets are never
# printed. Each artifact has its own section: compose file, example environment file,
# Unraid template (checked by scripts/deploy-template-check.py).
set -euo pipefail
cd "$(dirname "$0")/.."
command -v docker >/dev/null || { echo 'FAIL: docker is required' >&2; exit 1; }
command -v python3 >/dev/null || { echo 'FAIL: python3 is required' >&2; exit 1; }
unset COMPOSE_FILE COMPOSE_ENV_FILES COMPOSE_DISABLE_ENV_FILE COMPOSE_PROJECT_NAME
unset "${!REQUESTARR_@}" POSTGRES_PASSWORD 2>/dev/null || true

dir=deploy/unraid
compose=$dir/compose.yaml
example=$dir/requestarr.env.example
template=$dir/requestarr.xml
pinned_tag=v0.1.0-rc.1
# Independent list of what an operator must supply (the spec's settings).
required=(REQUESTARR_TAG POSTGRES_PASSWORD REQUESTARR_PUBLIC_URL REQUESTARR_OIDC_ISSUER
  REQUESTARR_OIDC_CLIENT_ID REQUESTARR_OIDC_CLIENT_SECRET REQUESTARR_SESSION_HMAC_SECRET REQUESTARR_SECRETS_KEY)
secrets=(POSTGRES_PASSWORD REQUESTARR_OIDC_CLIENT_SECRET REQUESTARR_SESSION_HMAC_SECRET REQUESTARR_SECRETS_KEY)

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
failures=0
fail() { echo "FAIL: $*" >&2; failures=$((failures + 1)); }

# Fixture values: shaped like real ones so resolution can be asserted; fake.
write_fixture() { # file [omit-key]
  local f=$1 omit=${2:-}
  cat > "$f" <<ENV
REQUESTARR_TAG=$pinned_tag
POSTGRES_PASSWORD=fixturepassword0123
REQUESTARR_PUBLIC_URL=https://requests.example.test
REQUESTARR_OIDC_ISSUER=https://id.example.test/realms/requestarr
REQUESTARR_OIDC_CLIENT_ID=requestarr
REQUESTARR_OIDC_CLIENT_SECRET=fixture-client-secret
REQUESTARR_SESSION_HMAC_SECRET=fixture-session-secret-0123456789abcdef0123
REQUESTARR_SECRETS_KEY=00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff
ENV
  if [[ -n $omit ]]; then sed -i "/^$omit=/d" "$f"; fi
}

# resolve <compose> <env> -> JSON on stdout; non-zero when compose rejects it.
resolve() { docker compose -f "$1" --env-file "$2" config --format json 2>/dev/null; }

# ---- Section: compose file -------------------------------------------------
check_compose() { # compose-file
  local c=$1 env="$work/fixture.env" json
  write_fixture "$env"
  json="$(resolve "$c" "$env")" || { fail "$c does not resolve against the fixture environment"; return 1; }
  printf '%s' "$json" | PINNED="$pinned_tag" python3 -c '
import json, os, sys
m = json.load(sys.stdin)
errs = []
svc = m["services"]
if set(svc) != {"postgres", "requestarr"}:
    errs.append("services must be exactly postgres and requestarr, got %s" % sorted(svc))
r, p = svc.get("requestarr", {}), svc.get("postgres", {})
if r.get("image") != "ghcr.io/evilperson1337/requestarr:" + os.environ["PINNED"]:
    errs.append("requestarr image is %r, want the pinned tag" % r.get("image"))
if r.get("depends_on", {}).get("postgres", {}).get("condition") != "service_healthy":
    errs.append("requestarr must wait for postgres to be service_healthy")
if "healthcheck" not in p:
    errs.append("postgres needs a healthcheck")
if "healthcheck" in r:
    errs.append("requestarr must use the image health check, not its own")
if r.get("environment", {}).get("REQUESTARR_ENV") != "production":
    errs.append("requestarr must set REQUESTARR_ENV=production, got %r" % r.get("environment", {}).get("REQUESTARR_ENV"))
vols = [v for v in p.get("volumes", []) if v.get("target") == "/var/lib/postgresql/data"]
if not vols or vols[0].get("type") != "volume" or not vols[0].get("source"):
    errs.append("postgres data must be on a named volume")
for e in errs:
    print("FAIL: " + e, file=sys.stderr)
sys.exit(1 if errs else 0)
' || return 1
}

# Every required setting must be refused when missing: that is what "no usable
# default" means for secrets and for the tag. Returns non-zero (printing why)
# when any omission still resolves.
check_no_defaults() { # compose-file
  local c=$1 key rc=0
  for key in "${required[@]}"; do
    write_fixture "$work/omit.env" "$key"
    if resolve "$c" "$work/omit.env" >/dev/null; then
      echo "FAIL: $c resolves without $key, so it has a default" >&2
      rc=1
    fi
  done
  return $rc
}

# The tag must resolve to the exact pre-release, never a floating one.
check_tag_pinned() { # compose-file
  local c=$1 image
  write_fixture "$work/fixture.env"
  image="$(resolve "$c" "$work/fixture.env" | python3 -c 'import json,sys; print(json.load(sys.stdin)["services"]["requestarr"]["image"])')" || return 1
  if [[ $image != "ghcr.io/evilperson1337/requestarr:$pinned_tag" ]]; then
    echo "FAIL: requestarr image resolves to $image, want tag $pinned_tag" >&2
    return 1
  fi
  if grep -Eq 'REQUESTARR_TAG:-|:latest|:main|:edge' "$c"; then
    echo "FAIL: $c hard-codes or defaults a floating tag" >&2
    return 1
  fi
}

# ---- Section: example environment file -------------------------------------
value_of() { sed -n "s/^$1=//p" "$example" | head -n1; }

check_example() { # example-file
  local ex=$1 key v rc=0
  if [[ ! -f $ex ]]; then echo "FAIL: $ex is missing" >&2; return 1; fi
  for key in "${required[@]}"; do
    if ! grep -Eq "^$key=" "$ex"; then echo "FAIL: $ex has no $key= line" >&2; rc=1; fi
  done
  [[ $(sed -n 's/^REQUESTARR_TAG=//p' "$ex") == "$pinned_tag" ]] || { echo "FAIL: $ex must set REQUESTARR_TAG=$pinned_tag" >&2; rc=1; }
  for key in "${secrets[@]}"; do
    v="$(sed -n "s/^$key=//p" "$ex" | head -n1)"
    case $v in CHANGE_ME*) ;; *) echo "FAIL: $key in $ex must be a CHANGE_ME placeholder" >&2; rc=1 ;; esac
  done
  # The two server-validated secrets must not pass validation as written.
  v="$(sed -n 's/^REQUESTARR_SESSION_HMAC_SECRET=//p' "$ex" | head -n1)"
  ((${#v} < 32)) || { echo "FAIL: session secret placeholder is long enough to be accepted" >&2; rc=1; }
  v="$(sed -n 's/^REQUESTARR_SECRETS_KEY=//p' "$ex" | head -n1)"
  KEY="$v" python3 -c '
import base64, binascii, os, sys
k = os.environ["KEY"]
ok = False
try: ok = len(binascii.unhexlify(k)) == 32
except Exception: pass
if not ok:
    try: ok = len(base64.b64decode(k, validate=True)) == 32
    except Exception: pass
sys.exit(1 if ok else 0)' || { echo "FAIL: master key placeholder is accepted as a 32-byte key" >&2; rc=1; }
  return $rc
}

# ---- Section: Unraid template ----------------------------------------------
# Public addresses only (the private domain must never appear in this repo).
template_url=https://raw.githubusercontent.com/evilperson1337/requestarr/main/deploy/unraid/requestarr.xml
project_url=https://github.com/evilperson1337/requestarr
# Names of the requestarr service's environment variables, from the resolved compose file.
compose_env_names() { # compose-file
  write_fixture "$work/fixture.env"
  resolve "$1" "$work/fixture.env" | python3 -c 'import json,sys; print(" ".join(json.load(sys.stdin)["services"]["requestarr"]["environment"]))'
}
check_template() { # template-file [compose-file]
  local t=$1 c=${2:-$compose}
  if [[ ! -f $t ]]; then echo "FAIL: $t is missing" >&2; return 1; fi
  if command -v xmllint >/dev/null; then
    xmllint --noout "$t" 2>/dev/null || { echo "FAIL: $t is not well-formed XML" >&2; return 1; }
  fi
  TEMPLATE="$t" PINNED="$pinned_tag" PROJECT="$project_url" TEMPLATE_URL="$template_url" \
    COMPOSE_ENV="$(compose_env_names "$c")" REQUIRED="${required[*]}" SECRETS="${secrets[*]}" CONFIG=internal/config/config.go \
    python3 -I "$PWD/scripts/deploy-template-check.py"
}

# Mutations prove each guard can fail: the check must reject a broken compose.
expect_rejected() { # description command...
  local what=$1; shift
  if "$@" 2>/dev/null; then fail "check accepted $what"; fi
}

check_compose "$compose" || failures=$((failures + 1))
check_no_defaults "$compose" || failures=$((failures + 1))
check_tag_pinned "$compose" || failures=$((failures + 1))
check_example "$example" || failures=$((failures + 1))
check_template "$template" || failures=$((failures + 1))

# mutate <in> <out> <var> <replacement>: swap ${var:?...} for the replacement.
mutate() {
  python3 -c 'import re, sys
src, dst, var, rep = sys.argv[1:]
text = open(src).read()
out = re.sub(r"\$\{" + var + r":\?[^}]*\}", lambda _: rep, text)
assert out != text, "mutation did not apply"
open(dst, "w").write(out)' "$@"
}
for key in "${secrets[@]}"; do
  mutate "$compose" "$work/default.yaml" "$key" "\${$key:-usable-default}"
  expect_rejected "a usable default for $key" check_no_defaults "$work/default.yaml"
done
sed 's#^\( *REQUESTARR_ENV:\) production#\1 development#' "$compose" > "$work/env.yaml"
expect_rejected "a compose file running in development" check_compose "$work/env.yaml"
grep -v '^ *REQUESTARR_ENV:' "$compose" > "$work/env.yaml"
expect_rejected "a compose file without REQUESTARR_ENV" check_compose "$work/env.yaml"
mutate "$compose" "$work/floating.yaml" REQUESTARR_TAG latest
expect_rejected "a floating tag" check_tag_pinned "$work/floating.yaml"
mutate "$compose" "$work/floating.yaml" REQUESTARR_TAG '${REQUESTARR_TAG:-latest}'
expect_rejected "a defaulted tag" check_no_defaults "$work/floating.yaml"
for key in "${required[@]}"; do
  grep -v "^$key=" "$example" > "$work/short.env.example" 2>/dev/null || true
  expect_rejected "an example missing $key" check_example "$work/short.env.example"
done

# Template mutations: each edit of a copy must be rejected.
mutate_template() { # description sed-expression
  local what=$1 expr=$2
  sed -E "$expr" "$template" > "$work/mutant.xml"
  if cmp -s "$template" "$work/mutant.xml"; then fail "template mutation did not apply: $what"; return; fi
  expect_rejected "$what" check_template "$work/mutant.xml"
}
for key in REQUESTARR_SESSION_HMAC_SECRET REQUESTARR_SECRETS_KEY REQUESTARR_OIDC_CLIENT_SECRET REQUESTARR_DATABASE_URL; do
  mutate_template "unmasked $key" "/Target=\"$key\"/ s/Mask=\"true\"/Mask=\"false\"/"
  mutate_template "a default for $key" "/Target=\"$key\"/ s/Default=\"\"/Default=\"usable-default\"/"
  mutate_template "$key not required" "/Target=\"$key\"/ s/Required=\"true\"/Required=\"false\"/"
done
for key in REQUESTARR_PUBLIC_URL REQUESTARR_OIDC_ISSUER REQUESTARR_OIDC_CLIENT_ID REQUESTARR_OIDC_CLIENT_SECRET \
  REQUESTARR_SESSION_HMAC_SECRET REQUESTARR_SECRETS_KEY REQUESTARR_DATABASE_URL REQUESTARR_LOG_LEVEL \
  REQUESTARR_HTTP_ALLOWED_NETWORKS REQUESTARR_ENV 8080; do
  mutate_template "a template without $key" "/Target=\"$key\"/ d"
done
mutate_template "an environment default of development" '/Target="REQUESTARR_ENV"/ s/Default="production"/Default="development"/'
mutate_template "an environment value of development" '/Target="REQUESTARR_ENV"/ s#>production<#>development<#'
mutate_template "an environment field marked required" '/Target="REQUESTARR_ENV"/ s/Required="false"/Required="true"/'
mutate_template "an environment field shown by default" '/Target="REQUESTARR_ENV"/ s/Display="advanced"/Display="always"/'
mutate_template "a floating tag" "s#(<Repository>[^<]*):$pinned_tag#\\1:latest#"
mutate_template "a tag-less repository" "s#(<Repository>[^<]*):$pinned_tag#\\1#"
mutate_template "a wrong tag in Branch" "s#<Tag>$pinned_tag#<Tag>latest#"
mutate_template "a malformed document" 's#</Container>##'
mutate_template "a wrong root version" 's#<Container version="2">#<Container version="1">#'
mutate_template "a variable the code does not read" 's#Target="REQUESTARR_LOG_LEVEL"#Target="REQUESTARR_LOG_LEVELS"#'
mutate_template "a provider credential field" 's#Target="REQUESTARR_LOG_LEVEL"#Target="REQUESTARR_PLEX_TOKEN"#'
mutate_template "a missing icon" 's#<Icon>[^<]*</Icon>#<Icon></Icon>#'
mutate_template "the private domain in the overview" "s#Requestarr is a shared#Requestarr (git.all""our""media.com) is a shared#"
mutate_template "a non-public project link" 's#<Project>[^<]*</Project>#<Project>http://localhost/x</Project>#'
mutate_template "a master key command without openssl" '/REQUESTARR_SECRETS_KEY/ s/openssl rand -base64/some command/'
mutate_template "a master key with no backup advice" '/REQUESTARR_SECRETS_KEY/ s/separately from the database/somewhere/'
mutate_template "an appdata path" 's#</Container>#<Config Name="Data" Target="/data" Type="Path" Display="always" Required="false" Mask="false"/></Container>#'
mutate_template "a description without a purpose" '/Target="REQUESTARR_PUBLIC_URL"/ s/Description="[^"]*"/Description="URL"/'
# The template must also notice a compose file that gained a setting.
sed 's#^\(      REQUESTARR_PUBLIC_URL:.*\)#\1\n      REQUESTARR_NEW_SETTING: ${REQUESTARR_NEW_SETTING:-x}#' "$compose" > "$work/extra.yaml"
expect_rejected "a compose setting with no template field" check_template "$template" "$work/extra.yaml"

# ---- Section: guide ----------------------------------------------------------
# Every REQUESTARR_* name docs/UNRAID.md mentions must be read by the configuration
# code (REQUESTARR_TAG is the compose file's own image-tag variable, not the app's).
guide=docs/UNRAID.md
check_guide_names() { # guide-file
  local g=$1 name rc=0 names
  if [[ ! -f $g ]]; then echo "FAIL: $g is missing" >&2; return 1; fi
  names="$(grep -Eo 'REQUESTARR_[A-Z0-9_]+' "$g" | sort -u)"
  [[ -n $names ]] || { echo "FAIL: $g mentions no REQUESTARR_ names" >&2; return 1; }
  for name in $names; do
    [[ $name == REQUESTARR_TAG ]] && continue
    if ! grep -rqF --include='*.go' --exclude='*_test.go' "\"$name\"" internal/config internal/secrets internal/sources; then
      echo "FAIL: $g mentions $name, which the configuration code does not read" >&2
      rc=1
    fi
  done
  return $rc
}
check_guide_names "$guide" || failures=$((failures + 1))
sed 's#REQUESTARR_HTTP_ALLOWED_NETWORKS#REQUESTARR_HTTP_ALLOWED_NETWORK#' "$guide" > "$work/guide-mutant.md"
expect_rejected "a guide naming a variable the code does not read" check_guide_names "$work/guide-mutant.md"
cp "$guide" "$work/guide-mutant.md"; echo 'Set REQUESTARR_NOT_A_SETTING=1.' >> "$work/guide-mutant.md"
expect_rejected "a guide naming an unknown variable" check_guide_names "$work/guide-mutant.md"

# The SWAG reverse-proxy sample: filled in (no template placeholders or "remove" notes), pointing at the
# container name and web port the template uses, over http inside the Docker network.
swag=deploy/swag/requestarr.subdomain.conf.sample
check_swag() { # conf-file
  local f="$1" rc=0 port
  [[ -f "$f" ]] || { echo "FAIL: $f is missing" >&2; return 1; }
  if grep -Eq '<[a-z_ ]+>|REMOVE THIS LINE' "$f"; then echo "FAIL: $f still has template placeholders or notes" >&2; rc=1; fi
  grep -Eq '^    server_name requestarr\.\*;$' "$f" || { echo "FAIL: $f: server_name must be requestarr.*" >&2; rc=1; }
  grep -Eq '^        set \$upstream_app requestarr;$' "$f" || { echo "FAIL: $f: upstream app must be the container name requestarr" >&2; rc=1; }
  grep -Eq '^        set \$upstream_proto http;$' "$f" || { echo "FAIL: $f: upstream proto must be http" >&2; rc=1; }
  grep -Eq '^        include /config/nginx/proxy\.conf;$' "$f" || { echo "FAIL: $f: must include SWAG's proxy.conf" >&2; rc=1; }
  port="$(sed -n 's/^        set \$upstream_port \([0-9]*\);$/\1/p' "$f")"
  grep -Eq "Target=\"8080\" Default=\"$port\"" "$template" || { echo "FAIL: $f: upstream port '$port' is not the template's web port" >&2; rc=1; }
  return $rc
}
check_swag "$swag" || failures=$((failures + 1))
sed 's#set \$upstream_app requestarr;#set $upstream_app <container_name>;#' "$swag" > "$work/swag-mutant.conf"
expect_rejected "a SWAG conf with a placeholder upstream" check_swag "$work/swag-mutant.conf"
sed 's#set \$upstream_port 8080;#set $upstream_port 8081;#' "$swag" > "$work/swag-mutant.conf"
expect_rejected "a SWAG conf whose port differs from the template" check_swag "$work/swag-mutant.conf"
{ cat "$swag"; echo '# REMOVE THIS LINE BEFORE SUBMITTING'; } > "$work/swag-mutant.conf"
expect_rejected "a SWAG conf with a leftover note" check_swag "$work/swag-mutant.conf"
sed 's#server_name requestarr\.\*;#server_name other.*;#' "$swag" > "$work/swag-mutant.conf"
expect_rejected "a SWAG conf for a different name" check_swag "$work/swag-mutant.conf"

if ((failures)); then echo "deploy files: $failures check(s) failed" >&2; exit 1; fi
echo 'deploy files: ok'
