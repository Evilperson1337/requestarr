# Audible / Audnexus audiobook resolver

Implement behind `metadata.Resolver`. V1 input is an Audible product URL. Parse the ASIN and storefront region defensively, then resolve/enrich through Audnexus into `domain.ResolvedItem` with `MediaAudiobook` and `AudiobookDetails`.

Rules:
- Audnexus identifies metadata; it never establishes availability.
- Persist normalized catalog data so existing requests render while Audnexus is unavailable.
- Prefer ASIN + Audible region as edition identity; preserve narrator, author, series, runtime, language, publisher, and alternate IDs when present.
- Bound redirects, DNS/network access, response size, timeouts, and rate behavior using the same outbound-client security standards as other metadata adapters.
- Add fixtures for multiple Audible storefronts, malformed URLs, changed slugs, missing optional fields, and multiple narrators.
