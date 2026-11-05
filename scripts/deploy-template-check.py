"""Checks deploy/unraid/requestarr.xml (section of scripts/deploy-files-test.sh).

Inputs come from the environment so the shell script stays the single place that
lists what an operator must supply. Prints one FAIL line per problem; exits 1
if any. Never prints a field's default value of a secret.
"""
import os
import re
import sys
import xml.etree.ElementTree as ET

errs = []


def err(msg):
    errs.append(msg)


try:
    root = ET.parse(os.environ["TEMPLATE"]).getroot()
except ET.ParseError as e:
    print("FAIL: template is not well-formed XML: %s" % e, file=sys.stderr)
    sys.exit(1)

if root.tag != "Container" or root.get("version") != "2":
    err('root element must be <Container version="2">')

for name in ("Name", "Repository", "Registry", "Network", "Overview", "Category", "Icon",
             "Project", "Support", "TemplateURL", "WebUI"):
    el = root.find(name)
    if el is None or not (el.text or "").strip():
        err("<%s> is missing or empty" % name)

# Network type is a visible, changeable choice; the tag is part of Repository.
configs = {}
for c in root.findall("Config"):
    target = c.get("Target", "")
    if target in configs:
        err("duplicate Config target %s" % target)
    configs[target] = c
    for attr in ("Name", "Target", "Type", "Display", "Required", "Mask"):
        if c.get(attr) is None:
            err("Config %s has no %s attribute" % (target or "?", attr))
    if c.get("Type") not in ("Variable", "Port"):
        err("Config %s has unsupported Type %r (this template needs no paths)" % (target, c.get("Type")))
    if len((c.get("Description") or "").strip()) < 40:
        err("Config %s needs a description that says what it is for" % target)

wanted = os.environ["REQUIRED"].split()
needed_targets = [w for w in wanted if w.startswith("REQUESTARR_") and w != "REQUESTARR_TAG"]
db_required = os.environ["DB_REQUIRED"].split()
for t in needed_targets + db_required + ["REQUESTARR_DB_PORT", "REQUESTARR_DB_SSLMODE", "REQUESTARR_DATABASE_URL",
                                         "REQUESTARR_LOG_LEVEL", "REQUESTARR_HTTP_ALLOWED_NETWORKS", "REQUESTARR_ENV", "8080"]:
    if t not in configs:
        err("template has no field for %s" % t)

# ---- environment: fixed to production so the https rules apply ----------------------
c = configs.get("REQUESTARR_ENV")
if c is not None:
    if c.get("Default") != "production" or (c.text or "").strip() != "production":
        err("REQUESTARR_ENV must default to production, with the value production")
    if c.get("Display") != "advanced" or c.get("Required") != "false":
        err("REQUESTARR_ENV must be advanced and not required to edit")
    if "production" not in (c.get("Description") or ""):
        err("REQUESTARR_ENV description must say why it is production")

# ---- secrets: masked, required, no default, with a generation command ----------
for name in ("REQUESTARR_OIDC_CLIENT_SECRET", "REQUESTARR_SESSION_HMAC_SECRET", "REQUESTARR_SECRETS_KEY"):
    c = configs.get(name)
    if c is None:
        continue
    if c.get("Mask") != "true" or c.get("Required") != "true":
        err("%s must be Mask=true and Required=true" % name)
    if c.get("Default") or (c.text or "").strip():
        err("%s must have no default or value" % name)
# The database is asked for as separate fields (D034). The password is masked, required and
# has no default; host and user are required with no default; the name defaults to requestarr.
c = configs.get("REQUESTARR_DB_PASSWORD")
if c is not None:
    if c.get("Mask") != "true" or c.get("Required") != "true":
        err("REQUESTARR_DB_PASSWORD must be Mask=true and Required=true")
    if c.get("Default") or (c.text or "").strip():
        err("REQUESTARR_DB_PASSWORD must have no default or value")
    if "any characters" not in (c.get("Description") or ""):
        err("REQUESTARR_DB_PASSWORD description must say any characters are fine")
for name in ("REQUESTARR_DB_HOST", "REQUESTARR_DB_USER"):
    c = configs.get(name)
    if c is not None and (c.get("Default") or (c.text or "").strip()):
        err("%s must have no default or value" % name)
c = configs.get("REQUESTARR_DB_HOST")
if c is not None and "user-defined" not in (c.get("Description") or ""):
    err("REQUESTARR_DB_HOST description must say a container name needs a user-defined network")
c = configs.get("REQUESTARR_DB_NAME")
if c is not None and (c.get("Default") != "requestarr" or (c.text or "").strip() != "requestarr"):
    err("REQUESTARR_DB_NAME must default to requestarr, with that value")
c = configs.get("REQUESTARR_DB_PORT")
if c is not None:
    if c.get("Default") != "5432" or (c.text or "").strip() != "5432" or c.get("Required") != "false":
        err("REQUESTARR_DB_PORT must be optional with default and value 5432")
c = configs.get("REQUESTARR_DB_SSLMODE")
if c is not None:
    if c.get("Display") != "advanced" or c.get("Required") != "false" or c.get("Default") or (c.text or "").strip():
        err("REQUESTARR_DB_SSLMODE must be advanced, optional and have no default")
c = configs.get("REQUESTARR_DATABASE_URL")
if c is not None:
    if c.get("Display") != "advanced" or c.get("Required") != "false":
        err("REQUESTARR_DATABASE_URL is an advanced override and must not be required")
    if c.get("Mask") != "true" or c.get("Default") or (c.text or "").strip():
        err("REQUESTARR_DATABASE_URL can carry a password: Mask=true and no default")
    if "replaces" not in (c.get("Description") or ""):
        err("REQUESTARR_DATABASE_URL description must say it replaces the database fields")
for name in ("REQUESTARR_SESSION_HMAC_SECRET", "REQUESTARR_SECRETS_KEY"):
    c = configs.get(name)
    if c is not None and "openssl rand -base64" not in (c.get("Description") or ""):
        err("%s description must give an openssl generation command" % name)
c = configs.get("REQUESTARR_SECRETS_KEY")
if c is not None and "separately from the database" not in (c.get("Description") or ""):
    err("master key description must say to back it up separately from the database")

# ---- required fields say so; optional ones are not Required ----------------------
for t, c in configs.items():
    d = c.get("Description") or ""
    want = c.get("Required") == "true"
    if want and not d.startswith("Required."):
        err("%s is required, so its description must start with 'Required.'" % t)
    if not want and not d.startswith("Optional."):
        err("%s is optional, so its description must start with 'Optional.'" % t)
for t in needed_targets + db_required + ["8080"]:
    if t in configs and configs[t].get("Required") != "true":
        err("%s must be Required=true" % t)
for t in ("REQUESTARR_LOG_LEVEL", "REQUESTARR_HTTP_ALLOWED_NETWORKS", "REQUESTARR_ENV", "REQUESTARR_DB_PORT",
          "REQUESTARR_DB_SSLMODE", "REQUESTARR_DATABASE_URL"):
    if t in configs and configs[t].get("Required") == "true":
        err("%s must be optional" % t)

# ---- tag: pinned while only pre-releases exist ------------------------------------
repo = (root.findtext("Repository") or "").strip()
if repo != "ghcr.io/evilperson1337/requestarr:" + os.environ["PINNED"]:
    err("Repository is %r, want the image pinned to %s" % (repo, os.environ["PINNED"]))
if ":" not in repo or repo.rsplit(":", 1)[-1] in ("latest", "main", "edge"):
    err("Repository must not use a floating or missing tag")
if root.findtext("Branch/Tag") not in (None, os.environ["PINNED"]):
    err("Branch/Tag must be the pinned tag")

# ---- environment names agree with the compose file and the configuration code -----
compose_env = set(os.environ["COMPOSE_ENV"].split())
template_env = {t for t, c in configs.items() if c.get("Type") == "Variable"}
for n in sorted(compose_env - template_env):
    err("compose sets %s but the template has no field for it" % n)
cfgdir = os.path.dirname(os.environ["CONFIG"])
known = set()
for f in os.listdir(cfgdir):
    if f.endswith(".go") and not f.endswith("_test.go"):
        known |= set(re.findall(r'"(REQUESTARR_[A-Z0-9_]+)"', open(os.path.join(cfgdir, f)).read()))
known.add("REQUESTARR_SECRETS_KEY")  # read through internal/secrets.MasterKeyEnv
for n in sorted(template_env - known):
    err("template sets %s, which the configuration code does not read" % n)
for n in sorted(template_env - compose_env - {"REQUESTARR_LOG_LEVEL", "REQUESTARR_HTTP_ALLOWED_NETWORKS", "REQUESTARR_DATABASE_URL"}):
    err("template sets %s, which the compose file does not" % n)

# ---- text and links ----------------------------------------------------------------
overview = root.findtext("Overview") or ""
for phrase in ("Settings", "PostgreSQL", "OIDC"):
    if phrase not in overview:
        err("Overview must mention %s" % phrase)
if len(overview) < 200:
    err("Overview is too short to explain Requestarr")
if "add another variable" not in (overview + (root.findtext("Requires") or "")).lower():
    err("template must point to Unraid's 'add another variable' for environment credentials")
if root.findtext("Project") != os.environ["PROJECT"]:
    err("Project must be %s" % os.environ["PROJECT"])
if root.findtext("TemplateURL") != os.environ["TEMPLATE_URL"]:
    err("TemplateURL must be %s" % os.environ["TEMPLATE_URL"])
for tag in ("Icon", "Project", "Support", "TemplateURL", "Registry"):
    v = root.findtext(tag) or ""
    if not v.startswith(("https://github.com/evilperson1337/", "https://raw.githubusercontent.com/evilperson1337/requestarr/main/")):
        err("<%s> must be a public evilperson1337 GitHub address, got %r" % (tag, v))
text = open(os.environ["TEMPLATE"]).read().lower()
if "allour" + "media" in text:  # split so this file does not itself carry the string
    err("template names the private domain")
if "mnt/user" in text or "/boot" in text or any(c.get("Type") == "Path" for c in root.findall("Config")):
    err("template must not need an appdata path")
for t in ("REQUESTARR_SETUP_TOKEN", "REQUESTARR_TMDB_READ_TOKEN", "REQUESTARR_PLEX_TOKEN", "REQUESTARR_JELLYFIN_API_KEY"):
    if t in configs:
        err("%s is a provider credential and must not be a template field" % t)

for e in errs:
    print("FAIL: template: " + e, file=sys.stderr)
sys.exit(1 if errs else 0)
