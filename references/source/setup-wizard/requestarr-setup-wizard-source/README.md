# Requestarr Setup Wizard Demo

Interactive first-run setup wizard source for Requestarr. The demo includes:

- Welcome, general configuration, OIDC, metadata, libraries, notifications,
  request policy, review, and completion screens.
- Keycloak, Authentik, Authelia, and generic OpenID Connect presets.
- Claims, group-to-role mapping, PKCE, verified-email, provisioning, and group
  allowlist controls.
- Responsive desktop and mobile layouts.
- Working light/dark mode with saved browser preference.

## Run locally

```bash
npm ci
npm run dev
```

The project uses a Vinext/Next-compatible React structure. The reusable wizard
implementation is in `app/page.tsx`; its complete visual system is in
`app/globals.css`.

## Integrating into Requestarr

This demo intentionally keeps all state in one component so the entire flow is
easy to review. For the production application, split it into:

```text
features/setup/
  SetupWizard.tsx
  setup-schema.ts
  setup-api.ts
  steps/
    WelcomeStep.tsx
    GeneralStep.tsx
    AuthenticationStep.tsx
    MetadataStep.tsx
    LibrariesStep.tsx
    NotificationsStep.tsx
    RequestPolicyStep.tsx
    ReviewStep.tsx
```

Replace simulated connection tests with backend endpoints. Never expose or
persist OIDC client secrets in browser storage. Submit secrets directly to the
Go API over HTTPS and store them using the project's encrypted-secret strategy.

Recommended production endpoints:

```text
GET  /api/setup/status
POST /api/setup/oidc/test
POST /api/setup/metadata/test
POST /api/setup/libraries/test
POST /api/setup/notifications/test
POST /api/setup/complete
```

The frontend configuration model in `app/page.tsx` is suitable as the starting
shape for the API request, but secrets should be write-only and omitted from
responses.

## Theme implementation

The theme toggle:

1. Reads `requestarr-theme` from `localStorage` after hydration.
2. Falls back to the operating-system color preference.
3. Sets `data-theme="light|dark"` on the document root.
4. Persists explicit user changes.

For the full application, move this logic into a shared `ThemeProvider` so the
setup wizard and normal Requestarr interface use one preference.

## Demo limitations

- Connection tests are simulated.
- Configuration is not submitted to a server.
- Provider marks are lightweight UI badges rather than official brand assets.
- The setup state resets when the page is refreshed; only the theme persists.

