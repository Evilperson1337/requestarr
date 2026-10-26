# First-run setup

## Bootstrap and the inactive configuration draft

First-admin bootstrap remains an OIDC sign-in plus one-time claim. It reads
public `GET /api/v1/setup/status`, which exposes only bootstrap completion, not
the configuration draft or identities. When bootstrap is required, the page
offers OIDC sign-in and collects the exact issuer/subject and one-time token.
The server checks the verified session identity, CSRF, and CLI-bound token.
Successful claims now open the admin-only **Setup** draft at `/setup`.

Existing admins can open Setup from navigation. The wizard handles
**General → Authentication → Metadata Providers → Libraries → Notifications →
Request Policy → Review**. The Libraries
step offers one fixed card for each supported adapter kind (Jellyfin, Plex,
RomM, Navidrome). Draft settings are **inactive**: saving or passing a check never
changes runtime configuration or hot-swaps OIDC. Only **Finish** on the Review
step marks the wizard complete.

- **Notifications:** enable the global Apprise route and record its API URL. The
  configuration key is the fixed server reference `REQUESTARR_APPRISE_CONFIG_KEY`.
  The check probes `/status` of the exact `REQUESTARR_APPRISE_URL` only; a
  different draft URL reports an environment item instead of being probed.
  In-app notifications are always on.
- **Request Policy:** choose which media types skip administrator approval.
  Empty means every request awaits approval. Like the rest of the draft, this is
  recorded but inactive until the server applies it.
- **Review:** shows the whole draft, the environment items still outstanding, and
  any required check that is not checked, failed or expired for the saved
  revision (General, Authentication, and every enabled provider, library and
  Apprise). **Run required checks** re-runs them.
- **Finish:** succeeds only with no outstanding checks, for the exact reviewed
  revision (`POST /api/v1/admin/setup/finish`), and is audited. Afterwards the
  draft is read-only (`setup_already_complete`), the Setup navigation link is
  hidden, and `/setup` shows a completion notice. Public `/setup/status` reports
  this as `wizardComplete`, separate from first-admin bootstrap `complete`.
  Finish adds no local-password path; sign-in stays OIDC-only.

The page does not receive identity fields or the setup token from the backend. It does not save the token to browser storage. The token field is cleared as soon as submission begins and after the request finishes. Failed claims use a generic message.

## Operator steps

1. Configure OIDC in the Requestarr server environment before first sign-in. At minimum set `REQUESTARR_OIDC_ISSUER`, `REQUESTARR_OIDC_CLIENT_ID`, and `REQUESTARR_OIDC_CLIENT_SECRET`; also set the public base URL and callback-related settings required by the deployment/provider. Follow [OIDC authentication](OIDC_AUTH.md) and the variable names documented in [`.env.example`](../.env.example). Never expose client secrets in the browser or commit them. Newly signed-in users are viewers who can browse and use Notify Me; administrators must promote users to requester to grant request-creation permission.
2. Restart Requestarr after changing environment configuration. Register the deployed callback URL with the OIDC provider as described in the OIDC guide.
3. Determine the exact issuer URL and subject (`sub`) of the identity that should become the first administrator. These are operator-provided values; the UI does not discover them.
4. On the server run `requestarr setup-token --issuer '<exact issuer>' --subject '<exact subject>'`. The CLI writes the high-entropy token to a protected file by default. Keep it private and provide it to the intended administrator through a secure channel. The token is bound to those exact identity values.
5. Open the Requestarr setup page, sign in with that OIDC identity, and enter the same exact issuer, subject, and setup token. The claim endpoint requires an authenticated session and CSRF protection.
6. On successful claim, the first administrator opens Setup. Public setup status
   still reports bootstrap completion using its existing semantics; this is not
   a claim that the configuration draft was activated or the full wizard finished.
   Later sessions can revisit `/setup` and resume the saved step.

## Saving and resuming

The shared PostgreSQL draft stores instance name, IANA timezone, public URL,
OIDC issuer/client ID, provider enablement/contact/client ID, library Source
URLs, the Navidrome username, enabled flags and supported authoritative media
types, and fixed secret environment-variable names. The Navidrome username is
plain draft data (not a secret); when the draft's username is empty it is seeded
from `REQUESTARR_NAVIDROME_USERNAME`, and the old `usernameEnv` reference in
earlier drafts is ignored. The draft never holds secret values or runtime secret
presence flags.

Secret values are write-only fields that are not part of the draft (D024). The
Notifications, Metadata Providers and Libraries steps show one such field for
each stored-capable secret. A saved value is stored in PostgreSQL encrypted under
the master key `REQUESTARR_SECRETS_KEY` (32 bytes as 64 hex characters, or base64
that decodes to 32 bytes; for example `openssl rand -hex 32`). Without that key
the fields are disabled and the environment path keeps working. Rules:

- An environment value always wins over a saved one. Each secret reports its
  source: `environment`, `stored`, or `stored (shadowed by environment)`; Review
  warns about shadowed values and never deletes them automatically.
- A saved secret applies on restart, like the rest of the inactive draft; Review
  lists it as "saved, pending restart". A connection check uses the value the
  next restart would use and names its source.
- Values are never returned, logged or audited; only presence and source are
  shown. Replacing or clearing a secret stays available after **Finish**.
- If a stored secret cannot be read (wrong or missing master key) the server
  still boots, the integration stays off and reports "stored secret unreadable".
  For a library Source or Apprise the URL is ignored for that boot (and for
  Navidrome the username as well) so the integration stays off as a unit. Restore
  the key, or re-enter or clear the secret. Clearing works without a master key,
  so an unreadable secret can always be discarded.
- Changing the master key makes existing stored secrets unreadable; re-enter
  them. Key rotation tooling is deferred.
- Back up the master key separately from the database (see
  [upgrade and backup](UPGRADE_AND_BACKUP.md)).

Stored-capable secrets (the registry in `internal/secrets`), with the
environment variable that overrides each:

- `REQUESTARR_TMDB_READ_TOKEN`
- `REQUESTARR_IGDB_CLIENT_SECRET`
- `REQUESTARR_APPRISE_CONFIG_KEY`
- `REQUESTARR_NAVIDROME_PASSWORD`
- `REQUESTARR_JELLYFIN_API_KEY`
- `REQUESTARR_PLEX_TOKEN`
- `REQUESTARR_ROMM_TOKEN`

`REQUESTARR_OIDC_CLIENT_SECRET`, `REQUESTARR_SESSION_HMAC_SECRET` and
`REQUESTARR_SECRETS_KEY` stay environment-only: sign-in must work before Setup is
reachable. Library and Apprise URLs also stay environment-only.

Use **Save draft** or the step navigation controls to persist edits and the
resume position. Saves use optimistic revisions; a concurrent change returns a
conflict without overwriting it. The UI preserves unsaved fields until you
choose **Reload saved draft**. Any successful save clears prior connection
results, since those results belong to the previous revision. Connection
failures do not prevent saving or erase the draft. General/provider fields may
remain partially filled while drafting, but checks require their target fields.

## Explicit connection checks

Routine draft loading never contacts a provider. Check actions first save the
form and then check that exact revision. Results expire after ten minutes; a
concurrent save prevents a stale result from being recorded. Each result has a
fixed safe message, never a raw service error or credential-bearing payload.

- **General:** validates name/timezone/URL and probes the configured public
  `/healthz`. A changed public URL needs an environment edit and restart before
  that round-trip can be verified.
- **Authentication:** fetches OIDC discovery metadata and validates its issuer
  and required endpoints. Discovery is **not interactive login proof** and
  does not verify arbitrary new client credentials. Issuer/client changes and
  missing server credentials remain environment/restart items. No secret is sent
  to a draft issuer.
- **TMDB:** checks the fixed configuration API with the token the next restart
  would use (environment, otherwise saved).
- **MusicBrainz:** checks a fixed public endpoint with an identifying user-agent
  and the draft's contact email; no secret is required.
- **IGDB:** checks fixed Twitch client-credentials and IGDB platform endpoints
  using the configured client ID and server-side secret.
- **Libraries:** each enabled Source has an explicit, bounded connection check
  using server-side credentials. A passed check proves connectivity only; it is
  not an inventory sync. Selected media types are limited to adapter capabilities
  and define that Source's intended authority in this inactive draft.

Checks have context cancellation, a ten-second deadline, a 1 MiB JSON response
limit, and disabled redirects. Arbitrary private/link-local/loopback destinations
are blocked at a DNS-validated, pinned dial boundary. The exact configured OIDC
issuer and configured public origin are operator-trusted exceptions for internal
deployments. Each library probe is an explicit administrator action against its
validated operator-entered origin; redirects are disabled and DNS is pinned for
that probe. Changing another private candidate does not gain trust.
An explicit check can fail behind deployment firewalls/proxies even when local
configuration is syntactically valid; it never changes known library availability.

## Environment and restart panel

The draft response computes an admin-only panel of environment-variable names,
configured-presence flags, required restart flags, and desired **non-secret**
values. Secret rows never return values. Configured means present, not proven
valid; failed credential checks may still require replacing the server secret
and restarting. The wizard cannot edit environment files for you. Its draft
remains inactive even after an environment restart.

## Security and scope

Do not expose setup-token output or the token file to untrusted users. Prefer limiting network access to the instance during bootstrap. The UI intentionally does not echo server error bodies on claim failure. The backend never returns the token or verified issuer/subject as setup status.

The draft does not activate settings, edit environment variables, or provide a
temporary/local-password sign-in. OIDC must work before the claim UI is usable.
Changing the active issuer still requires operator configuration and restart;
the existing session is never treated as proof of a new issuer's login flow.

The admin draft API is documented in `api/openapi.yaml`: GET/PUT
`/api/v1/admin/setup/draft`, POST `/api/v1/admin/setup/validate` and POST
`/api/v1/admin/setup/finish`. Writes require
CSRF and Origin checks. Saves and validation results emit value-free audit events
in the same transaction as their state change. Bootstrap token state remains
separate and is not changed by draft operations.
