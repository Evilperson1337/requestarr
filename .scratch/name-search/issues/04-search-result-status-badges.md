# 04: Show requested and in-library status on search results

Parent: `.scratch/name-search/spec.md`

**What to build:** A result for something already requested says "Requested" (with its Request status), and one for something a library already holds says "In library", even if nobody here has ever opened it. Results are never hidden. Clicking an already-requested result opens the usual modal with Notify Me, View request and Add to request.

One batched local query annotates each page (at most 50 results) from PostgreSQL only, with no provider or library calls. It matches catalog items and also library items by their stored provider identifiers, so never-previewed items can show "In library". It must follow resolve-time matching rules: only the latest complete snapshot of an enabled source counts, movie and TV IDs are separate namespaces. Each result's optional local state carries the catalog ID when one exists, the Request status and the availability; absent means "never resolved here", not "missing". The badge is advisory; transactional writes still enforce everything. Status is text plus an icon, not colour alone. This ticket covers Movies & TV; later per-type tickets extend the same query to their own identifiers.

**Blocked by:** 03

**Status:** ready-for-agent

- [ ] A result whose item has an open Request shows "Requested" and the status, and stays in the list
- [ ] A result whose item is in an enabled source's latest complete snapshot shows "In library" even with no catalog row
- [ ] A movie and a TV show with the same numeric ID are not confused
- [ ] A source whose latest sync failed, or that is disabled, does not produce "In library"
- [ ] A never-seen item has no local state and no badge
- [ ] The whole page is annotated with one query, with no provider or library calls (store integration test)
- [ ] Clicking an already-requested result opens the modal with Notify Me, View request and Add to request
- [ ] Badges are readable without colour (browser test)
- [ ] `make fmt test vet web-check`, `make test-integration` and the browser tests pass
