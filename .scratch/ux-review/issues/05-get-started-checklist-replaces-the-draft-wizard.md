# 05: A Get started checklist replaces the inactive draft wizard

**What to build:** After claiming admin, the admin lands on a short checklist that shows live state and links to the page that applies each item, instead of a seven-step draft that applies nothing. The IGDB client ID, the only draft value that takes effect today, becomes a live setting in Settings. (UX-001; D038.)

**Blocked by:** 01, 04

**Status:** done

- [x] The setup draft wizard (UI, `/admin/setup/draft`, `/admin/setup/validate`, `/admin/setup/finish`, and `/admin/setup/source-kinds` if nothing else uses it) is removed with its tests and OpenAPI entries; database tables and migrations are left alone; the OpenAPI contract test passes
- [x] A checklist page, shown to admins after the claim and from the nav until every required item is done or the admin dismisses it, computes each item from live state: at least one library connected and synced, a metadata provider configured (a TMDB token enables Movies and TV search), notifications configured (optional), default role and approval policy reviewed (optional); each item links to the Settings section that applies it
- [x] Dismissing is remembered for the instance; the nav item disappears when everything required is done or dismissed; non-admins never see it
- [x] The IGDB client ID is a field in Settings, Integrations next to the IGDB secret; saving it takes effect where the runtime reads it today (an environment value still wins); a client ID previously saved in the old draft keeps working after upgrade
- [x] No value is entered twice: the checklist asks for nothing itself
- [x] After claiming admin the app lands on the checklist; the Playwright setup and smoke tests are rewritten to match; accessibility checks on the new page pass
- [x] `docs/FIRST_RUN.md`, `docs/UNRAID.md` and `docs/MANUAL_TESTING.md` describe the checklist; D038 is written in `docs/DECISIONS.md` and notes what was retired and why
- [ ] Integration tests run against your own throwaway database; `scripts/check.sh` passes at the end
