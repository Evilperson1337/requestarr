# Definition of done / QA acceptance

## Product fidelity
- [ ] All currently working routes and flows remain functional.
- [ ] Only supported app categories: movies, TV, music, games, audiobooks.
- [ ] No acquisition, downloading, Radarr or Sonarr integration added by redesign.
- [ ] Search and request workflow preserves exact current backend semantics.
- [ ] OIDC/auth/authorization rules unchanged except explicitly reviewed improvements.
- [ ] Requests and Issues actions permission-gated and reflect real statuses.
- [ ] Integration sync/configuration states fetched from real backend.
- [ ] Logs nav exists; Logs screen shows only supported/sanitized data.

## Visual
- [ ] Dark-only SignalLuxe theme with navy surfaces, soft pink/violet highlights, restrained blue/cyan accents.
- [ ] Consistent shell/nav on all screens, including Notifications and Logs.
- [ ] Artwork format and typography preserved appropriately by category.
- [ ] Header/search/filter/action hierarchy reads clearly on desktop.
- [ ] Readable background overlay and no glowing borders on every nested panel.
- [ ] Empty states/loading/error states visually complete.

## Interaction and quality
- [ ] Full keyboard navigation + visible focus + status descriptions.
- [ ] Mobile 360px and tablet 768px: no horizontal overflow or lost functionality.
- [ ] Screen reader testing for page headers/forms/tabs/dialogs.
- [ ] Reduced motion setting respected.
- [ ] No broken images; lazy loading does not cause layout shifts.
- [ ] Existing tests pass; add unit/component tests for nav, request actions, filter state, logs permissions.
- [ ] Screenshot comparison at consistent viewport with reference mockups, with intentional differences documented.
- [ ] No hardcoded fake counts, dates, media or logs from concepts.
