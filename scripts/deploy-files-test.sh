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
pinned_tag=v0.1.0-rc.12
# Independent list of what an operator must supply (the spec's settings).
required=(REQUESTARR_TAG POSTGRES_PASSWORD REQUESTARR_PUBLIC_URL REQUESTARR_SESSION_HMAC_SECRET REQUESTARR_SECRETS_KEY)
# Optional settings (D040): OIDC is an override of what is saved in Settings, Authentication, and the
# initial administrator password is only used on a fresh install. None has a default.
optional=(REQUESTARR_OIDC_ISSUER REQUESTARR_OIDC_CLIENT_ID REQUESTARR_OIDC_CLIENT_SECRET REQUESTARR_ADMIN_PASSWORD REQUESTARR_TRUSTED_PROXIES)
# Database settings the template must ask for (the compose file derives them).
db_required=(REQUESTARR_DB_HOST REQUESTARR_DB_NAME REQUESTARR_DB_USER REQUESTARR_DB_PASSWORD)
secrets=(POSTGRES_PASSWORD REQUESTARR_SESSION_HMAC_SECRET REQUESTARR_SECRETS_KEY)

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
failures=0
fail() { echo "FAIL: $*" >&2; failures=$((failures + 1)); }

# The retired enrolment of the first person who signs in (D040). Split so this file does not match itself.
old_flow='setup[-_ ]?tok''en|claim-ad''min|claim (the )?(first )?ad''min|first-ad''min'

# Fixture values: shaped like real ones so resolution can be asserted; fake.
write_fixture() { # file [omit-key]
  local f=$1 omit=${2:-}
  cat > "$f" <<ENV
REQUESTARR_TAG=$pinned_tag
POSTGRES_PASSWORD=fixturepassword0123
REQUESTARR_PUBLIC_URL=https://requests.example.test
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
  json="$(resolve "$c" "$env")" || { echo "FAIL: $c does not resolve against the fixture environment" >&2; return 1; }
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
env = r.get("environment", {})
penv = p.get("environment", {})
if "REQUESTARR_DATABASE_URL" in env:
    errs.append("requestarr must get the database as separate settings, not REQUESTARR_DATABASE_URL")
for k, v in env.items():
    if "://" in str(v) and "@" in str(v):
        errs.append("%s looks like a URL carrying credentials" % k)
if env.get("REQUESTARR_DB_HOST") != "postgres" or "postgres" not in svc:
    errs.append("REQUESTARR_DB_HOST must be the postgres service name, got %r" % env.get("REQUESTARR_DB_HOST"))
if env.get("REQUESTARR_DB_PORT") != "5432":
    errs.append("REQUESTARR_DB_PORT must be 5432, got %r" % env.get("REQUESTARR_DB_PORT"))
if not env.get("REQUESTARR_DB_NAME") or env.get("REQUESTARR_DB_NAME") != penv.get("POSTGRES_DB"):
    errs.append("REQUESTARR_DB_NAME must be the database the postgres service creates")
if not env.get("REQUESTARR_DB_USER") or env.get("REQUESTARR_DB_USER") != penv.get("POSTGRES_USER"):
    errs.append("REQUESTARR_DB_USER must be the user the postgres service creates")
if not env.get("REQUESTARR_DB_PASSWORD") or env.get("REQUESTARR_DB_PASSWORD") != penv.get("POSTGRES_PASSWORD"):
    errs.append("REQUESTARR_DB_PASSWORD must be the postgres service password")
for k in ("REQUESTARR_OIDC_ISSUER", "REQUESTARR_OIDC_CLIENT_ID", "REQUESTARR_OIDC_CLIENT_SECRET", "REQUESTARR_ADMIN_PASSWORD", "REQUESTARR_TRUSTED_PROXIES"):
    if k not in env:
        errs.append("%s must be passed through to requestarr" % k)
    elif env[k] != "":
        errs.append("%s must be empty when the environment file does not set it, got a default" % k)
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

# A value set in the environment file must reach the container unchanged.
check_optional_passthrough() { # compose-file
  local c=$1 env="$work/optional.env" key out rc=0
  write_fixture "$env"
  for key in "${optional[@]}"; do echo "$key=fixture-$key" >> "$env"; done
  out="$(resolve "$c" "$env" | python3 -c 'import json,sys; e=json.load(sys.stdin)["services"]["requestarr"]["environment"]; print(" ".join(k+"="+e.get(k,"") for k in sys.argv[1:]))' "${optional[@]}")" || { echo "FAIL: $c does not resolve with the optional settings set" >&2; return 1; }
  for key in "${optional[@]}"; do
    case " $out " in *" $key=fixture-$key "*) ;; *) echo "FAIL: $c does not pass $key through" >&2; rc=1 ;; esac
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
  for key in "${optional[@]}"; do
    grep -Eq "^#? ?$key=" "$ex" || { echo "FAIL: $ex does not show $key (commented out)" >&2; rc=1; }
    v="$(sed -n "s/^$key=//p" "$ex" | head -n1)"
    [[ -z $v ]] || { echo "FAIL: $key in $ex must be unset or empty, not carry a value" >&2; rc=1; }
  done
  if grep -Eiq "$old_flow" "$ex"; then echo "FAIL: $ex mentions the removed claim flow" >&2; rc=1; fi
  if grep -Eq 'DATABASE_URL|postgres://' "$ex"; then echo "FAIL: $ex must not build or mention a database URL" >&2; rc=1; fi
  if grep -Eiq 'letters and digits|percent-encod' "$ex"; then echo "FAIL: $ex still restricts the database password to letters and digits" >&2; rc=1; fi
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
    COMPOSE_ENV="$(compose_env_names "$c")" REQUIRED="${required[*]}" OPTIONAL="${optional[*]}" DB_REQUIRED="${db_required[*]}" SECRETS="${secrets[*]}" CONFIG=internal/config/config.go \
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
check_optional_passthrough "$compose" || failures=$((failures + 1))
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
for key in "${optional[@]}"; do
  sed "s#^\( *$key:\).*#\1 \${$key:?set it}#" "$compose" > "$work/mut.yaml"
  expect_rejected "a compose file that requires $key" check_compose "$work/mut.yaml"
  sed "s#^\( *$key:\).*#\1 \${$key:-usable-default}#" "$compose" > "$work/mut.yaml"
  expect_rejected "a compose file with a default for $key" check_compose "$work/mut.yaml"
  grep -v "^ *$key:" "$compose" > "$work/mut.yaml"
  expect_rejected "a compose file that drops $key" check_compose "$work/mut.yaml"
  sed "s#^\( *$key:\).*#\1 fixed#" "$compose" > "$work/mut.yaml"
  expect_rejected "a compose file that ignores the environment for $key" check_optional_passthrough "$work/mut.yaml"
  { cat "$example"; echo "$key=usable-value"; } > "$work/short.env.example"
  expect_rejected "an example that sets $key" check_example "$work/short.env.example"
done
{ cat "$example"; echo '# Run setup-tok''en first.'; } > "$work/short.env.example"
expect_rejected "an example that mentions the removed claim flow" check_example "$work/short.env.example"
sed 's#^\( *REQUESTARR_ENV:\) production#\1 development#' "$compose" > "$work/env.yaml"
expect_rejected "a compose file running in development" check_compose "$work/env.yaml"
grep -v '^ *REQUESTARR_ENV:' "$compose" > "$work/env.yaml"
expect_rejected "a compose file without REQUESTARR_ENV" check_compose "$work/env.yaml"
mutate "$compose" "$work/floating.yaml" REQUESTARR_TAG latest
expect_rejected "a floating tag" check_tag_pinned "$work/floating.yaml"
mutate "$compose" "$work/floating.yaml" REQUESTARR_TAG '${REQUESTARR_TAG:-latest}'
expect_rejected "a defaulted tag" check_no_defaults "$work/floating.yaml"
sed 's#^\( *REQUESTARR_DB_PASSWORD:.*\)#\1\n      REQUESTARR_DATABASE_URL: postgres://u:p@postgres:5432/requestarr#' "$compose" > "$work/mut.yaml"
expect_rejected "a compose file that also sets a database URL" check_compose "$work/mut.yaml"
sed 's#^\( *REQUESTARR_DB_HOST:\).*#\1 elsewhere#' "$compose" > "$work/mut.yaml"
expect_rejected "a compose file with a wrong database host" check_compose "$work/mut.yaml"
sed 's#^\( *REQUESTARR_DB_USER:\).*#\1 other#' "$compose" > "$work/mut.yaml"
expect_rejected "a compose file with a wrong database user" check_compose "$work/mut.yaml"
sed 's#^\( *REQUESTARR_DB_PASSWORD:\).*#\1 hardcoded#' "$compose" > "$work/mut.yaml"
expect_rejected "a compose file with a different database password" check_compose "$work/mut.yaml"
grep -v '^ *REQUESTARR_DB_NAME:' "$compose" > "$work/mut.yaml"
expect_rejected "a compose file without a database name" check_compose "$work/mut.yaml"
{ cat "$example"; echo 'REQUESTARR_DATABASE_URL=postgres://u:p@postgres/requestarr'; } > "$work/short.env.example"
expect_rejected "an example that builds a database URL" check_example "$work/short.env.example"
{ cat "$example"; echo '# Use letters and digits only.'; } > "$work/short.env.example"
expect_rejected "an example that restricts the password" check_example "$work/short.env.example"
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
for key in REQUESTARR_SESSION_HMAC_SECRET REQUESTARR_SECRETS_KEY REQUESTARR_DB_PASSWORD; do
  mutate_template "unmasked $key" "/Target=\"$key\"/ s/Mask=\"true\"/Mask=\"false\"/"
  mutate_template "a default for $key" "/Target=\"$key\"/ s/Default=\"\"/Default=\"usable-default\"/"
  mutate_template "$key not required" "/Target=\"$key\"/ s/Required=\"true\"/Required=\"false\"/"
done
# The optional fields: never required, no default, advanced where they are overrides; the two
# secrets masked. Each is rejected when edited toward the old behaviour.
for key in REQUESTARR_OIDC_ISSUER REQUESTARR_OIDC_CLIENT_ID REQUESTARR_OIDC_CLIENT_SECRET REQUESTARR_ADMIN_PASSWORD REQUESTARR_TRUSTED_PROXIES; do
  mutate_template "$key required again" "/Target=\"$key\"/ s/Required=\"false\"/Required=\"true\"/"
  mutate_template "a default for $key" "/Target=\"$key\"/ s/Default=\"\"/Default=\"usable-default\"/"
  mutate_template "a value for $key" "/Target=\"$key\"/ s#Mask=\"(true|false)\"/>#Mask=\"\\1\">usable-value</Config>#"
done
for key in REQUESTARR_OIDC_ISSUER REQUESTARR_OIDC_CLIENT_ID REQUESTARR_OIDC_CLIENT_SECRET; do
  mutate_template "$key shown by default" "/Target=\"$key\"/ s/Display=\"advanced\"/Display=\"always\"/"
  mutate_template "$key that does not say the environment overrides Settings" "/Target=\"$key\"/ s/overrides anything saved in Settings, Authentication/mentions Settings/"
  mutate_template "$key that does not say all three come from the environment" "/Target=\"$key\"/ s/all three/some/"
done
mutate_template "trusted proxies shown by default" '/Target="REQUESTARR_TRUSTED_PROXIES"/ s/Display="advanced"/Display="always"/'
mutate_template "trusted proxies that do not warn about spoofing" '/Target="REQUESTARR_TRUSTED_PROXIES"/ s/can forge/can send/'
mutate_template "trusted proxies read by nothing" 's#Target="REQUESTARR_TRUSTED_PROXIES"#Target="REQUESTARR_TRUSTED_PROXY"#'
mutate_template "an unmasked OIDC client secret" '/Target="REQUESTARR_OIDC_CLIENT_SECRET"/ s/Mask="true"/Mask="false"/'
mutate_template "an unmasked initial administrator password" '/Target="REQUESTARR_ADMIN_PASSWORD"/ s/Mask="true"/Mask="false"/'
mutate_template "an initial password description without the forced change" '/Target="REQUESTARR_ADMIN_PASSWORD"/ s/forced to change/kept/'
mutate_template "an initial password description without the log" '/Target="REQUESTARR_ADMIN_PASSWORD"/ s/container log/somewhere/'
mutate_template "an initial password variable the code does not read" 's#Target="REQUESTARR_ADMIN_PASSWORD"#Target="REQUESTARR_ADMIN_PASSWORDS"#'
mutate_template "an OIDC variable the code does not read" 's#Target="REQUESTARR_OIDC_ISSUER"#Target="REQUESTARR_OIDC_ISSUERS"#'
mutate_template "a requirement that OIDC is installed first" 's#OIDC is optional#An OIDC provider is needed#'
mutate_template "the removed claim flow in the overview" 's#Requestarr is a shared#Requestarr uses a setup-tok''en and is a shared#'
# The database password and the URL override are the secrets; the override is the
# only database field that is optional, advanced and defaultless at once.
for key in REQUESTARR_DB_HOST REQUESTARR_DB_USER REQUESTARR_DB_NAME; do
  mutate_template "$key not required" "/Target=\"$key\"/ s/Required=\"true\"/Required=\"false\"/"
done
mutate_template "a default for the database host" '/Target="REQUESTARR_DB_HOST"/ s/Default=""/Default="postgres"/'
mutate_template "a default for the database user" '/Target="REQUESTARR_DB_USER"/ s/Default=""/Default="requestarr"/'
mutate_template "a different default port" '/Target="REQUESTARR_DB_PORT"/ s/Default="5432"/Default="5433"/'
mutate_template "a required port" '/Target="REQUESTARR_DB_PORT"/ s/Required="false"/Required="true"/'
mutate_template "a different default database name" '/Target="REQUESTARR_DB_NAME"/ s/Default="requestarr"/Default="other"/'
mutate_template "a required SSL mode" '/Target="REQUESTARR_DB_SSLMODE"/ s/Required="false"/Required="true"/'
mutate_template "a defaulted SSL mode" '/Target="REQUESTARR_DB_SSLMODE"/ s/Default=""/Default="disable"/'
mutate_template "an SSL mode shown by default" '/Target="REQUESTARR_DB_SSLMODE"/ s/Display="advanced"/Display="always"/'
mutate_template "a required database URL override" '/Target="REQUESTARR_DATABASE_URL"/ s/Required="false"/Required="true"/'
mutate_template "a defaulted database URL override" '/Target="REQUESTARR_DATABASE_URL"/ s#Default=""#Default="postgres://x"#'
mutate_template "an unmasked database URL override" '/Target="REQUESTARR_DATABASE_URL"/ s/Mask="true"/Mask="false"/'
mutate_template "a database URL override shown by default" '/Target="REQUESTARR_DATABASE_URL"/ s/Display="advanced"/Display="always"/'
mutate_template "an override that does not say it replaces the fields" '/Target="REQUESTARR_DATABASE_URL"/ s/replaces/mentions/'
mutate_template "a password description that restricts characters" '/Target="REQUESTARR_DB_PASSWORD"/ s/any characters/letters and digits/'
# A setting the template sets that the compose file does not.
grep -v '^ *REQUESTARR_DB_PORT:' "$compose" > "$work/mut.yaml"
expect_rejected "a template setting the compose file does not" check_template "$template" "$work/mut.yaml"
for key in REQUESTARR_PUBLIC_URL REQUESTARR_OIDC_ISSUER REQUESTARR_OIDC_CLIENT_ID REQUESTARR_OIDC_CLIENT_SECRET REQUESTARR_ADMIN_PASSWORD \
  REQUESTARR_SESSION_HMAC_SECRET REQUESTARR_SECRETS_KEY REQUESTARR_DB_HOST REQUESTARR_DB_NAME REQUESTARR_DB_USER REQUESTARR_DB_PASSWORD \
  REQUESTARR_DB_PORT REQUESTARR_DB_SSLMODE REQUESTARR_DATABASE_URL REQUESTARR_LOG_LEVEL \
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

# ---- Section: the retired enrolment flow is gone from documents and scripts ----
# Only a decision record (docs/DECISIONS.md) and .scratch/ may mention it, as history.
check_no_old_flow() { # root
  local root=$1 hits
  hits="$(cd "$root" && grep -rIlE "$old_flow" README.md .env.example docs deploy scripts prompts AGENTS.md CLAUDE.md GLOSSARY.md 2>/dev/null | grep -v '^docs/DECISIONS.md$' || true)"
  if [[ -n $hits ]]; then echo "FAIL: the removed claim flow is still mentioned in: $hits" >&2; return 1; fi
}
check_no_old_flow "$PWD" || failures=$((failures + 1))
mkdir -p "$work/tree/docs" "$work/tree/scripts"
echo 'Run setup-tok''en first.' > "$work/tree/docs/X.md"
expect_rejected "a guide that still mentions the removed claim flow" check_no_old_flow "$work/tree"

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
