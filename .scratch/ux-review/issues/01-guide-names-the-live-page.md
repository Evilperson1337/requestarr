# 01: The guides send people to the page that applies their settings

**What to build:** The Unraid guide and the manual checklist tell an admin to add libraries "in Setup", which is the inactive draft wizard. They must name the page that actually applies a library: Settings, Integrations. (UX-001, docs part; this is a fix to documentation I shipped.)

**Blocked by:** None (can start immediately)

**Status:** done

- [x] `docs/UNRAID.md` section 7 and every other place it says to add a library, credential or provider "in Setup" names Settings, Integrations, and says that saving there applies at once (libraries) or after a restart (metadata credentials, until ticket 08)
- [x] `docs/MANUAL_TESTING.md` section 14 and the template and example text that say the same are corrected
- [x] The words "Setup" and "Settings" are used consistently across `docs/`, the template description and the app's own text that this ticket touches; the wizard is called the setup wizard only where it still exists
- [x] The deployment-file guide check still passes (`bash scripts/deploy-files-test.sh`)
- [x] `scripts/check.sh` passes (run on the integration branch tip with the PostgreSQL integration tests and Playwright)
