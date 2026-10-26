# 01: Stored TMDB token, end to end (tracer)

**What to build:** An operator sets REQUESTARR_SECRETS_KEY, then an admin enters the TMDB token in Setup. It is stored encrypted, never shown again (only a presence flag and its source), checked using the stored value, applied on restart, and recorded in a value-free audit event. This builds the whole spine for later tickets: master key validation, the AES-256-GCM sealer behind a narrow interface, a new migration and repository (with key id), startup resolution of environment then stored value before Validate, a write-only set/clear admin endpoint with CSRF and Origin checks, the connection check using the resolved value and naming its source, and the first Setup field including a disabled state when no master key exists. Update api/openapi.yaml first (D024).

**Blocked by:** None (can start immediately)

**Status:** ready-for-agent

- [x] Malformed master key fails startup; an absent key is allowed and the UI refuses secret values (config tests; Playwright disabled-field test; startup wiring verified by build, not by a live boot)
- [x] Round-trip, wrong-key, tampered-ciphertext and key-id tests pass; environment value wins over stored
- [x] A wrong or missing key at boot leaves the server running and the TMDB secret reported as unreadable, not unconfigured (unit tests of startup resolution, GET /admin/secrets and the UI; the /admin/status note is wired but has no test; no live process boot)
- [x] The secret never appears in any response, log line, error or audit row (PostgreSQL integration test covers stored and audit rows; HTTP handler tests cover responses, errors and logs with a fake service)
- [x] The TMDB check uses the value the next restart would use and names its source
- [x] Secret input is cleared after submit and not kept in browser storage; field is keyboard and screen-reader usable
- [x] A new migration is added; applied migrations are untouched
