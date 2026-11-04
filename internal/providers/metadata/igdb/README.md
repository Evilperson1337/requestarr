# IGDB resolver and searcher

Implements `metadata.Resolver` (igdb.com game URLs) and `metadata.Searcher` (name search, `search.go`). Keep provider-specific DTOs here and map them into `domain.ResolvedItem` / `domain.SearchResult` before returning. Enforce outbound HTTP timeouts, response-size limits, and provider-specific rate limits.

## Name search

- Query: `search "<text>"; fields ...; where (game_type = (0,8,9,10,11) | game_type = null); limit 21; offset N;`. Page N uses `offset = (N-1)*20`; the 21st row only signals `hasMore`.
- The user's text is escaped (backslash, double quote; control characters become spaces) and is the only interpolated value.
- Refs are `https://www.igdb.com/games/<slug>`, which `Resolve` accepts; a row whose slug the resolver would refuse is dropped.
- Concurrency is capped at 3 searches in flight (IGDB allows 4 requests per second and 8 open requests, shared with resolves). A search that cannot get a slot within 2s, and any 429/5xx from IGDB, is `ErrBusy`; a search is never retried.

## Game kind field: NOT VERIFIED LIVE

IGDB's documentation marks `category` deprecated in favour of `game_type`. The search was written against the documented `game_type` enum and has never been run against the live API (no IGDB credentials were available when it was written). Treat as unverified until someone with credentials checks:

- that `game_type` is accepted in `fields` and in `where game_type = (...)`;
- the enum ids used here, taken from the docs: 0 main_game, 8 remake, 9 remaster, 10 expanded_game, 11 port (shown); 1 dlc_addon, 2 expansion, 3 bundle, 4 standalone_expansion, 5 mod, 6 episode, 7 season, 12 fork, 13 pack, 14 update (hidden);
- that `game_type = null` is accepted in a `where` clause. A game with no `game_type` is treated as a main game and kept, in both the query (`| game_type = null`) and the Go mapping; whether the live API accepts that clause, and whether it actually has such rows, is also unverified live.

Tests use a fixture (`testdata/search_games.json`) shaped from the documentation, so they prove the mapping, not the live field.
