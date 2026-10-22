# 10: Sync now and job status in Settings

**What to build:** An admin can trigger a sync for a Source and see recent runs and errors in the Settings page.

**Blocked by:** 09 Persistent job runner

**Status:** ready-for-agent

- [ ] An admin-only trigger endpoint and button exist; non-admins get 403
- [ ] Triggering while a run is active reports it rather than starting another
- [ ] Settings shows recent runs with status and errors
- [ ] Tests cover the role check and the active-run case
