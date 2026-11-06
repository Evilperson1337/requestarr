Status: resolved

# A built-in local administrator, and OIDC configured in Settings

Source: the grilling session of 2026-11-05, `GLOSSARY.md` (Local administrator), and D040 in `docs/DECISIONS.md`, which this spec implements. It changes D034, D037 and D038 where they depend on the first-admin claim.

## Problem Statement

Before anyone can sign in to a new Requestarr instance, the operator needs an OIDC provider, a reverse proxy, correct environment variables, a CLI command run inside the container and a copied token. On a home server that is the hardest part of the product, and it happens before the operator has seen anything work. OIDC itself can only be configured by environment variables and a restart, so a typo means editing the container and starting over.

## Solution

A fresh instance creates one built-in **local administrator** at first startup and prints its one-time password in the log. The operator signs in, chooses a real password, and configures OIDC from Settings, Authentication with a Test button, applying at once. Everyone else signs in through OIDC. The local administrator stays as the permanent recovery path. The old claim flow is removed.

## User Stories

### First run
1. As an operator, I want a working administrator account created at first startup, so that I can sign in without an identity provider.
2. As an operator, I want the generated password shown once in the container log, so that I can read it from the Unraid log view.
3. As an operator, I want to set the initial password through an environment variable when I automate deployment, so that no log reading is needed.
4. As an operator, I want to be forced to choose my own password at first sign-in, so that the generated one is never a lasting credential.
5. As an operator, I want the log line to say exactly what to do next, so that I am not left guessing.
6. As an operator, I want the app to start with no OIDC configuration at all, so that I can set it up from the interface.
7. As an operator, I want an instance that already has people and administrators to create no new account on upgrade, so that an upgrade never adds a credential I did not choose.
8. As an administrator on an upgraded instance, I want to create the local administrator from Settings when I want a recovery account, so that I decide whether it exists.

### Signing in
9. As the local administrator, I want to sign in with a username and password, so that I can administer the instance.
10. As the local administrator, I want a clear error that never says whether the username or the password was wrong, so that nobody can probe for accounts.
11. As the local administrator, I want repeated failures to slow down further attempts without locking me out, so that an attacker cannot lock me out of my own recovery account.
12. As the local administrator, I want every successful and failed sign-in recorded, so that I can see attempts on the account.
13. As the local administrator, I want passwords of 12 to 128 characters with no composition rules, so that I can use a passphrase.
14. As the local administrator, I want changing my password to end my other sessions, so that a stolen session does not survive a change.
15. As the local administrator, I want to change my password from Settings, so that I can rotate it.
16. As the local administrator, I want a command that resets my password from the server, so that a forgotten password is not a database repair.
17. As the local administrator, I want that command to print a random password once and force a change at the next sign-in, so that the reset leaves no known password.
18. As a person with OIDC configured, I want "Sign in with <provider>" as the main sign-in action, so that I use my normal account.
19. As an administrator, I want an "Administrator sign-in" link beside it, so that I can still use the local account.
20. As an operator with no OIDC configured, I want the sign-in screen to show only the local form, so that nothing offered is broken.
21. As an operator, I want the local sign-in reachable at its own address even when OIDC is broken, so that a bad setting can never hide it.
22. As a person who is not signed in, I want the sign-in screen to explain what Requestarr is and how to sign in, so that I know what to do.

### Configuring OIDC
23. As an administrator, I want an Authentication page in Settings, so that sign-in is configured where other integrations are.
24. As an administrator, I want to enter issuer, client ID and client secret, so that I can connect my identity provider.
25. As an administrator, I want the client secret stored encrypted and never shown again, so that it is handled like other secrets.
26. As an administrator, I want to see the redirect address to register with my provider and copy it, so that I do not assemble it by hand.
27. As an administrator, I want a Test button that runs provider discovery and says what failed, so that I find mistakes before enabling.
28. As an administrator, I want a configuration that fails its test to be unable to be enabled, so that I cannot break sign-in by saving.
29. As an administrator, I want a failed change never to replace a working configuration, so that a typo does not take sign-in down.
30. As an administrator, I want changes to apply at once, so that I do not restart the container.
31. As an administrator, I want to set the name shown on the sign-in button, so that people see "Sign in with Keycloak".
32. As an administrator, I want an enable switch that keeps my settings, so that I can turn OIDC off and on without retyping.
33. As an administrator, I want the page to say whether OIDC is working right now, so that I know its state without testing.
34. As an administrator, I want to see when the environment sets OIDC, so that I know why I cannot edit it here.
35. As an administrator, I want a visible warning when the environment overrides saved settings, naming what differs, so that I am not confused about which values are used.
36. As an administrator, I want to discard the saved settings from that warning, so that I can clear the conflict.
37. As an administrator, I want a clear error naming the missing variable when the environment sets only some OIDC values, so that I can fix the deployment.
38. As an administrator, I want the page to say when secrets cannot be saved because no master key is set, so that I know what to configure.

### People and roles
39. As an administrator, I want the local administrator listed in Users and labelled as such, so that I know what it is.
40. As an administrator, I want its role locked to admin, so that I cannot demote the recovery account by mistake.
41. As an administrator, I want to disable the local administrator only while another admin exists, so that I can never remove every way in.
42. As an administrator, I want to promote a person who signed in through OIDC from Settings, Users, so that they become an admin without any token.
43. As an administrator, I want new OIDC sign-ins to get the default role I chose, so that the earlier policy still applies.
44. As an operator, I want the Get started checklist to treat OIDC as optional, so that it does not demand it to finish.

### Deploying
45. As an Unraid user, I want the template's OIDC fields to be optional overrides, so that I can install without an identity provider.
46. As an Unraid user, I want an optional field for the initial administrator password, so that I can set it at install.
47. As an Unraid user, I want the guide to walk me from first sign-in to a working OIDC setup, so that I do not need another document.
48. As an Unraid user, I want the guide to keep the HTTPS requirement and explain why a password needs it, so that I put a proxy in front.
49. As an upgrading user, I want my environment-configured OIDC to keep working unchanged, so that nothing breaks.

## Implementation Decisions

- **Local credential.** A new table holds the local administrator's credential: the user it belongs to, the password hash with its scheme, parameters and salt, a "must change" flag, a disabled flag and timestamps. The local administrator is an ordinary user row with the admin role, so sessions, roles and the Users list work unchanged; the row is marked as the built-in account. A new migration adds both; migrations stay append-only.
- **Hashing.** One small module owns password hashing and verification behind a narrow interface (hash, verify, needs-rehash), implemented with the standard library's PBKDF2-SHA256 at the current OWASP iteration count, with a per-password salt and constant-time comparison. The scheme and iteration count are stored with the hash so they can be raised later.
- **Local sign-in.** A sign-in endpoint accepts a username and password, applies the protection below, and on success creates a session by the existing mechanism (opaque token hashed at rest, cookie, CSRF). A first-sign-in session may do nothing except change the password until it has. The password-change endpoint verifies the current password, enforces length 12 to 128, ends the account's other sessions and clears the must-change flag.
- **Protection.** Failed attempts add an exponentially growing delay per account and per client address, capped, held in memory in the single process (a restart clears it; documented). Nothing locks the account. Every success and failure is written to a new audit table. Responses and timing do not reveal whether the username or the password was wrong.
- **Creation.** At startup, if there are no users, the local administrator is created with a random password (or `REQUESTARR_ADMIN_PASSWORD` when set), must-change set, and the generated password is logged once as a single clear line. If users already exist, nothing is created. An admin can create it from Settings, Authentication, which returns the generated password once in the response and never again.
- **Reset command.** A `reset-admin-password` subcommand next to the other subcommands generates a random password, stores its hash with must-change set, ends the account's sessions, prints the password once, and fails clearly when the account does not exist. It replaces `setup-token`.
- **Removal.** The claim endpoint, the claim page, the `firstAdminClaim` field on the current-user response, the claim-related status, the startup claim hint, the `setup-token` subcommand and their tests, OpenAPI entries and docs are removed. The tables stay.
- **OIDC settings.** Non-secret settings (enabled, provider name, issuer, client ID) live in the existing settings store; the client secret is a stored secret (D024) and needs the master key. The OIDC authenticator is held behind an atomically swapped snapshot, like the metadata providers (D039): a save rebuilds it, a failed rebuild leaves the previous one in place and reports the error without leaking the value, and a configuration that has not passed discovery cannot be enabled. The server starts with no OIDC configuration.
- **Environment as a unit.** If any `REQUESTARR_OIDC_ISSUER`, `_CLIENT_ID` or `_CLIENT_SECRET` is set, all three come from the environment and saved values are ignored. A small pure function takes the environment values and the saved values and returns one of: not configured, saved only, environment only, environment overriding saved (listing which saved values differ or are ignored), and environment incomplete (listing what is missing). The settings API and the page are built on its result.
- **Authentication API.** Admin-only routes read the state, save settings, run the discovery test, discard saved settings, create the local administrator and change the local administrator's disabled state. The public sign-in options (is OIDC enabled, its provider name, is the local form offered) come from an unauthenticated response that carries nothing else.
- **Sign-in screen.** It shows the OIDC button when enabled with the provider name, an "Administrator sign-in" link, or only the local form; the local form has its own address that is always served.
- **Users and roles.** The Users list marks the local administrator, locks its role, and refuses to disable it unless another admin exists; the last-admin protection already in place keeps applying.
- **Checklist and deployment.** The Get started item for sign-in becomes "OIDC configured" and is optional. The Unraid template's OIDC fields become optional advanced overrides, a masked optional field supplies the initial administrator password, and the compose example follows; the guide and first-run documentation are rewritten around signing in first and configuring OIDC from Settings.
- **Unchanged.** `https://` stays required for the public URL outside development; the stored-secret mechanism, the roles, the default role for first OIDC sign-in and the library and provider settings are unchanged.
- **Design.** The Authentication page follows the existing Settings layout and is reviewed with the design skill as part of its ticket, applying the skill's findings only where they fit the existing system.

## Testing Decisions

- A good test checks behaviour a person or operator can observe, through the seams below, not implementation details.
- **Seam 1, local sign-in and sessions:** handler tests with a fake store (wrong password, unknown username, delay growth and cap, no lockout, audit events for success and failure, forced change, session revocation on change, length limits, response uniformity) and PostgreSQL integration tests for the credential table, creation on a fresh database and no creation when users exist. Prior art: the auth handler tests and the claim tests being removed.
- **Seam 2, `reset-admin-password`:** tests through the binary's entry point against a test database, in the style of the existing `setup-token` tests: prints once, forces change, ends sessions, fails when no account exists, never prints a hash.
- **Seam 3, OIDC settings:** the pure state function tested with a table covering every state and every combination of set and missing variables; an app-level test of the live swap with a fake provider (save, failed test keeps the old one, cannot enable without a passing test, enable and disable, environment precedence, startup with no OIDC). Prior art: the provider-set tests from D039.
- **Seam 4, UI:** Playwright with axe for the sign-in screen in each state, the forced-change first sign-in, and the Authentication page including each conflict message and the missing master key state; the design-skill review is an acceptance step.
- **Seam 5, deployment files:** the existing deploy-files check extended for the optional OIDC override fields and the initial-password field (masked, no default).
- Every authentication change includes explicit tests for unauthenticated, viewer, requester and admin paths (`AGENTS.md`).

## Out of Scope

- Any local account other than the built-in administrator, local user management, self-registration and password reset by email.
- Multi-factor authentication for the local administrator, and passkeys.
- A choice of password hashing scheme beyond the stored-parameters upgrade path, and a login lockout.
- OIDC group-to-role mapping, multiple OIDC providers, and social login.
- Changing the public-URL HTTPS rule, and any change to the roles themselves.

## Further Notes

- rc.3 ships before this work, with the current claim flow; this is the plan for rc.4.
- The generated password is the most sensitive value the app ever prints. It appears once, in one log line, and nowhere else; tests assert it is absent from every other output.
- On an Unraid host the log is readable by anyone with Docker access; the forced change at first sign-in is what limits that exposure.
