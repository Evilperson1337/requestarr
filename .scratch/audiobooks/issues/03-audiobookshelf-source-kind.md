# Audiobookshelf Source kind and settings

Status: ready-for-agent
Blocked by: 01

Register `audiobookshelf` in `internal/sources/sources.go`: `MediaTypes=[audiobook]`, credential fields URL and API key (Stored secret, D024, never logged, never returned by the API). Settings > Libraries gets the new kind with a connection test.

- Connection test: `GET /api/libraries` with `Authorization: Bearer`; require at least one `mediaType: book` library; report 401 as a bad key and a server older than the API-key release as an upgrade hint (>= 2.26.0). Header auth only, never `?token=`.
- Setup wizard and Settings copy: recommend a dedicated ABS `user` account with library access limited and update/delete/upload off (research §1, no read-only user type exists).
- Docs: add the connection to the setup docs and note that BookOrbit is not supported.

Acceptance: an admin can add, test, disable and remove an ABS connection; the key never appears in responses or logs.
