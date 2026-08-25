# Requestarr cumulative Audiobook V1 + Chaptarr update

This update is generated against the same Requestarr repository snapshot supplied on 2026-08-17. It **includes the complete prior Audiobook V1 / discovery decision update** plus the Chaptarr-derived refinements agreed on 2026-08-19. You do not need to apply the older update archive first.

## Included decisions

- Audiobooks are V1; ebooks remain V2.
- Audible product URLs / ASIN + storefront region are the initial audiobook identity path.
- Audnexus is the preferred V1 audiobook metadata/enrichment adapter.
- Audiobookshelf is the sole V1 audiobook availability authority.
- Shared requests, one request per item, requester auto-subscription, and `Notify Me` remain unchanged.
- OIDC/Keycloak remains mandatory V1 authentication.
- Apprise remains the external notification gateway.
- Seerr is the primary long-term discovery/request UX reference; Libreseerr adapts those ideas to book/audiobook presentation.
- Shelfmark, Listenarr, Readarr, and now Chaptarr are audiobook/book domain references.
- Chaptarr is **not** a runtime integration or acquisition dependency.

## Chaptarr-derived refinements

1. Audiobook identity is explicitly manifestation/edition aware: ASIN, region, narrator(s), publisher, language, runtime, release date, abridged state, edition/presentation type, and alternate stable identifiers can be significant.
2. Reconciliation records match provenance and confidence (`exact`, `high`, `possible`).
3. Only `exact` and `high` confidence matches may automatically transition a request to `available`; `possible` matches require review.
4. Audiobook title+author alone is insufficient for automatic fulfillment when narrator/production identity is ambiguous.
5. Metadata provider seams should permit future fallback/enrichment providers without implementing multi-provider consensus in V1.
6. V1 remains compatible with a future `work -> manifestation` relationship for ebook V2 without prematurely implementing the ebook/work hierarchy.

## Database migrations

This cumulative pack adds forward-only migrations:

- `000007_audiobook_v1` — adds `audiobook` catalog media type and `audiobookshelf` library source kind.
- `000008_reconciliation_match_evidence` — adds cross-media request availability match provenance/confidence.

Do not edit already-applied migrations.

## Apply

Extract this ZIP into the **root of the current Requestarr repository**, then run one of:

```bash
./apply-requestarr-update.sh
```

```powershell
./apply-requestarr-update.ps1
```

The script runs `git apply --check` first. If any baseline target has diverged, it refuses to apply anything and directs you to the manual merge copies.

## Review and validate

After application:

```bash
git diff --stat
git diff
make test
make vet
make build
```

Also run migration/integration checks against PostgreSQL and the normal frontend checks documented by the repository.

## Reference source

Run the updated reference fetcher when network access is available:

```bash
./scripts/fetch-references.sh
```

or:

```powershell
./scripts/fetch-references.ps1
```

This now includes Chaptarr along with Seerr, Libreseerr, Shelfmark, Listenarr, Readarr, Radarr, Sonarr, Lidarr, GG Requestz, RomM, Audnexus, Audiobookshelf, Apprise, and Apprise API.
