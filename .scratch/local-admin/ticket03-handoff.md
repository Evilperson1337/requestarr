# Ticket03 merger and ticket04 backend handoff

## Separate merger

- Worker: `.claude/worktrees/agent-a884313af677de3a3`, branch `feat/local-admin-ticket03`.
- Code commit: `00feceaf85347ba525715cca2bdf9ce06afcffb9`; the following commit only resolves the issue and records evidence. Read the worker branch tip for the final hash.
- Integration base/latest incorporated: `1e5a13d38ed045159b3151877e1d3cdf59a9cd21` (`feat/local-admin`). Worker-side merge completed “Already up to date”; ancestry checked after implementation.
- Review [verification](ticket03-verification.md) and [resolved ticket](issues/03-oidc-configured-at-runtime.md), then merge the worker into integration in a separate merger effort. This task deliberately did not merge into or edit integration.
- Protected root stays clean at `feat/unraid-deployment`, `a5dbdb4af7bc43e6e5541d79a1a715e3701695fd`. Preserve unrelated workers and their resources.
- Worker-local private validation artifacts and the owned PostgreSQL container are retained; see verification for resource ownership and safe reuse/cleanup constraints. Do not publish the raw logs or private environment files.

## Ticket04 API seams (no UI implemented)

[OpenAPI](../../api/openapi.yaml) is authoritative. All admin routes use the common role, Origin, must-change and CSRF policies; settings responses are non-cacheable.

| Operation | Route under `/api/v1` | Semantics |
| --- | --- | --- |
| Public options | GET `/auth/options` | Exactly `oidcEnabled`, `providerName`; no private state, local option lookup only |
| Authentication state | GET `/admin/settings/authentication` | Five-state resolution, saved/effective non-secret values, missing/ignored/differing variable names, redirect URI, secret/key capability, working snapshot |
| Save | PUT `/admin/settings/authentication` | `enabled`, `providerName`, `issuer`, `clientId`, optional `clientSecret`; empty/omitted retains; enabled candidate always re-discovers before commit |
| Test | POST `/admin/settings/authentication/test` | Same candidate; diagnostic discovery only, no state mutation; environment unit used when set |
| Switch | PUT `/admin/settings/authentication/enabled` | `enabled`; disable retains values, enable re-discovers |
| Discard | DELETE `/admin/settings/authentication` | Removes saved metadata and encrypted secret atomically; environment provider remains unchanged |
| Recovery state | GET `/admin/settings/authentication/local-administrator` | `exists`, `disabled`, `canDisable` |
| Explicit recovery creation | POST same recovery route | No body needed; 201 with `username`, one-time `password`, `mustChangePassword`; never repeat/reset on conflict |
| Recovery disable state | PUT recovery route + `/disabled` | `disabled`; last enabled admin protected; disable revokes local sessions; value-free actor/target audit |

Use the existing local-password-change endpoint for rotation. OIDC secret edits must go through Authentication, not the generic secret routes. Environment overrides are read-only but Discard remains available for shadowed saved values. Missing master key is reported without preventing local recovery; show the recovery creation response only once and never place its password in persistent browser storage, logs or diagnostics. The later sign-in screen can use public options and keep the local form reachable; this worker does not implement ticket04/05 UI or mark their acceptance complete.

Migration 36 adds authentication-settings audit; migration 37 adds local-administrator-management audit. Existing migrations were not edited. Preserve numbering/order when integrating other workers.
