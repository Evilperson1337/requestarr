# First-run setup and the Get started checklist

## Signing in as the local administrator (D040)

On a fresh install (no users), startup creates the one built-in local
administrator, username `admin`. Read the generated one-time password in the
container log and sign in at `/login` on your HTTPS public URL. You must choose
your own password (12–128 characters) before doing anything else. Anyone with
log access can read the initial password: never copy logs containing it into
chat or tickets. `REQUESTARR_ADMIN_PASSWORD` can supply the initial password for
automation; it is never logged and still requires a change.

The startup log line to look for begins `Requestarr created the local
administrator. Sign in as 'admin' at <your public URL>/login with the one-time
password in this line`; the password is the `one_time_password` value at its
end. It is written once, when the account is created. With
`REQUESTARR_ADMIN_PASSWORD` the line says so and shows no password.

HTTPS is a prerequisite for the password as well as for OIDC: the session
cookies are Secure, so a browser on a plain `http://` address would not keep
the sign-in, and the password would cross the network in clear. Put a TLS
reverse proxy in front first ([Unraid guide](UNRAID.md)).

An upgrade with existing users creates no local account and preserves their
roles. Existing administrators retain access, and an OIDC configuration in the
environment keeps working unchanged. An existing administrator can create the
local administrator later in **Settings, Authentication** with **Create local
administrator** (the password is shown once and must be changed at first
sign-in). Other people sign in through OIDC and become administrators only when
an existing administrator promotes them in **Settings, Users**. There is no
public administrator-enrollment flow.

## Recovering a forgotten password

With shell/Docker access to the server, run the binary directly (the container
image has no shell):

```sh
docker exec <container-name> /requestarr reset-admin-password
```

For a non-container deployment, run `requestarr reset-admin-password` with the
same database configuration as the server. The command needs no OIDC or session
secret configuration. It prints exactly one new random password after the
transaction commits; keep it private and clear your terminal. It stores only a
hash, requires a password change at the next sign-in, ends **all** of the local
administrator's sessions and records an audit event. Other accounts are
unchanged. A failed database transaction prints no password. If printing fails
after commit, run the command again. If the local administrator does not exist
(for example an upgraded instance that has people but no administrator, so
nobody can reach Settings to create it), the command creates it instead: same
single printed password, must-change set, an audit event, and a note on the
error output saying it was created. Shell access is the gate. The command does
not enable a disabled account.

Historical setup tables and migrations remain for database compatibility but
are no longer read or written by administrator enrollment.

## The Get started checklist (D038)

After signing in and changing the initial password, an administrator can open **Get started** (`/get-started`). It
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
nothing else is carried over. There is no public setup-status endpoint.

## Operator steps

1. Set the database, the master key, the session secret and the public URL for the server. Keep HTTPS in production so password and session cookies are protected; follow [the Unraid guide](UNRAID.md) for reverse-proxy setup. No identity provider is needed yet.
2. On a fresh database, obtain the one-time local administrator password from the startup log (or use your `REQUESTARR_ADMIN_PASSWORD`).
3. Open `/login`, sign in as `admin`, and choose a new password (**Choose a new password**). Open **Get started** (`/get-started`); each item links to the Settings that applies it.
4. In **Settings, Authentication**, fill the **OpenID Connect** card (Provider name, Client ID, Issuer, Client secret), register the shown **Redirect address** with your provider, press **Save OIDC settings**, **Test OIDC connection**, then switch on **Enable OIDC**. It applies at once. Never expose the client secret in the browser or commit it; see [OIDC authentication](OIDC_AUTH.md) and the Keycloak example in the [Unraid guide](UNRAID.md).
5. Sign in once through OIDC with the person who should administer, then in **Settings, Users** promote them. New OIDC sign-ins default to viewer unless **New people** (the default role: viewer or requester) says otherwise; a login never grants admin automatically.
6. For a forgotten local administrator password, use the recovery command above.

If you set `REQUESTARR_OIDC_ISSUER`, `REQUESTARR_OIDC_CLIENT_ID` and
`REQUESTARR_OIDC_CLIENT_SECRET` in the environment instead, Settings,
Authentication shows them as set by the environment and read-only; anything
saved in the app is ignored and the card says so (with **Discard saved
settings**). If only some of the three are set the card names the missing ones
and OIDC stays off. The local administrator keeps working in every case.

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

Keep initial and recovery passwords private, including logs, terminal capture
and CI output. Passwords are hashed at rest. Use HTTPS outside development;
cookies remain Secure. The local administrator is the sole password account:
everyone else signs in through OIDC. Roles are granted by an existing admin in
Settings, Users. OIDC runtime Settings and startup without OIDC are separate
parts of D040; password recovery does not depend on either.
