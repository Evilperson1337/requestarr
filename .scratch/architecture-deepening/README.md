# Architecture deepening

Output of `/improve-codebase-architecture` on 2026-10-29. Six deepening candidates, grilled and agreed. Tickets are in `issues/`.

Order and dependencies (one branch and PR per ticket, each green before the next starts):

    01 sync health  ->  02 sources  ->  03 secrets  ->  04 runtime  ->  05 probes  ->  06 route policy

Work branches from `feat/stored-secrets` once its in-progress changes are committed.

## Decisions

- New pure-data package `internal/sources` holds the Source kind registry. `domain` keeps media types. It imports no HTTP, DB, provider or `config` code.
- A kind declares an ordered list of Credential fields (Navidrome = username + password). No per-kind special cases remain.
- The server serves the Source kind and secret registries (`api/openapi.yaml`); the web app renders from them and the TS copies are deleted.
- `internal/secrets` stays free of `config`/`setup` imports; `app` composes them. `secrets.Service` implements what `httpapi`/`setup` need directly.
- Job runs are authoritative for run outcomes; `library_sources` keeps snapshot facts. No migration now; unused columns are dropped in a later one (migrations are append-only).
- Deliberate behavior change: `Config.Validate` gains an IGDB client id + secret pairing rule (ticket 04). Record in D026.
- The seam for snapshot persistence has two adapters: Postgres and a test-only in-memory one, both run by one contract suite.
- New glossary terms already added: Source kind, Credential field, Sync health. D026 is written when ticket 01 lands, extended by 02 and 04.

## Out of scope

No source code changes were made in the planning pass. Uncommitted `feat/stored-secrets` work is untouched.
