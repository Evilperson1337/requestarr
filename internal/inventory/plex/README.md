# Plex authoritative inventory — Phase A/B

This package is a **read-only, in-memory** Plex inventory reader. It does not
configure sources, persist snapshots, schedule jobs, create inventory cycles,
reconcile requests, or mutate Plex or any acquisition software.

## Supported Plex JSON contract

The adapter supports only Plex Media Server's modern JSON responses:

1. `GET /library/sections` returns a JSON `MediaContainer.Directory` array with
   unique `key`, `title`, and `type` fields. Supported section types are
   `movie`, `show`, and `artist`.
2. For each configured selected section and enabled supported media type, it
   calls `GET /library/sections/{key}/all` with `type`,
   `X-Plex-Container-Start`, and `X-Plex-Container-Size=100` query parameters.
3. Each content response supplies `MediaContainer.offset`, `size`,
   `totalSize`, and `Metadata`. Completion is accepted only after every page is
   contiguous, has a stable total, and yields exactly `totalSize` items.
4. Supported item mappings are Plex `movie`, `show`, `album`, and `track` to
   Requestarr movie, TV, album, and song. Source item IDs are stable only within
   the selected section and are emitted as `{section-key}:{ratingKey}`.
5. Recognized GUIDs are limited to `tmdb://`, `tvdb://`, and
   `musicbrainz://`; unknown or malformed GUIDs are omitted. They are metadata
   hints only, not availability evidence.

Authentication is sent exclusively in the `X-Plex-Token` request header. The
token is not added to query parameters, errors, or logs.

## Safety contract

Production Plex endpoints must use HTTPS. Plaintext HTTP is rejected by default
and may be enabled only with the narrowly scoped `AllowLoopbackHTTP` option for
development or tests; every resolved destination must then be loopback. An
allowed CIDR never authorizes plaintext HTTP. The standalone transport disables
proxies and redirects, validates and pins DNS addresses, denies non-public HTTPS
destinations unless an explicitly allowed CIDR is provided, requires TLS 1.2 or
newer for HTTPS, and applies connection, header, body, and total request limits.
Responses must be bounded UTF-8 `application/json` and reject duplicate JSON
keys.

## Explicit non-goals

- Plex source configuration schema, API routes, setup UI, secrets persistence,
  jobs, scheduler, and application wiring.
- Snapshot/cycle commits, removal calculation, persistence projections, and
  reconciliation mutation.
- Plex playlist, collection, watch-state, artwork, metadata refresh, or any
  write endpoint.
- Acquisition clients or Radarr, Sonarr, Lidarr, download-client, indexer,
  torrent, or Usenet integration.

## Assumptions retained for the next slice

`/library/sections/{key}/all` is assumed to honor `type` filtering with the
numeric Plex types 1, 2, 9, and 10 and to return an exact `totalSize`. Mixed
music libraries are intentionally read once per enabled album/track type. A
future projection slice must define durable selected-section configuration,
credential retrieval, source revision/cycle ownership, item upsert identity,
and removal semantics before this reader can affect availability.
