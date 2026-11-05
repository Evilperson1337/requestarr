# Prompt 02 — Setup Wizard + OIDC

Implement first-run setup bootstrap and OIDC authentication. Follow `docs/FIRST_RUN.md` exactly. The wizard must be resumable/idempotent, setup endpoints require the bootstrap token/session, OIDC discovery is standards compliant, state/nonce/PKCE and cookie protections are implemented, and Keycloak is covered by integration tests. Implement configurable groups claim and group-to-role mapping. Normal post-setup access must not fall back to local passwords. Build the wizard UI matching the approved mockup direction.
