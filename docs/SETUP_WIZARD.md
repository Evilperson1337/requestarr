# First-run setup and the Get started checklist

## Claiming the first admin

First-admin bootstrap is an OIDC sign-in plus one-time claim (D037). The claim
page reads public `GET /api/v1/setup/status`, which exposes only whether the
first admin has been claimed, never an identity or a setting. The server checks
the verified session, CSRF and the CLI-bound token. Everything else is set up
afterwards, live, in Settings.

## The Get started checklist (D038)

After the claim an administrator lands on **Get started** (`/get-started`). It
replaces the seven-step inactive draft wizard that saved values and applied
nothing. The checklist has no fields of its own: every item is computed from the
instance's live state and links to the part of Settings that applies it, so no
value is ever entered twice.

| Item | Required | Done when |
| --- | --- | --- |
| Connect a media library | yes | a library is connected and has completed a sync (Settings, Integrations, Media libraries) |
| Add a metadata provider | yes | a TMDB token (Movies and TV search) or IGDB (Games) is configured; MusicBrainz is always on and does not count (Settings, Integrations, Metadata) |
| Set up notifications | no | Apprise is configured (Settings, Integrations, Notifications) |
| Review who can do what | no | an administrator has saved the default role for new people or the approval policy (Settings, Users) |

- Required and optional items are listed apart, each with a text status ("Done"
  or "To do"), not only a colour.
- **Dismiss checklist** hides it for the whole instance and is remembered (stored
  in the settings store, `setup.checklist_dismissed`). The nav item **Get started**
  shows for administrators only, until every required item is done or the
  checklist is dismissed; the page stays reachable by its address.
- The API is `GET /api/v1/admin/checklist` and `POST /api/v1/admin/checklist/dismiss`,
  both administrator-only (the second needs CSRF), documented in `api/openapi.yaml`.
- The **IGDB client ID** is a live field in Settings, Integrations, Metadata,
  beside the IGDB secret (`GET`/`PUT /api/v1/admin/settings/igdb`). It applies
  at once, like the secrets (D039). `REQUESTARR_IGDB_CLIENT_ID` still wins; the
  field is then read-only.

### Upgrading from the draft wizard

The draft tables and migrations stay in the database, unused. A client ID saved
in the old draft keeps working: while no client ID has been saved in Settings, it
is read from the old draft at startup. Saving one in Settings (even an empty
one) supersedes it. Nothing else the old draft held was ever applied, so
nothing else is carried over. `wizardComplete` is gone from
`GET /setup/status`.

## Operator steps

1. Configure OIDC in the Requestarr server environment before first sign-in. At minimum set `REQUESTARR_OIDC_ISSUER`, `REQUESTARR_OIDC_CLIENT_ID`, and `REQUESTARR_OIDC_CLIENT_SECRET`; also set the public base URL and callback-related settings required by the deployment/provider. Follow [OIDC authentication](OIDC_AUTH.md) and the variable names documented in [`.env.example`](../.env.example). Never expose client secrets in the browser or commit them. Newly signed-in users are viewers who can browse and use Notify Me; administrators must promote users to requester to grant request-creation permission.
2. Restart Requestarr after changing environment configuration. Register the deployed callback URL with the OIDC provider as described in the OIDC guide.
3. Sign in to Requestarr as the person who should become the first administrator (they are a viewer at first). The claim page then shows that person's own subject in a ready-to-run command; there is no need to look the ID up in the identity provider.
4. On the server run the command the claim page shows, `requestarr setup-token --subject '<subject>'`. `--issuer` defaults to the configured `REQUESTARR_OIDC_ISSUER`; the page adds it only when the sign-in issuer differs. The CLI needs exactly one of `--token-file PATH` (creates a new file readable only by its owner, mode 0600) or `--stdout` (prints the token; for a container with no safe place for a file, see [the Unraid guide](UNRAID.md)). Keep the token private and provide it to the intended administrator through a secure channel. The token is bound to those exact identity values.
   While no administrator exists, the server log prints once at startup that the claim page shows the command, with the subject as a placeholder; it never prints a token or an identity.
5. Paste only the token on the claim page. The claim endpoint requires an authenticated session and CSRF protection and takes issuer and subject from the verified session.
6. On successful claim, the first administrator lands on **Get started** (`/get-started`), described below. Public `GET /api/v1/setup/status` reports only whether the first admin has been claimed.

## Secrets

Secret values are write-only fields (D024). Settings, Integrations shows one such
field for each stored-capable secret, in its integration's card. A saved value is stored in PostgreSQL encrypted under
the master key `REQUESTARR_SECRETS_KEY` (32 bytes as 64 hex characters, or base64
that decodes to 32 bytes; for example `openssl rand -hex 32`). Without that key
the fields are disabled and the environment path keeps working. Rules:

- An environment value always wins over a saved one. Each secret reports its
  source: `environment`, `stored`, or `stored (shadowed by environment)`; A shadowed
  value is never deleted automatically.
- A saved or cleared credential applies at once and the field says "saved and
  applied": library credentials (D028), and the TMDB read token, the IGDB client
  ID and secret and the Apprise key (D039). Movies & TV and Games appear (or
  disappear) as search types immediately, search uses a new TMDB token on its
  next call, and the notification worker uses a new Apprise key on its next
  delivery. Albums, Songs and Audiobooks need no credential and are always
  offered. If the running providers cannot be rebuilt the value stays saved, the
  previous configuration stays in use, the field says so, and the card keeps
  "saved, pending restart". Only `REQUESTARR_SECRETS_KEY`, environment variables
  and `REQUESTARR_APPRISE_URL` need a restart.
- Values are never returned, logged or audited; only presence and source are
  shown. Replacing or clearing a secret is always available.
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
- `REQUESTARR_AUDIOBOOKSHELF_API_KEY`

`REQUESTARR_OIDC_CLIENT_SECRET`, `REQUESTARR_SESSION_HMAC_SECRET` and
`REQUESTARR_SECRETS_KEY` stay environment-only: sign-in must work before Setup is
reachable. The Apprise URL stays environment-only.


## Library connection checks

Settings, Integrations has an explicit **Test connection** for each connected library: one
bounded read-only request using the stored address and credentials, with a fixed
safe message and no inventory sync. Checks have a ten-second deadline, a 1 MiB
JSON response limit and disabled redirects; arbitrary private, link-local or
loopback destinations are blocked at a DNS-validated, pinned dial boundary,
except a library address the administrator entered, which is probed only when it
passes the private-host rule (D033). A failed check never changes known
availability.

## Security and scope

Do not expose setup-token output or the token file to untrusted users. Prefer limiting network access to the instance during bootstrap. The UI intentionally does not echo server error bodies on claim failure. The backend never returns the token, and returns the verified issuer/subject only to their owner while setup is unclaimed (see D037); public setup status never carries them.

Nothing here provides a temporary or local-password sign-in. OIDC must work
before the claim page is usable. Changing the active issuer still requires
operator configuration and a restart.
