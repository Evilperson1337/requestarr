# Security Baseline

- Everyone except the built-in local administrator signs in through OIDC; Keycloak compatibility covered by integration tests. The local administrator's password is hashed (PBKDF2), must be changed at first sign-in, and failed attempts are delayed (D040).
- Authorization enforced server-side for every mutating/admin endpoint. Four roles (viewer, requester, admin, superuser, D041); authentication, the local administrator, stored secrets, library connections and the Get started checklist are superuser-only. Group names from an ID token decide a role at sign-in and are never stored or logged.
- Admin user management exposes the IdP-provided email address and display name to administrators so they can deliberately identify and select accounts; no OIDC tokens or credentials are exposed.
- The initial local administrator password is random, shown once in the server log (or supplied by `REQUESTARR_ADMIN_PASSWORD`), never stored in plaintext, and must be changed at first sign-in. Shell access is the recovery gate (`reset-admin-password`).
- Never expose provider/library secrets after storage; UI returns redacted configuration state.
- Encrypt secret material at rest or use a deployment secret provider before production release.
- CSRF protections appropriate to session/cookie architecture; SameSite/Secure/HttpOnly cookies.
- OIDC state, nonce, PKCE, issuer/audience validation and strict redirect URL policy.
- SSRF controls for administrator-supplied integration URLs: allow explicit schemes, reject unsafe URL forms, resolve/re-check destinations where practical, and document private-LAN requirements.
- Outbound HTTP clients have finite connect/TLS/header/body timeouts and maximum response sizes.
- Artwork/proxy behavior must not become an unrestricted URL fetcher.
- Sanitize logs. Authorization/cookie/token/secret values are always redacted.
- Audit request state changes, subscription changes, admin configuration changes, and setup and sign-in events.
- Database migrations and backups documented before stable release.
- Rate-limit URL resolution, authentication callbacks, setup validation, and notification test actions.
