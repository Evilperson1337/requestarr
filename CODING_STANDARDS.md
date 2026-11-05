# Coding standards

Read during review. These are judgement calls; anything a test, `go vet`, gofmt or `tsc` already enforces is not repeated here. `AGENTS.md` holds the binding product and engineering rules, and `docs/DECISIONS.md` the accepted decisions.

## Where per-kind knowledge lives

- A fact about a Source kind (its media types, credential fields, env var names, ID, display name) belongs in `internal/sources`, and callers loop over the registry. A test lists the few files allowed to spell a kind's slug; widening that list needs a reason in the diff.
- A fact about a stored secret (env var, label, noun) belongs in `internal/secrets/registry.go` or the Source kind registry, not in a caller or the web app. The web app renders labels it gets from the API.

## HTTP

- Every application route registers through the route table (`rt.handle`) with an access level. A handler does not check sessions or roles itself.
- Narrow existing exception: `auth.Handler.Register` owns the authentication/session bootstrap routes (`/auth/login`, `/auth/callback`, `/auth/logout`, `/auth/me`, `/auth/local/login`, `/auth/local/password`). These routes establish, inspect or end sessions, or gate the local credential change; they use the auth handler's own session/CSRF checks (and Origin plus JSON checks for local sign-in). The router mirrors each policy with `rt.record` for the route-policy/contract checks. Keep those records aligned with registration; this exception does not permit other application handlers to bypass `rt.handle`.
- A new dependency of the router is a required field of `httpapi.Deps`. Discovering a capability by type assertion is a review finding.
- A route change and its `api/openapi.yaml` entry land in the same change.

## Tests

- Test at the module's interface: the registry, `app.Build`, `setup.Service`, `Checker.Describe`/`fetch`, the route table, the snapshot contract. A test that reaches past the interface (private helpers, SQL in a unit test) needs a reason.
- A new snapshot-persistence rule is a case in `internal/store/snapshottest`, so both adapters must agree on it.
- Expected values in a test come from the spec or a literal, never recomputed the way the code computes them.

## Messages and secrets

- Administrator-facing wording for a connection check lives in `internal/setup/checks/messages.go`.
- A value from a secret never appears in a message, log line, error, response or test failure output. Tests use a canary value and assert it is absent.
