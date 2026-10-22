# 10: Sync now and job status in Settings

**What to build:** An admin can trigger a sync for a Source and see recent runs and errors in the Settings page.

**Blocked by:** 09 Persistent job runner

**Status:** done

- [x] An admin-only trigger endpoint and button exist; non-admins get 403
- [x] Triggering while a run is active reports it rather than starting another
- [x] Settings shows recent runs with status and errors
- [x] Tests cover the role check and the active-run case
