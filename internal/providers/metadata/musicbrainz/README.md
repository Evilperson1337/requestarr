# MusicBrainz resolver

Implements `metadata.Resolver`. Albums are identified by release group and songs by recording; a pasted release link resolves to its release group (D020). Provider-specific DTOs stay here and are mapped into `domain.ResolvedItem` before returning. `Aliaser` looks up the release and release-track ids that library Sources expose for a catalog item, spaced to MusicBrainz's one-request-per-second rule. Enforce outbound HTTP timeouts, response-size limits, and provider-specific rate limits.

## Search ranking note (2026-11-04)

MusicBrainz holds many recordings and release groups with the same title (covers, live versions, tributes) and exposes no popularity signal, so a title-only search cannot reliably put the well-known item first. Live measurement: album "dark side of the moon" has Pink Floyd's at position 12 of 25; song "bohemian rhapsody" has Queen's recording nowhere on page 1. With the artist added, "pink floyd dark side of the moon" and "queen bohemian rhapsody" both rank the right item at position 1.

Decision (owner): keep the query unchanged and tell people to add the artist; the Albums and Songs hints on Home say so. Query-shape boosts (such as a phrase boost) were tried and rejected: they worsened the artist-plus-title cases without fixing the title-only headline ones. If title-only search proves too weak in practice, the proper fix is a separate Artist field, not further query tuning.

## Busy answers (2026-11-07)

MusicBrainz's search servers answer 503 "busy" to a fraction of requests even from a client well inside the rate limit (live: about one recording search in four, spaced 2 s apart). A name search (`searchFetch`) asks again up to three times when the answer is busy, each attempt waiting for its own slot in the spacer's search line, so the budget holds and previews still go first (D042). Resolves use `metadata.FetchRetrying`.
