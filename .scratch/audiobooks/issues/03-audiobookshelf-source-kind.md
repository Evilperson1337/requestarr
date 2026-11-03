# Audiobookshelf Source kind and settings

Status: resolved
Blocked by: 01

Register `audiobookshelf` in `internal/sources/sources.go`: `MediaTypes=[audiobook]`, credential fields URL and API key (Stored secret, D024, never logged, never returned by the API). Settings > Libraries gets the new kind with a connection test.

- Connection test: `GET /api/libraries` with `Authorization: Bearer`; require at least one `mediaType: book` library; report 401 as a bad key and a server older than the API-key release as an upgrade hint (>= 2.26.0). Header auth only, never `?token=`.
- Setup wizard and Settings copy: recommend a dedicated ABS `user` account with library access limited and update/delete/upload off (research §1, no read-only user type exists).
- Docs: add the connection to the setup docs and note that BookOrbit is not supported.

Acceptance: an admin can add, test, disable and remove an ABS connection; the key never appears in responses or logs.

## Answer

Registered the `audiobookshelf` kind (`internal/sources`, stable ID, `REQUESTARR_AUDIOBOOKSHELF_URL` / `_API_KEY`, stored secret `audiobookshelf_api_key`), wired through config, the setup draft (`libraries.audiobookshelf`, target `library:audiobookshelf`), the connection check, OpenAPI and web types, with migration `000032` widening the `library_sources.kind` and `library_connections.slug` checks (applied and reverted against a throwaway Postgres). The check calls `GET /api/libraries` with a Bearer header and passes only when a `book` library is visible. Because Settings > Libraries and the wizard are driven by the registry, no page needed a hand edit.

Not as specified: the check reports only the generic code, so a 401 is not distinguished from a server older than 2.26.0 (those hints are in docs/INTEGRATIONS.md and `.env.example`). BookOrbit is documented as unsupported. No Audiobookshelf logo asset was added (the UI falls back to a blank mark).
