# 04: Show requested and in-library status on search results

Parent: `.scratch/name-search/spec.md`

**What to build:** A result for something already requested says "Requested" (with its Request status), and one for something a library already holds says "In library", even if nobody here has ever opened it. Results are never hidden. Clicking an already-requested result opens the usual modal with Notify Me, View request and Add to request.

One batched local query annotates each page (at most 50 results) from PostgreSQL only, with no provider or library calls. It matches catalog items and also library items by their stored provider identifiers, so never-previewed items can show "In library". It must follow resolve-time matching rules: only the latest complete snapshot of an enabled source counts, movie and TV IDs are separate namespaces. Each result's optional local state carries the catalog ID when one exists, the Request status and the availability; absent means "never resolved here", not "missing". The badge is advisory; transactional writes still enforce everything. Status is text plus an icon, not colour alone. This ticket covers Movies & TV; later per-type tickets extend the same query to their own identifiers.

**Blocked by:** 03

**Status:** needs-review

- [x] A result whose item has an open Request shows "Requested" and the status, and stays in the list (verified: store integration test plus Playwright search-status.spec.ts; the API wiring is tested with a fake lookup)
- [x] A result whose item is in an enabled source's latest complete snapshot shows "In library" even with no catalog row (verified: store integration test on real PostgreSQL, no catalog row)
- [x] A movie and a TV show with the same numeric ID are not confused (verified: store integration tests)
- [x] A source whose latest sync failed, or that is disabled, does not produce "In library" (verified: store integration test (disabled source, item dropped from the latest snapshot, source with no completed snapshot))
- [x] A never-seen item has no local state and no badge (verified: store, handler and Playwright tests)
- [x] The whole page is annotated with one query, with no provider or library calls (store integration test) (verified: store integration test asserts a single pool acquisition for 50 keys; local tables only)
- [ ] Clicking an already-requested result opens the modal with Notify Me, View request and Add to request (Playwright with a mocked /resolve shows all three actions; rests on a mock, left unticked)
- [x] Badges are readable without colour (browser test) (verified: Playwright with all colour stripped, aria-hidden icon, axe)
- [x] `make fmt test vet web-check`, `make test-integration` and the browser tests pass (verified: all pass; integration on a fresh DB; web-test only known failures (plus 4173-hardcoded asserts when run on another port))
