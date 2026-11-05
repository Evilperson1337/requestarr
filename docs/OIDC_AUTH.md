# OIDC authentication

The server provides OIDC authorization-code login at `/api/v1/auth/login`, callback at the fixed configured `REQUESTARR_PUBLIC_URL/api/v1/auth/callback`, current-user lookup at `/api/v1/auth/me`, and POST logout at `/api/v1/auth/logout`. It uses one-time database transactions, PKCE S256, nonce validation, opaque DB-hashed sessions, and role lookup on every session read. Newly provisioned OIDC identities receive the least-privilege `viewer` role; no identity is auto-admin.

## Blockers before enabling login

Set `REQUESTARR_OIDC_ISSUER`, `REQUESTARR_OIDC_CLIENT_ID`, `REQUESTARR_OIDC_CLIENT_SECRET`, and a unique `REQUESTARR_SESSION_HMAC_SECRET` of at least 32 bytes. Set `REQUESTARR_PUBLIC_URL` to the externally reachable canonical origin; register its exact callback URL with the provider. HTTPS is required in deployment so cookies are Secure. POST requests require the matching Origin and `X-CSRF-Token` obtained from `/me`; the CSRF cookie is intentionally readable by the same-origin UI.

Request operations remain intentionally unavailable until request listing/use cases and explicit role authorization are wired. Setup status remains incomplete: authentication configuration does not mean setup has been completed. PostgreSQL integration checks require a disposable database; app startup applies migrations.

## Roles and permissions

Viewers can browse shared requests and use **Notify Me** to subscribe to request updates, but cannot create requests. An administrator must promote a user to `requester` to grant request-creation permission. Requesters can create requests; administrators retain administrative permissions. A user's role is not reset during subsequent OIDC logins. This default change does not alter or demote roles already stored in the database.

## Endpoints and deployment configuration

- `GET /api/v1/auth/login` starts login; state and PKCE transaction is stored for five minutes.
- `GET /api/v1/auth/callback` is the only redirect URI and is derived from `REQUESTARR_PUBLIC_URL`.
- `GET /api/v1/auth/me` returns the current identity and CSRF token with `Cache-Control: no-store`.
- `POST /api/v1/auth/logout` requires same-origin `Origin` and `X-CSRF-Token`.

Configuration uses `REQUESTARR_OIDC_ISSUER`, `REQUESTARR_OIDC_CLIENT_ID`, `REQUESTARR_OIDC_CLIENT_SECRET`, and `REQUESTARR_SESSION_HMAC_SECRET` (minimum 32 bytes). `REQUESTARR_PUBLIC_URL` must be the canonical absolute origin. Session cookie security follows HTTPS on that URL. Configure the reverse proxy to preserve that canonical scheme/host and do not expose the service over plain HTTP in production.
# Initial administrator bootstrap

New accounts receive the `viewer` role. An operator issues a one-time
bootstrap token with `requestarr setup-token --issuer <exact-issuer> --subject <exact-subject> --token-file /secure/path/token`.
The command applies migrations, writes only the token's SHA-256 digest, and creates
the token file exclusively with mode 0600 (it never overwrites an existing file).
Keep the file in a private location, transfer it only through an approved secret
channel, and securely remove it after use. Avoid shell tracing, shared temporary
directories, backups, and command output capture. `--stdout` is an explicit
opt-in; stdout may be captured by terminals, CI systems, or service managers and
must only be used in a controlled environment. The token expires after 30 minutes.
An expired token may be replaced with `--rotate`; a live token or completed setup
cannot be overwritten.

`--issuer` defaults to `REQUESTARR_OIDC_ISSUER`; pass it only to override.

The matching OIDC user must sign in and POST only the `token` to
`/api/v1/setup/claim-admin` with the session cookie, CSRF cookie, matching
`X-CSRF-Token`, and same-origin `Origin`. The server takes issuer and subject from
the verified session; a request that still sends them is rejected unless they equal
the session's. While first-admin setup is unclaimed, `GET /api/v1/auth/me` returns
the caller's own verified issuer and subject as `firstAdminClaim` so the claim page
can show the command to run; once an administrator exists it is never returned (D037).
Consumption, role promotion, setup completion, and audit event commit atomically.
Failures are intentionally generic. Claim traffic should additionally be
rate-limited at the reverse proxy; the endpoint does not expose token validity.

## Development issuer over HTTP

`REQUESTARR_OIDC_ISSUER` must be HTTPS, except that with `REQUESTARR_ENV=development` a plain-HTTP issuer on `localhost`, `127.0.0.1` or `::1` is accepted so the dev compose Keycloak works. Run `make check-login` to verify login against it. The dev realm registers `http://localhost:8080/api/v1/auth/callback` and `http://localhost:18080/api/v1/auth/callback`.
