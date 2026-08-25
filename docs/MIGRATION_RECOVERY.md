# Certified dirty-v20 migration recovery

This document covers one exceptional operational procedure only. It is not a
general migration repair or force interface.

## Scope and safety boundary

[`requestarr maintenance recover-dirty-v20`](../cmd/requestarr/main.go:53) is
available only when migration history reports **version 20 and dirty=true**.
The command rejects every clean version, every other dirty version, malformed
arguments, and any confirmation other than the documented exact phrase. Normal
application startup remains fail-closed for all dirty history.

Run it only with the application stopped and a tested database backup available:

```bash
requestarr maintenance recover-dirty-v20 --confirm=certify-dirty-v20-recovery
```

The command holds the normal PostgreSQL migration advisory lock on one
connection across history inspection, certification, history reset, and replay.
It first proves that the physical database is exactly the transactional rollback
state from the known unmodified v20 failure: no v20 objects exist and every
existing v19 configuration projection and revision evidence record validates.

Only then does it reset migration history to **clean v19** and replay ordinary
embedded migrations from v20 forward. It never marks v20 clean, accepts an
arbitrary version, executes a down migration, or inspects/diffs unrelated
application data. History-read or database/system failures are reported as
sanitized operational errors; eligibility failures remain distinct and do not
change history. Interrupting the command with SIGINT or SIGTERM cancels it.

The command requires only a valid `REQUESTARR_DATABASE_URL`; unrelated runtime
configuration is not evaluated and the URL is never echoed. Migration
[`000024_source_configuration_orphan_audit_validation.up.sql`](../db/migrations/000024_source_configuration_orphan_audit_validation.up.sql:1)
also rejects pre-existing orphan audit or audit-selection evidence during the
reachable forward correction.

Do not modify applied migrations
[`000020_source_configuration_credential_envelope_evidence.up.sql`](../db/migrations/000020_source_configuration_credential_envelope_evidence.up.sql:1)
through
[`000023_dirty_v20_recovery_audit_integrity.up.sql`](../db/migrations/000023_dirty_v20_recovery_audit_integrity.up.sql:1).
Verify the procedure on a backed-up disposable PostgreSQL database before a
production operation.
