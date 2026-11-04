Status: ready-for-agent

# Unraid deployment

Source: the grilling session of 2026-11-04, `GLOSSARY.md`, and decisions D023, D033 and D034 in `docs/DECISIONS.md`. Read those first; this spec does not restate their reasoning.

## Problem Statement

Someone who runs Unraid cannot install Requestarr today. No container image is published, so there is nothing to pull. There is no Unraid template and no compose file for a home server, and nothing says what a self-hoster must bring (PostgreSQL, TLS, an OIDC provider). The server refuses any library URL that is not https, which rules out the normal way a home server reaches its own Plex or Jellyfin (a LAN address or a Docker container name). The distroless image has no way to report that it is healthy. And the repository is hosted on a private domain, so it cannot be published publicly without leaking that domain through the module path, documentation and every commit author.

## Solution

An Unraid user installs Requestarr from a Community Applications-style template, fills in labelled fields (database URL, public URL, OIDC settings, two generated secrets) and starts it. A compose file does the same for first testing and for the Compose Manager plugin. Both pull a published, signed image from the GitHub container registry. A short guide lists the three things the user must already have (PostgreSQL, a TLS reverse proxy, an HTTPS OIDC provider), shows a Keycloak setup, and walks through first-admin sign-in. Library Sources on a private host can use `http://`. The container reports healthy only when the app is ready. A scripted export produces a scrubbed copy of the repository for GitHub, and refuses to publish if any trace of the private domain survives.

## User Stories

### Installing
1. As an Unraid user, I want to install Requestarr from a template with labelled fields, so that I do not have to write Docker arguments by hand.
2. As an Unraid user, I want each template field to say what it is for and whether it is required, so that I can fill the form without reading the source.
3. As an Unraid user, I want the template to pull a published image, so that installation does not need a build on my server.
4. As an Unraid user, I want to add the template through Unraid's Template Repositories setting, so that I can install it before it is listed in Community Applications.
5. As an Unraid user, I want the template to default to a pinned release tag while only pre-releases exist, so that installing never fails on a tag that does not exist yet.
6. As an Unraid user, I want the template to move to the stable `latest` tag once a stable release ships, so that updates work the Unraid way.
7. As an Unraid user, I want the template to show an icon and a link to the project page, so that I can recognise it in the Docker tab.
8. As an Unraid user, I want to choose the container's network (including a user-defined Docker network), so that I can reach other containers by name.
9. As an Unraid user, I want the web port to be a visible, changeable field, so that it does not clash with another container.
10. As an Unraid user, I want the container to need no appdata folder, so that there is nothing to back up on the flash drive or array for the app itself.

### Prerequisites and secrets
11. As an Unraid user, I want the guide to list what I must bring before installing (PostgreSQL, a TLS reverse proxy, an OIDC provider), so that I know up front what I am signing up for.
12. As an Unraid user, I want the guide to show how to create the database and user in the PostgreSQL container I already run, so that I can build the database URL correctly.
13. As an Unraid user, I want the session secret and the master key to be required, masked fields with no default, so that I never run with a shared, well-known secret.
14. As an Unraid user, I want each secret field to give the command that generates a valid value, so that I do not have to know the accepted formats.
15. As an Unraid user, I want a value that the master key field cannot accept to be reported clearly at startup, so that I can fix it without guessing.
16. As an Unraid user, I want the guide to say that the master key must be backed up separately from the database, so that I do not lose my stored integration secrets on a restore.
17. As an Unraid user, I want the guide to explain that the public URL and the identity provider must be https, so that I understand why a plain-http setup does not start.

### Signing in and first admin
18. As an Unraid user, I want a worked Keycloak example (client type, redirect URI, scopes), so that I can set up sign-in without trial and error.
19. As an Unraid user running another OIDC provider, I want the generic requirements stated (issuer, client ID and secret, exact redirect URI), so that I can adapt them to my provider.
20. As an Unraid user, I want the guide to show how to get the one-time first-admin token from the running container, so that I can claim the admin account after signing in.
21. As an Unraid user, I want a clear note that new people sign in as viewers and the admin promotes them, so that I am not surprised by who can request.

### Library Sources
22. As an Unraid user, I want to point Requestarr at Plex, Jellyfin, Navidrome, RomM or Audiobookshelf by a LAN address, so that I do not have to put every library behind HTTPS first.
23. As an Unraid user, I want to point a Source at a Docker container name on a user-defined network, so that traffic stays on my server.
24. As an Unraid user, I want an address on a public DNS name to still require https, so that a library token never crosses the internet in cleartext.
25. As an Unraid user, I want to add my own network ranges and name suffixes to the allowed list through one setting, so that an unusual private network (for example a VPN range) works.
26. As an Unraid user, I want a Source address that is rejected to say why and what would make it acceptable, so that I can correct it.
27. As an Unraid user, I want the same rule to apply whether I set the address in the environment or save it in Setup, so that the two never disagree.
28. As an Unraid user, I want the guide to say that an `http://` library connection sends its token in cleartext on my network, so that I make that choice knowingly.
29. As an administrator, I want the public URL, the sign-in issuer and the notification gateway address to stay https-only, so that relaxing the library rule does not weaken sign-in.

### Health and operation
30. As an Unraid user, I want the Docker tab to show the container as healthy only when Requestarr is ready, so that a container that started but cannot reach its database is not shown as fine.
31. As a compose user, I want other services to wait for Requestarr to be healthy, so that start-up order is correct.
32. As an Unraid user, I want to see the migration and readiness messages in the container log, so that I can diagnose a failed start.
33. As an Unraid user, I want the upgrade guide to apply to the template (back up, change the tag, restart), so that updates are safe.

### Trying it first
34. As a tester, I want a compose file with Requestarr and PostgreSQL only, so that I can try it before using the template.
35. As a tester, I want an example environment file listing every required setting with placeholders and no real secrets, so that I know what to fill in.
36. As a tester, I want the compose file pinned to an exact pre-release tag, so that I test the artifact that was actually published.
37. As a tester, I want to run the same first-admin steps as in the template guide, so that the two paths behave the same.

### Publishing
38. As a maintainer, I want a tagged release to publish a multi-architecture image to the GitHub container registry, so that Unraid can pull it.
39. As a maintainer, I want each release image to carry provenance, an SBOM and a signature, so that users can verify it (D023).
40. As a maintainer, I want only stable tags to move `latest`, so that a pre-release never becomes the default install.
41. As a maintainer, I want to cut a pre-release first and test it on a real Unraid server, so that the stable release is not the first time the whole pipeline runs.
42. As a maintainer, I want the published image to apply migrations on start and run as a non-root user, so that it matches the documented upgrade path.
43. As a maintainer, I want the build to fail if the template or compose file is malformed or lets a secret have a default, so that a broken template is never published.

### The public copy
44. As a maintainer, I want one command that turns the private repository into a scrubbed public one, so that I never hand-edit a mirror.
45. As a maintainer, I want the export to change the Go module path to the public one in every import, so that the public copy builds on its own.
46. As a maintainer, I want every other mention of the private domain replaced, so that no document, comment or link points at it.
47. As a maintainer, I want every commit author and committer mapped to a public noreply identity, so that no private email appears in the history.
48. As a maintainer, I want the export to keep the full commit history, so that contributors see how the project evolved.
49. As a maintainer, I want the export to be deterministic, so that republishing produces the same commit hashes and pushes only what is new.
50. As a maintainer, I want the export to refuse to publish if the private domain still appears in any ref, commit message, author, filename or file content, so that a missed string is caught before it is public.
51. As a maintainer, I want the export to fall back, on request, to one snapshot commit per release, so that an unscrubbable history never blocks a release.
52. As a maintainer, I want the export to leave the private repository untouched, so that rewriting history never affects the source of truth.
53. As a maintainer, I want to run the export by hand with a release tag, so that I control when something becomes public.
54. As a maintainer, I want a guided script for the steps only I can do (create the public repository, add the push credential, push the first tag), so that I do not have to remember them.
55. As a contributor to the public copy, I want the setup guide link inside the app to point at the public repository, so that it is not a dead link.

### Done
56. As a maintainer, I want a manual checklist for the real-server test (first-admin sign-in, a healthy container, a library syncing over a private `http://` address, a template install from the repository URL), so that "done" is observable and repeatable.

## Implementation Decisions

- **Library URL rule (D033).** One validator decides whether a Source URL is acceptable, and both the environment path and the Setup "apply" path go through it. Outside development it requires https unless the host is a **private host** (see `GLOSSARY.md`): a loopback, RFC 1918, link-local or CGNAT IP literal, a single-label name, or a name ending `.local`, `.lan`, `.internal` or `.home.arpa`. A new environment setting holds a comma-separated list of CIDRs and hostname suffixes and only adds to the built-ins; a malformed entry is a startup problem that names the entry. The validator is syntactic and never resolves names. Rejection messages say what would make the address acceptable.
- **What stays https-only.** The public URL, the OIDC issuer and the Apprise URL are unchanged.
- **Stored-connection validation.** The connection model already accepts both schemes; the https-or-private-host decision stays in the configuration layer that is applied when a Source is enabled, so there is still one rule.
- **Health probe.** The binary gains a `healthcheck` subcommand next to the existing `setup-token` one. It performs one bounded HTTP GET of the readiness endpoint on the configured listener address (mapping a wildcard address to loopback), exits 0 on a ready response and non-zero otherwise, prints nothing secret, and needs no database or OIDC configuration of its own. The Dockerfile declares it as the image health check; compose uses it for `depends_on` conditions.
- **Image.** The existing multi-stage build is kept: distroless, non-root, applies migrations at start. It is published to the GitHub container registry for amd64 and arm64 by the tag-triggered release workflow, with the D023 pieces added (SBOM, provenance attestation, keyless signature). `latest` tracks stable tags only. The container needs no volume.
- **Compose file.** Requestarr and PostgreSQL only, with the database on a named volume, Requestarr waiting for the database and itself reporting health, and every operator-supplied value read from an environment file. An example environment file lists each setting with a placeholder and no usable secret. The compose image tag is pinned to an exact pre-release while only pre-releases exist.
- **Template.** One Unraid template with: repository and tag, web port, network type, and fields for the database URL, public URL, OIDC issuer, client ID and client secret, session secret and master key (the last three masked and required, with no default and a generation command in the description), plus optional fields for log level and the extra private networks. Library and provider credentials are not template fields; they are entered in Setup after sign-in (D024), with their environment variables available through Unraid's "add another variable" for people who prefer them. The master key description gives a command that yields 32 random bytes in a format the server accepts; the session secret description gives one that yields at least 32 bytes.
- **Guide.** One document covers prerequisites, creating the database in an existing PostgreSQL container, building the database URL, reverse-proxy requirements, a Keycloak client example (confidential client, exact redirect URI, standard scopes), generic OIDC requirements, first-admin claim from the running container, the cleartext-token note for `http://` libraries, and links to the upgrade and backup guide. It states that PostgreSQL, TLS and the identity provider are not bundled.
- **Public export.** A single script takes a release tag and a destination, works on a fresh clone, rewrites history with a history-filter tool using a replace-text rule set (the module path first, then every other private-domain string, then author and committer identities), and never touches the source repository. A gate then scans every ref, every commit message and identity, every path and every blob (including binary files) for the private domain, case-insensitively, and the script exits non-zero without pushing if anything is found. Pushing is a separate, explicit step. A flag selects the snapshot-per-release fallback. The rule set lives in the repository, so changing it is reviewed.
- **Content fixed up in the source tree.** The in-app setup guide link currently points at a wrong GitHub address and is corrected to the public repository. Documentation that names the private domain keeps doing so in the private repository; the export rewrites it.
- **Decision records.** D033 and D034 already record the library rule and the deployment plan; the release-readiness note in the release documentation is updated when the workflow lands.
- **Release sequence.** First tag is a pre-release; it is tested on a real Unraid server with the compose file pinned to it; then a stable tag; then the template default moves to `latest`.

## Testing Decisions

- **What a good test is here.** It exercises behaviour a user or maintainer would see (an address is accepted or rejected, a probe exits 0 or not, a template is valid or not, an export is clean or refused), not how the code gets there.
- **Seam 1: the library URL rule.** Table-driven tests on the configuration validation that already gates a Source URL. Cases: each private-host form accepted over `http://`; a public DNS name rejected over `http://` and accepted over `https://`; the extension setting adding a CIDR and a suffix; a malformed extension entry reported by name; the public URL, issuer and Apprise URL still rejecting `http://`; development behaviour unchanged. Prior art: the existing configuration and secrets-key validation tests, and the connection validation tests.
- **Seam 2: the `healthcheck` subcommand.** Run through the program's entry point against a local test server: ready response exits 0; not-ready response, a refused connection and a timeout exit non-zero; no secret appears in output. Prior art: the entry-point tests for the existing `setup-token` subcommand.
- **Seam 3: deployment files.** One script-level check in the style of the existing `stack-*-test.sh` scripts: the compose file resolves under `docker compose config` with a fixture environment file and without starting containers; the template is well-formed XML; every required field is present; the secret fields are masked and have no default; the pinned tag is not a floating one while only pre-releases exist. It runs in the web-and-docs part of CI, so a broken template fails the build.
- **Seam 4: the public export.** One script-level test on a small fixture repository seeded with the private domain in a file body, a file name, a commit message, an author and a binary file. The export must come out with the module path rewritten, no private strings anywhere, and the same hashes on a second run; and the gate must fail (non-zero, nothing pushed) when a string is deliberately left behind. The test needs the history-filter tool and skips with a clear message if it is absent.
- **Not automated.** The real Unraid test (sign-in through Keycloak, healthy container, a library over `http://`, installing from the repository URL) is a manual checklist added to the manual testing guide. The registry publish, signing and attestation cannot be verified until a tag is pushed to GitHub.
- **Existing checks stay green.** The full local check (format, tests, vet, integration, web checks) continues to pass; the contract test that ties the API description to the routes is unaffected because no API changes.

## Out of Scope

- A Community Applications listing, its separate templates repository and the Unraid forum support thread. A follow-up after the first stable release and a real-server test.
- Bundling PostgreSQL, Keycloak or a TLS proxy in the template.
- An `http://` mode for the public URL, the OIDC issuer or the Apprise URL.
- Entering library or provider credentials as template fields.
- Other platforms (TrueNAS, Synology, Kubernetes, Helm), and any change to the product's behaviour beyond the library URL rule and the health probe.
- Automating the export in CI (a later step once a runner and a GitHub credential exist).
- Key rotation for the master key.
- Closing the other release-readiness gaps listed in the project status document; this spec only needs enough of the release workflow to publish an image.

## Further Notes

- Creating the public GitHub repository, adding the push credential and pushing the first tag are the maintainer's steps, handled by the guided script in story 54.
- The release workflow is written for GitHub Actions and has never run; expect to iterate on it against a pre-release tag. Provenance and signing can only be confirmed on GitHub.
- The public module path is lower-case (`github.com/evilperson1337/requestarr`) because Go module paths are case-sensitive in tooling, while the repository URL the user gave uses a capital.
- Unraid's default bridge network gives no name resolution between containers; a user-defined network does. The guide must say this wherever it shows a container-name address.
- The master key and session secret are different things with different accepted formats (the master key is exactly 32 bytes as hex or base64); the guide and field text must not conflate them.
