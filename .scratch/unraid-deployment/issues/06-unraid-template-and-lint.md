# 06: The Unraid template and its lint

**What to build:** A Community Applications-style Unraid template that installs Requestarr from a form of labelled fields (D034), installable through Unraid's Template Repositories URL, with CI failing if it is malformed or unsafe.

**Blocked by:** 01 (the optional extra-networks field uses its setting), 04 (extends its deployment-file check)

**Status:** ready-for-agent

- [ ] The template has fields for repository and tag, web port, network type (so a user-defined Docker network can be chosen), database URL, public URL, OIDC issuer, client ID and client secret, session secret and master key, plus optional log level and extra private networks
- [ ] The session secret and master key are required and masked with no default; each description gives a generation command that yields a value the server accepts (the master key is exactly 32 bytes as hex or base64; the session secret is at least 32 bytes), and the master key description says to back it up separately from the database
- [ ] Every field says what it is for and whether it is required; the template carries an icon, an overview and a project link, and needs no appdata path
- [ ] Library and provider credentials are not fields; the description points to Setup and to Unraid's "add another variable" for the environment alternative
- [ ] The image tag defaults to the pinned pre-release while only pre-releases exist
- [ ] The existing deployment-file check validates the template: well-formed XML, required fields present, secret fields masked and without defaults, tag not floating while only pre-releases exist (seam 3 of the spec); it runs in CI
- [ ] `scripts/check.sh` passes
