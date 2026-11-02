# Audiobook metadata resolver (Audnexus, then Audible)

Status: ready-for-agent
Blocked by: 01

Resolve an ASIN to a Catalog item without a hard dependency on either upstream.

- New provider under `internal/providers/metadata/` behind the existing `Resolver` shape, plus a small additive entry point for a bare ASIN (the URL path starts at `ParseURL`; research §4).
- Audnexus `GET https://api.audnex.us/books/{ASIN}?region=`; on `REGION_UNAVAILABLE` or other failure fall back to Audible `https://api.audible.<tld>/1.0/catalog/products/{ASIN}` with the response groups listed in research §3. Never call Audnexus's DELETE endpoints.
- Catalog item: title, subtitle, authors, narrators, cover (square), abridged/unabridged, language, runtime, region in `Metadata`.
- Admin-configurable default region (setting, US default).
- If both upstreams fail, return an unresolved result (ASIN kept, placeholder title) and mark it for retry; reuse the cache and `ErrBusy` stale-serve in `Service`. Descriptive `User-Agent`, bounded response size, outbound timeout, no more than one lookup per new Request.
- httptest fixtures: success, region 404, both down, malformed JSON, oversized body.

Acceptance: a Request can be created for a valid ASIN with the upstreams up, partly down, and fully down.
