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

Wizard state is resumable and idempotent. Secrets must be stored encrypted/secret-backed rather than returned to the browser after save.
