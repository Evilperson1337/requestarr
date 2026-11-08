Status: ready-for-agent

# A read-only diagnostics token, so an agent can see a running instance

Source: the conversation of 2026-11-08 (rc.8 test on Unraid). Builds on D041 (roles, superuser-only routes) and D042 (Settings, Logs). Decision recorded as D043 when accepted.

## Problem Statement

Diagnosing the OIDC, RomM and song-search problems of 2026-11-07 meant guessing from code, asking the operator to paste log lines, and building probe scripts, because an agent has no way to look at a running Requestarr. The pieces to look at already exist as superuser-only routes (`GET /admin/logs`, `/admin/status`, `/admin/job-runs`, `/admin/library-connections`), but they need a browser session of a signed-in superuser. Handing an agent that session would give it everything a superuser can do, including clearing credentials and changing roles.

## Solution

A **diagnostics token**: a long random API token, created and revoked by a superuser in Settings, that can only call a short, fixed list of read-only routes and nothing else. It expires, is shown once, is stored only as a keyed hash, and stops working at once when revoked. A small wrapper script uses it from a private file the agent never prints. An MCP server, if wanted later, is a thin wrapper over the same routes.

## User Stories

1. As a superuser, I want to create a diagnostics token in Settings, so that I can let a tool read the state of my instance without giving it my session.
2. As a superuser, I want the token shown once when created and never again, so that it cannot be read back from Requestarr.
3. As a superuser, I want every token to expire (30 days by default, 90 at most), so that a forgotten token stops working by itself.
4. As a superuser, I want to see each token's name, creation date, expiry and last use, and revoke any of them, so that I know what has access.
5. As a superuser, I want a revoked or expired token refused on its very next request, so that revoking is real.
6. As a superuser, I want a token to stop working if I stop being a superuser who can sign in, so that demoting me demotes what I created.
7. As an operator, I want a token to be unable to change anything, so that a leaked one can only be used to look.
8. As an operator, I want the list of routes a token may call to be short, fixed in code and checked by a test, so that a new route is never readable by a token by accident.
9. As an operator, I want a token to see logs, operational status, sync runs and library connection status, and not people's emails, roles, requests or secret values, so that a leak exposes operations, not people.
10. As an operator, I want creating and revoking a token audited, and use throttled, so that misuse leaves a trace and guessing is not practical.
11. As an agent, I want one command per view (status, logs, sync runs, connections), so that I can read the instance in a single step.

## Implementation Decisions

- **Token.** `rqd_` followed by 32 random bytes, base64url. Stored as HMAC-SHA256 under the session secret (as sessions are, `auth.Hash`), never in plain text. A new table `diagnostics_tokens`: id, name (1 to 80 plain characters), created_by (user id), created_at, expires_at (required), last_used_at, revoked_at, token_hash (unique). At most 5 unrevoked, unexpired tokens at once. Migration 42, append-only like the others.
- **Where it works.** `Authorization: Bearer rqd_...` only; never a query parameter or cookie. When the header is present the request is judged as a token request only. The route table gains an explicit marker, set per route when it is registered; a route without the marker answers 401 to a token. Only `GET` routes may carry it.
- **Readable routes (first list).** `GET /admin/logs`, `GET /admin/status`, `GET /admin/job-runs`, `GET /admin/library-connections` and `GET /admin/secrets` (names, source and configured flags, never values). Not readable: Users, requests, notifications, Authentication settings, the checklist, anything that mutates. `TestTokenReadableRoutesAreExactlyThese` pins the list, like `TestSuperuserOnlyRoutesAreExactlyThese`.
- **Who may use it.** A token is valid only while its creator is an enabled superuser who can sign in (checked on every request) and it is neither expired nor revoked.
- **Managing tokens.** `GET`, `POST` and `DELETE /admin/diagnostics-tokens[/{id}]`, superuser-only, CSRF on change, `POST` returns the token once in a `no-store` response. A token can never create or revoke tokens. UI: a card in Settings, Logs.
- **Abuse.** Failed bearer attempts are throttled per client address with the existing sign-in throttle; a token is limited to 60 requests a minute. `last_used_at` is written at most once a minute.
- **Audit and logging.** Create and revoke write a value-free row to the auth audit with the actor. Failed use logs at warning level with a fixed reason (unknown, expired, revoked, creator not a superuser), never the value. Successful reads are not logged (it would fill the log being read).
- **Client.** `scripts/agent/requestarr-diag.sh status|logs|runs|connections` reads the address and token from `~/.config/requestarr-probe/` (mode 600 enforced, as `romm-probe.sh`), never prints the token, and formats the JSON for reading.
- **Contract.** `api/openapi.yaml` gains a bearer security scheme and an `x-token-readable: true` marker on the five routes; a test ties the marker to the route table.

## Testing Decisions

- Route table: the readable list is exactly the five routes; every other route answers 401 to a valid token; every non-GET answers 401 to a token.
- Token lifecycle: expired, revoked, unknown, malformed and wrong-prefix tokens are refused with the same 401; revoking takes effect on the next request; demoting or disabling the creator disables the token; the sixth active token is refused.
- Storage: a PostgreSQL test shows the plain token is nowhere in the database; a canary token never appears in logs, audit rows or any response except the single creation response.
- Throttle: repeated failures from one address are refused without checking the token.
- UI: Playwright with axe at desktop and phone widths covers create (shown once), list and revoke.
- `scripts/check.sh` passes; the log-field guardrail test still passes.

## Out of Scope

- An MCP server: decided against building it first; it becomes a thin wrapper over these routes if wanted (ticket 05).
- Any write or action through a token, including starting a sync.
- Per-token route scopes. One fixed read-only set.

## Decisions on the open questions (2026-11-08)

1. `GET /admin/secrets` and `GET /admin/library-connections` are readable by a token: they show where each credential comes from and what is missing, never a value.
2. Lifetime: 30 days by default, 90 at most.
3. A token cannot read `GET /admin/users`, even with emails removed.
