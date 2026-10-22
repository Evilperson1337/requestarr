# 02: Integration test target

**What to build:** A single make target runs the PostgreSQL integration suite against the dev compose database, so SQL, migrations and transactions are exercised on demand and a real baseline is known.

**Blocked by:** None (can start immediately)

**Status:** done

- [x] A make target runs the suite with the integration build tag against the compose Postgres
- [x] scripts/check.sh can include it when a database URL is set
- [x] The baseline result is recorded (pass/fail with any failures listed) in docs/research/ or the implementation plan
- [x] docs/TESTING.md explains how to run it
