# OIDC authentication

The server provides OIDC authorization-code login at `/api/v1/auth/login`, callback at the fixed configured `REQUESTARR_PUBLIC_URL/api/v1/auth/callback`, current-user lookup at `/api/v1/auth/me`, and POST logout at `/api/v1/auth/logout`. It uses one-time database transactions, PKCE S256, nonce validation, opaque DB-hashed sessions, and role lookup on every session read. Newly provisioned OIDC identities receive the least-privilege `viewer` role; no identity is auto-admin.

## Blockers before enabling login

OIDC is optional at startup. Set a unique `REQUESTARR_SESSION_HMAC_SECRET` of at least 32 bytes. To configure OIDC using deployment overrides, set all three of `REQUESTARR_OIDC_ISSUER`, `REQUESTARR_OIDC_CLIENT_ID` and `REQUESTARR_OIDC_CLIENT_SECRET`; otherwise use the runtime Authentication API described below. Set `REQUESTARR_PUBLIC_URL` to the externally reachable canonical origin; register its exact callback URL with the provider. HTTPS is required in deployment so cookies are Secure. POST requests require the matching Origin and `X-CSRF-Token` obtained from `/me`; the CSRF cookie is intentionally readable by the same-origin UI.

Authentication configuration does not grant an administrator role. PostgreSQL integration checks require a disposable database; app startup applies migrations.

## Runtime Authentication API (D040)

The backend for Settings, Authentication applies changes without a process restart. The Authentication page (Settings, Authentication) uses these endpoints; the final sign-in-screen design is a separate ticket.

- `GET /api/v1/auth/options` is unauthenticated and returns **only** `oidcEnabled` and `providerName`, from the current local snapshot. It makes no outbound request and carries `Cache-Control: no-store`. The local form remains served at `/login` even when OIDC is unavailable; an unavailable OIDC start answers 503 rather than panicking.
- Admin-only `GET /api/v1/admin/settings/authentication` returns saved non-secret metadata, effective metadata, the derived redirect URI, secret presence/readability, master-key capability, override conflicts and the current snapshot's working state. “Working” means a discovered snapshot is active, not that a new health request has just succeeded. No settings response ever contains the client secret.
- Admin-only `PUT` at the same address accepts `enabled`, `providerName`, `issuer`, `clientId` and optional write-only `clientSecret`. An omitted or empty secret retains the encrypted saved secret. A replacement requires `REQUESTARR_SECRETS_KEY`. Metadata, encrypted secret and value-free audit commit in one database transaction. Identical saves do not duplicate durable changes.
- `POST /api/v1/admin/settings/authentication/test` accepts the same candidate and performs bounded discovery only, without saving or publishing. Discovery checks provider metadata; it does **not** prove client credentials or a complete user login. Save and enable always re-run discovery for their own exact configuration; a previous Test is never a reusable authorization to enable different values.
- `PUT /api/v1/admin/settings/authentication/enabled` accepts `enabled`; disabling retains settings and does not need provider reachability or a readable secret. Enabling requires successful discovery. `DELETE /api/v1/admin/settings/authentication` atomically discards saved metadata and the stored secret. The generic secret API cannot mutate the OIDC secret independently.
- If **any** of the three OIDC environment values is non-empty, all three effective values come from the environment. Saved values never fill gaps. The response state is one of `not_configured`, `saved_only`, `environment_only`, `environment_overriding_saved` or `environment_incomplete`. `missing`, `ignored` and `differing` contain variable names, never secret values. Environment configuration is read-only, but saved shadowed settings may still be discarded. Removing environment overrides takes a restart, as with other deployment configuration.
- No OIDC configuration, incomplete overrides, lost master key and a temporarily unavailable provider leave local recovery available. Complete malformed issuer overrides still fail configuration validation rather than weakening the deployment URL policy. A failed runtime discovery or save never replaces a working snapshot or saved configuration. Runtime writes serialize, and each in-flight sign-in retains its original provider **and identity issuer** for its five-minute browser-bound transaction. A restart invalidates those in-memory snapshots; start sign-in again.
- Discovery, token and JWKS requests have context cancellation, a ten-second client timeout, an explicit user agent, JSON-only bodies capped at 1 MiB, and no HTTP redirects. Issuer and discovered authorization/token/JWKS endpoints must use HTTPS without userinfo, query or fragment. Loopback HTTP is allowed only in development; private-network HTTP is not an exception for identity providers. Discovery errors expose only fixed stage descriptions.

All mutations require the existing matching Origin, session and CSRF protections. Viewer and requester roles cannot read or write Authentication settings. Responses are not cacheable. See the [HTTP contract](../api/openapi.yaml) for payloads and status codes.

### Recovery-account management backend

The admin-only `/api/v1/admin/settings/authentication/local-administrator` endpoint supports GET (presence, disabled state and disable eligibility) and POST (explicit creation on an upgraded instance). Creation returns a generated password **once**, in its non-cacheable response, never in logs; it does not reset an existing credential. The account must change that password on first sign-in. PUT to its `/disabled` suffix accepts `disabled` and preserves its password. Disable requires another enabled administrator, revokes all local sessions and commits a value-free actor/target audit in the same transaction. Role changes and disable share the last-admin lock; a disabled local account does not count as a remaining administrator. The built-in account's role is locked to admin. Settings, Authentication drives these endpoints (create, disable and enable, and change password through `/auth/local/password`); it is not additional local-account management. The Get started checklist lists "OIDC configured" as an optional item, done once a provider is active.

## The built-in local administrator (D040)

One account, `admin`, signs in with a password instead of OIDC. It is an ordinary `users` row with the admin role, marked `builtin`, with the fixed `oidc_subject` `local:admin` (OIDC subjects are stored as `oidc:<digest>`, so they cannot collide). Its credential (hash, must-change, disabled) is in `local_credentials`; every sign-in success and failure, password change and creation is in `auth_audit_events`, which never holds password material.

- **Creation.** At startup, only when there are no users, it is created with a random 24-character password, or with `REQUESTARR_ADMIN_PASSWORD` (12 to 128 characters; startup fails naming the variable otherwise). Must-change is set either way. A generated password is printed in exactly one Info log line, after the account is committed; an environment-supplied password is never printed. With users already present nothing is created.
- **Sign-in.** `POST /api/v1/auth/local/login` with JSON `username` and `password`, behind the local form at `/login` (always served by the single-page app). It needs a matching `Origin` and a JSON content type, because it has no session to carry a CSRF token. A wrong password, an unknown username and a disabled account answer the same 401 after the same hashing work.
- **Delay, not lockout.** Failed attempts add a delay of 1 second doubling to a 5 minute cap, per account name (after 2 free typos) and per client address (after 5), held in memory, so a restart clears it. At most one password verification may be in flight for an account name or client address, including current-password checks; overlapping requests answer 429 with `Retry-After` without hashing. Reservations are released when the attempt ends, and tracked in-flight state is bounded. A delayed request also answers 429 with `Retry-After` and is not checked; nothing locks the account. The client address is the TCP peer: behind a reverse proxy that is the proxy, which is why the address allowance is looser.
- **Passwords.** PBKDF2-HMAC-SHA256, 600000 iterations, 16-byte random salt, stored as `pbkdf2-sha256$<iterations>$<salt>$<hash>` so the cost can be raised later. 12 to 128 characters, no composition rules.
- **Forced change.** While `mustChangePassword` (on `/auth/me`) is true, every API route except `/auth/me`, `/auth/logout` and `/auth/local/password` answers 403 `password_change_required`. The change verifies the current password, refuses reusing it, ends the account's other sessions and clears the flag. The credential update, session revocations and successful password-change audit (including the client address) commit together; audit failure rolls them all back. A sign-in that verified a credential superseded by a committed password change cannot create a session.
- **First-run setup and recovery.** Sign in as `admin` at `/login`, choose a new password, then open Get started. `requestarr reset-admin-password` (or `docker exec <container-name> /requestarr reset-admin-password`) prints one new random password, forces a change, revokes all local-account sessions and audits the change atomically. It needs only database configuration and fails clearly if the account does not exist. It does not enable a disabled account. Keep the printed password private; neither a hash nor database credentials are printed. See [First run](FIRST_RUN.md).

## Roles and permissions

Viewers can browse shared requests and use **Notify Me** to subscribe to request updates, but cannot create requests. An administrator must promote a user to `requester` to grant request-creation permission. Requesters can create requests; administrators retain administrative permissions. A user's role is not reset during subsequent OIDC logins. This default change does not alter or demote roles already stored in the database.

## Endpoints and deployment configuration

- `GET /api/v1/auth/login` starts login; state and PKCE transaction is stored for five minutes.
- `GET /api/v1/auth/callback` is the only redirect URI and is derived from `REQUESTARR_PUBLIC_URL`.
- `GET /api/v1/auth/me` returns the current identity and CSRF token with `Cache-Control: no-store`.
- `POST /api/v1/auth/logout` requires same-origin `Origin` and `X-CSRF-Token`.

Configuration uses `REQUESTARR_OIDC_ISSUER`, `REQUESTARR_OIDC_CLIENT_ID`, `REQUESTARR_OIDC_CLIENT_SECRET`, and `REQUESTARR_SESSION_HMAC_SECRET` (minimum 32 bytes). `REQUESTARR_PUBLIC_URL` must be the canonical absolute origin. Session cookie security follows HTTPS on that URL. Configure the reverse proxy to preserve that canonical scheme/host and do not expose the service over plain HTTP in production.
## Administrator access

Only the built-in local administrator starts with the admin role. An existing
administrator promotes OIDC users in Settings, Users. New OIDC accounts receive
viewer, or requester if that default was chosen; never admin. The current-user
response contains the user, CSRF token and password-change state, not the raw
OIDC issuer/subject. See [First run and recovery](FIRST_RUN.md).

## Development issuer over HTTP

`REQUESTARR_OIDC_ISSUER` must be HTTPS, except that with `REQUESTARR_ENV=development` a plain-HTTP issuer on `localhost`, `127.0.0.1` or `::1` is accepted so the dev compose Keycloak works. Run `make check-login` to verify login against it. The dev realm registers `http://localhost:8080/api/v1/auth/callback` and `http://localhost:18080/api/v1/auth/callback`.
