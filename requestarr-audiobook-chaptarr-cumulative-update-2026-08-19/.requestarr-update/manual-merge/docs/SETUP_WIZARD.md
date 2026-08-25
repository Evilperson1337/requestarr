# Setup Wizard

## Security model

Before setup completes, normal OIDC login may not yet work. The recommended bootstrap is an explicit high-entropy `REQUESTARR_SETUP_TOKEN` supplied out-of-band in the deployment environment. Setup endpoints require that token and issue only a short-lived setup session. Once setup is finalized, the bootstrap token is rejected until an explicit administrative reset/recovery action is performed.

Do not expose an unauthenticated first-run wizard to the network.

## Steps

1. **Welcome** — explain scope and what will be configured.
2. **General** — instance name, public URL, timezone.
3. **Authentication** — OIDC issuer/client/scopes/groups claim/group-role mapping, discovery validation, test login.
4. **Metadata Providers** — enable media types, configure/test TMDB, IGDB, MusicBrainz contact identity, and audiobook/Audnexus settings needed for Audible URL resolution.
5. **Libraries** — add zero or more Plex/Jellyfin/Navidrome/RomM/Audiobookshelf sources and select authoritative media types. Audiobookshelf is the V1 audiobook authority. Warn but allow no libraries.
6. **Notifications** — in-app always; optional Apprise API configuration/test notification.
7. **Request Policy** — approval requirement per media type (including audiobook), notes, subscription behavior defaults.
8. **Review** — concise summary with validation state.
9. **Finish** — transactional commit, setup_complete=true, invalidate bootstrap session, prompt OIDC sign-in.

Wizard state is resumable and idempotent. `GET`/`HEAD /api/v1/setup/state` requires a valid temporary setup session and returns only the current step plus typed non-secret drafts; HTTP `HEAD` returns the same status and headers with no body. `PUT /api/v1/setup/general` currently persists the normalized instance name, origin-only public URL, and IANA timezone, then advances monotonically to Authentication. Mutations require exactly one configured Origin header and exactly `X-Requestarr-CSRF: 1`; repeated and comma-joined values are rejected. Reads do not require Origin or CSRF. `OPTIONS` and all unsupported methods return `405` with the resource's exact `Allow` header; no setup route enables CORS or preflight.

General string limits are user-facing Unicode code-point limits with explicit UTF-8 byte ceilings: instance name is 1-100 code points and at most 400 bytes, public URL is 1-2048 code points and at most 8192 bytes, and timezone is 1-128 code points and at most 512 bytes. Values are trimmed and must be valid UTF-8. Public URL accepts only an absolute HTTP(S) origin with a host and no userinfo, non-root path, query, fragment, or opaque form. Timezone must be a loadable IANA name other than `Local`. The byte ceilings preserve compatibility for existing valid UTF-8 drafts accepted under the earlier byte-count interpretation while bounding transport and persistence work.

Mutation processing precedence is method, Origin, CSRF, exact content type and strict bounded JSON shape, typed General validation, then cookie/session authorization and persistence. Production accepts only one canonical `__Host-requestarr_setup` cookie and explicit loopback HTTP development accepts only one canonical `requestarr_setup_dev` cookie; duplicate, malformed, wrong-environment, and simultaneous names are rejected. Session expiry retains its absolute 15-minute lifetime and is evaluated using authoritative PostgreSQL wall time after the setup-state row lock is acquired.

Each effective General state mutation atomically appends a narrow `general_saved` workflow event containing only its generated identifier, event type, and database timestamp. Repeating an identical normalized save after the workflow has already advanced is a no-op and emits no duplicate event. Event rows never contain General values, session material or digests, cookies, headers, or request bodies.

Secrets must be stored encrypted/secret-backed rather than returned to the browser after save. Raw bootstrap/session material never enters workflow persistence; PostgreSQL receives only a keyed session digest.

The Authentication draft locally normalizes issuer, client ID, required client type (`public` or `confidential`), scopes (which must include `openid`), groups claim, default role, and ordered exact group-role mappings. Client secret writes are encrypted immediately under the fixed setup Authentication context, and reads expose presence only. Public clients must explicitly clear any envelope; confidential clients replace a secret or preserve one only when already configured and cannot clear it. Every effective save increments `authentication_revision` and invalidates prior discovery evidence; a true no-op preserves both.

`POST /api/v1/setup/authentication/validate` accepts only the current positive `authentication_revision`. It reserves durable per-session/global five-minute rate limits in a short transaction, performs one hardened secret-free discovery request without a transaction, and commits evidence only after rechecking completion, session, revision, and the Authentication snapshot in another short transaction. Late results are stale. Protected state reads expose only current-revision/current-policy evidence from PostgreSQL and never perform discovery or decrypt the client secret.

Discovery derives the standard well-known URL from the saved issuer, disables redirects and proxies, validates the issuer's complete DNS answer set, and pins dialing to checked addresses while preserving TLS SNI. Production requires HTTPS; development permits loopback HTTP. Private/internal issuer destinations require exact canonical CIDRs in `REQUESTARR_OIDC_DESTINATION_CIDRS`; there is no allow-private switch. Metadata must exactly match the issuer and advertise structurally safe authorization/token/JWKS endpoints, authorization code flow, public or pairwise subjects, RS256, S256, and an explicit client-type-appropriate `none` or `client_secret_basic` token authentication capability. Discovery does not resolve or call advertised endpoints, including private HTTPS literals and mixed/private names; every later endpoint use must freshly resolve all answers, apply the same special-use deny policy and CIDR allowlist, and pin dialing while retaining hostname verification and SNI. Validation evidence is capability/fingerprint evidence, not durable network trust. This validation does not test credentials, exchange tokens, or complete setup.

`REQUESTARR_ENCRYPTION_KEY` is separate from the temporary setup token and is required before final activation can persist any secret. Draft steps may record non-secret fields and secret presence/validation state, but never plaintext secret values. Final activation encrypts accepted secrets into versioned envelopes transactionally. An installation with no encrypted rows can remain locked for secret operations; once rows exist, a missing, malformed, or wrong key prevents startup. V1 supports one key and no online rotation, so the key and PostgreSQL backup must be retained and restored together.
