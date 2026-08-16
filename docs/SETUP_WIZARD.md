# Setup Wizard

## Security model

Before setup completes, normal OIDC login may not yet work. The recommended bootstrap is an explicit high-entropy `REQUESTARR_SETUP_TOKEN` supplied out-of-band in the deployment environment. Setup endpoints require that token and issue only a short-lived setup session. Once setup is finalized, the bootstrap token is rejected until an explicit administrative reset/recovery action is performed.

Do not expose an unauthenticated first-run wizard to the network.

## Steps

1. **Welcome** — explain scope and what will be configured.
2. **General** — instance name, public URL, timezone.
3. **Authentication** — OIDC issuer/client/scopes/groups claim/group-role mapping, discovery validation, test login.
4. **Metadata Providers** — enable media types, configure/test TMDB, IGDB, MusicBrainz contact identity.
5. **Libraries** — add zero or more Plex/Jellyfin/Navidrome/RomM sources and select authoritative media types. Warn but allow no libraries.
6. **Notifications** — in-app always; optional Apprise API configuration/test notification.
7. **Request Policy** — approval requirement per media type, notes, subscription behavior defaults.
8. **Review** — concise summary with validation state.
9. **Finish** — transactional commit, setup_complete=true, invalidate bootstrap session, prompt OIDC sign-in.

Wizard state is resumable and idempotent. `GET`/`HEAD /api/v1/setup/state` requires a valid temporary setup session and returns only the current step plus typed non-secret drafts; HTTP `HEAD` returns the same status and headers with no body. `PUT /api/v1/setup/general` currently persists the normalized instance name, origin-only public URL, and IANA timezone, then advances monotonically to Authentication. Mutations require exactly one configured Origin header and exactly `X-Requestarr-CSRF: 1`; repeated and comma-joined values are rejected. Reads do not require Origin or CSRF. `OPTIONS` and all unsupported methods return `405` with the resource's exact `Allow` header; no setup route enables CORS or preflight.

General string limits are user-facing Unicode code-point limits with explicit UTF-8 byte ceilings: instance name is 1-100 code points and at most 400 bytes, public URL is 1-2048 code points and at most 8192 bytes, and timezone is 1-128 code points and at most 512 bytes. Values are trimmed and must be valid UTF-8. Public URL accepts only an absolute HTTP(S) origin with a host and no userinfo, non-root path, query, fragment, or opaque form. Timezone must be a loadable IANA name other than `Local`. The byte ceilings preserve compatibility for existing valid UTF-8 drafts accepted under the earlier byte-count interpretation while bounding transport and persistence work.

Mutation processing precedence is method, Origin, CSRF, exact content type and strict bounded JSON shape, typed General validation, then cookie/session authorization and persistence. Production accepts only one canonical `__Host-requestarr_setup` cookie and explicit loopback HTTP development accepts only one canonical `requestarr_setup_dev` cookie; duplicate, malformed, wrong-environment, and simultaneous names are rejected. Session expiry retains its absolute 15-minute lifetime and is evaluated using authoritative PostgreSQL wall time after the setup-state row lock is acquired.

Each effective General state mutation atomically appends a narrow `general_saved` workflow event containing only its generated identifier, event type, and database timestamp. Repeating an identical normalized save after the workflow has already advanced is a no-op and emits no duplicate event. Event rows never contain General values, session material or digests, cookies, headers, or request bodies.

Secrets must be stored encrypted/secret-backed rather than returned to the browser after save. Raw bootstrap/session material never enters workflow persistence; PostgreSQL receives only a keyed session digest.
