# 01: Record the name-search decision and update the wording

Parent: `.scratch/name-search/spec.md`

**What to build:** Make the project's documents say that name search is allowed, so no agent or maintainer reads "link-first" as a prohibition. Add a new dated decision (next number after D031), "Name search is a front door to the resolver": search returns refs the resolver already accepts, creates no new request path, never writes, is a post-release V1.x feature, and assumes self-hosted free AGPL use is non-commercial for TMDB, MusicBrainz and IGDB (image CDNs are hotlinked, as previews already do). D002 stays accepted; the new decision supersedes only its "no search" part. Split the scope document's single native-search/discovery exclusion so name search is V1.x and discovery, trending and recommendations stay out. Reword the agent rules' accepted decision 1, the project summary and the architecture notes (module description, plus search as a fourth interactive caller of providers beside URL resolution, connection tests and background jobs). Point the search research note at the settled answers.

**Blocked by:** None (can start immediately)

**Status:** ready-for-agent

- [x] The decision log has the new entry, with the provider-terms assumption and the two unread terms (Twitch Developer Service Agreement, Cover Art Archive) listed as open items
- [x] D002 is unchanged except for a pointer to the new entry
- [x] The scope document lists name search under V1.x and keeps discovery, trending and recommendations out
- [x] The agent rules, project summary and architecture notes no longer describe request entry as link-only, and the fast-path principle names search as an interactive caller
- [x] The research note's open questions are marked settled or still open to match the decision
- [x] No behavior or code changes; `make fmt test vet web-check` still pass
