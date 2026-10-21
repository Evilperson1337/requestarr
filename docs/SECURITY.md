# Security Baseline

- OIDC required after first-run setup; Keycloak compatibility covered by integration tests.
- Authorization enforced server-side for every mutating/admin endpoint.
- Admin user management exposes the IdP-provided email address and display name to administrators so they can deliberately identify and select accounts; no OIDC tokens or credentials are exposed.
- Bootstrap setup token is temporary, high entropy, and invalidated after completion.
- Never expose provider/library secrets after storage; UI returns redacted configuration state.
- Encrypt secret material at rest or use a deployment secret provider before production release.
- CSRF protections appropriate to session/cookie architecture; SameSite/Secure/HttpOnly cookies.
- OIDC state, nonce, PKCE, issuer/audience validation and strict redirect URL policy.
- SSRF controls for administrator-supplied integration URLs: allow explicit schemes, reject unsafe URL forms, resolve/re-check destinations where practical, and document private-LAN requirements.
- Outbound HTTP clients have finite connect/TLS/header/body timeouts and maximum response sizes.
- Artwork/proxy behavior must not become an unrestricted URL fetcher.
- Sanitize logs. Authorization/cookie/token/secret values are always redacted.
- Audit request state changes, subscription changes, admin configuration changes, and setup completion.
- Database migrations and backups documented before stable release.
- Rate-limit URL resolution, authentication callbacks, setup validation, and notification test actions.
