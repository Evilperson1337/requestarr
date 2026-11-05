# 08: Metadata credentials apply at once

**What to build:** Saving the TMDB token, the IGDB secret or client ID, or the Apprise key in Settings takes effect immediately, including which search types are offered, so an Unraid admin never has to leave the app to restart a container. Library connections already work this way (D028). (UX-006; D039.)

**Blocked by:** 05, 06 (the IGDB client ID moves to Settings in 05; 06 changes the same credential field)

**Status:** done

- [x] After saving or clearing a TMDB token, IGDB secret or client ID, or Apprise key, the running server uses the new value without a restart: resolvers, the search types offered by `GET /search/types`, and the notification worker all reflect it
- [x] The UI says "saved and applied", the "pending restart" state is gone for these credentials, and the Movies and TV (and Games) search types appear without reloading the server
- [x] An environment value still wins over a stored one and is still shown as shadowing; a reload that fails (for example a malformed value) leaves the previous working configuration in place and reports the error without leaking the value
- [x] In-flight searches and requests are not broken by a reload (no partial swap); concurrent saves are safe
- [x] Tests cover: save then use without restart; clear then the type disappears; environment precedence; failed reload keeps the old one; race-safe swap (run with the race detector)
- [x] `docs/UNRAID.md`, the template text and `docs/MANUAL_TESTING.md` no longer say a restart is needed; D039 is written in `docs/DECISIONS.md`
- [x] Integration tests run against your own throwaway database; `scripts/check.sh` passes at the end (verified by the implementer on a throwaway database, and again by the orchestrator's full `scripts/check.sh`)
