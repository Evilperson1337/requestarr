Status: ready-for-agent

# UX review: first run, new people, approvals, follow-up

Source: the product UX review of 2026-11-05 (findings UX-001 to UX-008), `GLOSSARY.md`, and decisions D028, D030 and D034 in `docs/DECISIONS.md`. All evidence in the review was code-traced; no browser was run.

## Problem Statement

The requester's path (search, click, request) is already short. The friction is in everything around it:

- An operator who has just claimed admin is sent to a seven-step Setup wizard that collects about 40 inputs and applies almost none of them. Nothing outside the setup draft reads the instance name, timezone, public URL, OIDC fields, library addresses, Apprise URL or the request policy; only the IGDB client ID takes effect. The live place to connect libraries is Settings, and the Unraid guide wrongly tells people to use Setup.
- The first-admin claim asks for the issuer and subject, which the server already knows from the signed-in session, and the subject can only be found in the identity provider's admin console.
- New people sign in as viewers and cannot request, nobody tells the admin they exist, and promotion signs them out.
- Every request waits for a manual per-request approval click that gates nothing, with no signal that anything is waiting; the auto-approve policy in the wizard is never applied.
- Requesters only learn an outcome by opening the app; the nav shows no unread count.
- Metadata credentials (TMDB token, IGDB secret, Apprise key) apply only after a restart, and a library connection needs two separate saves.

## Solution

- A short "Get started" checklist with live state replaces the draft wizard; each item links to the Settings page that actually applies it, and the IGDB client ID becomes a live setting.
- The claim page shows the signed-in person their own issuer and subject as a ready-to-run command and asks only for the token.
- An admin-set default role for new sign-ins (still `viewer` by default) and an in-app notice to admins when someone signs in for the first time.
- An auto-approve policy by media type that really applies, and an awaiting-approval count in the admin nav.
- An unread count on the Notifications nav item.
- One save for a library connection, and metadata credentials that apply at once.

## Implementation Decisions

- **Draft wizard retired (D038).** The seven-step draft is removed (UI, the `/admin/setup/draft`, `/validate`, `/finish` routes and their tests and OpenAPI entries). Database tables and migrations stay (migrations are append-only). Its replacement is a checklist computed from live state. The IGDB client ID moves to Settings and applies live.
- **Claim by token (D037).** The token stays bound to one issuer and subject and the shell stays the real gate. The claim form takes only the token; the server derives issuer and subject from the verified session. The caller's own identity is shown only to that caller, only while first-admin setup is unclaimed. `setup-token` defaults `--issuer` to the configured issuer.
- **People policy (D035).** A new setting, default role for first sign-in, one of `viewer` or `requester`, default `viewer`. It never produces `admin`; it applies only to users created after it is set. A first sign-in notifies every admin in-app with a link to Settings, Users.
- **Approval policy (D036).** A setting, auto-approve by media type, default none. A request in an auto-approved type is created as `approved` with an audit event saying so. The admin nav shows a count of requests still `requested`.
- **Signals.** The Notifications nav item shows the unread count.
- **Live credentials.** TMDB, IGDB and Apprise secrets reload at runtime like library connections already do, including which search types are offered. A library connection saves its address and credential in one atomic request.
- All new settings live in one small settings store, with environment values still winning where an environment variable exists, as for stored secrets (D024).

## Testing Decisions

- Test external behaviour at the existing seams: the HTTP API handlers (with fakes), the configuration and setup services, the PostgreSQL integration tests, the OpenAPI contract test, and Playwright for the UI.
- Prior art: `internal/httpapi` handler tests, `internal/setup` tests, the library connection tests, `web/tests/setup.spec.ts`, `web/tests/library-issues.spec.ts`, and the OpenAPI contract tests.
- Every security-relevant change (claim, roles, first-admin identity exposure) has negative tests: wrong token, expired token, a different signed-in user, a viewer reading another person's data, a default role never becoming admin.

## Out of Scope

- A blended search across media types (UX-008, D032).
- Mapping IdP groups to roles, per-user push notifications (email or Apprise targets per person), invite-by-email, and bulk approval actions. Each is a separate product decision.
- Anything in the Library and Issues flows, match suggestions, and mobile layouts, which this review did not trace.

## Further Notes

- Decision numbers D035 to D039 are reserved for these tickets so parallel work does not collide: 02 uses D035, 03 uses D036, 04 uses D037, 05 uses D038, 08 uses D039.
- Tickets that touch the PostgreSQL store test against their own throwaway database, never the shared `requestarr_test`.
