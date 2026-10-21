# First-run setup

## What the current setup page does

The browser page is a bootstrap guide and first-admin claim form; it is **not** a configuration wizard. It reads public `GET /api/v1/setup/status`. When setup is required, it offers OIDC sign-in and, after sign-in, collects the exact issuer and subject plus the one-time setup token. The claim request is CSRF-protected. The server verifies that the submitted issuer and subject exactly match the verified session identity and that the CLI-bound token is valid. On success the first administrator can enter the application.

The page does not receive identity fields or the setup token from the backend. It does not save the token to browser storage. The token field is cleared as soon as submission begins and after the request finishes. Failed claims use a generic message.

## Operator steps

1. Configure OIDC in the Requestarr server environment before first sign-in. At minimum set `REQUESTARR_OIDC_ISSUER`, `REQUESTARR_OIDC_CLIENT_ID`, and `REQUESTARR_OIDC_CLIENT_SECRET`; also set the public base URL and callback-related settings required by the deployment/provider. Follow [OIDC authentication](OIDC_AUTH.md) and the variable names documented in [`.env.example`](../.env.example). Never expose client secrets in the browser or commit them. Newly signed-in users are viewers who can browse and use Notify Me; administrators must promote users to requester to grant request-creation permission.
2. Restart Requestarr after changing environment configuration. Register the deployed callback URL with the OIDC provider as described in the OIDC guide.
3. Determine the exact issuer URL and subject (`sub`) of the identity that should become the first administrator. These are operator-provided values; the UI does not discover them.
4. On the server run `requestarr setup-token --issuer '<exact issuer>' --subject '<exact subject>'`. The CLI writes the high-entropy token to a protected file by default. Keep it private and provide it to the intended administrator through a secure channel. The token is bound to those exact identity values.
5. Open the Requestarr setup page, sign in with that OIDC identity, and enter the same exact issuer, subject, and setup token. The claim endpoint requires an authenticated session and CSRF protection.
6. On successful claim, the first administrator is routed into the application. Setup status then reports complete and later visitors follow the normal sign-in/app flow.

## Security and scope

Do not expose setup-token output or the token file to untrusted users. Prefer limiting network access to the instance during bootstrap. The UI intentionally does not echo server error bodies on claim failure. The backend never returns the token or verified issuer/subject as setup status.

The current implementation does **not** edit environment variables, configure or test OIDC, configure metadata providers, libraries, notifications/Apprise, request policy, or provide a multi-step review/commit flow. Those settings remain operator-managed; the future full setup wizard described in planning materials is not implemented. OIDC must work before the claim UI can be used.
