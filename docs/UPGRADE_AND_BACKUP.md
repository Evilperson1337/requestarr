# Upgrade and backup

Requestarr keeps all of its state in one PostgreSQL database. The OIDC client
secret, the session HMAC secret and the master key (`REQUESTARR_SECRETS_KEY`) live
only in the server environment. Provider tokens, library credentials, the Apprise
key and the Navidrome password may be set in the environment or saved through
Setup (D024); saved values are stored in the database **encrypted** under the
master key. A database backup therefore contains no plaintext credentials (only
hashed session and setup tokens, secret references and ciphertext), but it is
also not enough to restore saved secrets: you need the master key too.

## What to back up

1. The PostgreSQL database named in `REQUESTARR_DATABASE_URL`: requests,
   subscriptions, users and roles, notifications and the Apprise outbox, catalog
   and inventory data, job runs, the setup draft and audit events.
2. Your environment configuration (`.env`, compose file or secret manager),
   including `REQUESTARR_SESSION_HMAC_SECRET`. Changing that secret signs everyone
   out; it does not corrupt data.
3. `REQUESTARR_SECRETS_KEY`, **backed up separately from the database**. Keep it
   in a different store than the dump (a password manager or secret store), so
   that no single leaked backup holds both the ciphertext and its key. If the key
   is lost or changed, stored secrets become unreadable: the server still boots,
   each affected integration stays off and reports "stored secret unreadable", and
   you must re-enter the secrets in Settings, Integrations (or set them in the environment). Key
   rotation tooling is deferred. An unreadable secret can be cleared without the
   key. An environment value always wins over a stored one, and a saved secret
   applies as soon as it is saved. Changing the key itself needs a restart.

Nothing else on the application host needs backing up. The binary and container
image are replaceable.

```sh
# Back up (custom format, compressed, restorable with pg_restore)
pg_dump --format=custom --file=requestarr-$(date +%F).dump "$REQUESTARR_DATABASE_URL"

# Restore into an empty database, with Requestarr stopped
createdb requestarr_restored
pg_restore --no-owner --dbname=requestarr_restored requestarr-2026-10-24.dump
```

Test a restore occasionally; an untested backup is not a backup. Library
inventory is rebuilt by the next sync, but requests, subscriptions, roles and
history exist only in your backup.

## How upgrades work

- The migrations in `db/migrations/` are embedded in the binary and applied
  automatically at startup (`golang-migrate`, which takes a PostgreSQL advisory lock while
  migrating, so concurrent replicas should not race; run one instance during an
  upgrade to be safe). The server does not accept traffic
  until they finish; a failed or dirty migration stops startup with a non-zero exit.
- Migrations are **forward-only** in production. The `.down.sql` files exist for
  tests and local development; do not run them against real data. Applied
  migrations are never edited (a new migration is added instead).
- **Downgrading the binary after a migration has run is unsupported.** Roll back
  by restoring the backup you took before upgrading.
- The schema version at the time of writing is 23 (`000023_stored_secrets`);
  the highest migration in the release you install is the target. A
  database from a pre-release prototype should be recreated rather than upgraded:
  early migrations were corrected in place before they shipped (see D-notes in
  `docs/DECISIONS.md`).

## Upgrade steps

1. Read the release notes for new or changed environment variables.
2. Take a database backup (above).
3. Replace the binary or image tag and restart. Watch the log for
   `database ready`; a migration failure is reported before the listener starts.
4. Check `GET /readyz` and sign in. `GET /api/v1/version` shows the running build.
5. As an administrator, open **Settings** (operational status) and confirm Sources
   show a recent successful sync; trigger **Sync now** if you do not want to wait.
   A failed sync never marks previously available items missing.

If startup fails on a migration, do not edit the database by hand. Restore the
backup, keep the previous version running, and report the migration error.

## Migration history

| Version | Adds |
| --- | --- |
| 000001 | Initial schema (catalog, requests, users, setup state) |
| 000002-000005 | Request use case, auth, auth runtime, catalog platforms |
| 000006-000009 | Availability core, admin bootstrap, request transitions, availability versioning |
| 000010-000011 | Durable in-app notifications, global Apprise outbox |
| 000012-000014 | Admin user audit, Jellyfin absence reconciliation, viewer role audit |
| 000015 | Persistent job runs |
| 000016-000019 | Multi-source inventory, catalog item aliases, platform-aware evidence, match suggestions |
| 000020 | Request list snapshots (pagination) |
| 000021-000022 | Setup wizard drafts, wizard completion (unused since D038; the tables stay) |
| 000023 | Encrypted stored secrets and their value-free audit events |

## Operational notes

- Job-run history is pruned per Source (newest 200 kept); notifications and audit
  events are retained until you remove them.
- The first administrator is created with `requestarr setup-token`; see
  `docs/SETUP_WIZARD.md`. Back up nothing for this: the token is single-use.
