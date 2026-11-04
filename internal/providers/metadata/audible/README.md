# Audible metadata provider

Resolves an ASIN or Audible product link (Audnexus first, Audible catalog
second, placeholder last) and searches audiobooks by name (D031, D032).

## The catalog endpoint is undocumented

`GET https://api.<marketplace host>/1.0/catalog/products/` is not a published
or contractual API. Audnexus has no title search, so name search depends on it.
Treat it as able to change or throttle without notice:

- one attempt per search, bounded by the shared fetch budget and the search
  service's 10 s call budget; answers are cached in memory (75 s fresh, up to
  6 h stale when the endpoint is failing);
- a non-200 answer, undecodable JSON, or a JSON object with no `products`
  list is an error, never an empty page. Only `"products": []` means no matches;
- the marketplace is the admin's default (`REQUESTARR_AUDIBLE_REGION`, `us`
  when unset); there is no per-request region.

## Observed behavior (live probes, checked 2026-11-04, marketplace `us`)

Request used: `?keywords=hail+mary&num_results=5&response_groups=media,contributors,product_attrs,series&image_sizes=500`
(plus `products_sort_by=Relevance` and `page`), no authentication, plain User-Agent.

- HTTP 200 with `{total_results, products[], response_groups, product_filters}`.
  `total_results` was 124 for "hail mary".
- `page` is 0-based (page 0 and page 1 returned different first rows);
  `num_results=25` was accepted. `num_results=500` returned HTTP 400.
- A keyword with no matches returned HTTP 200 with `products: []`.
- Each product: `asin`, `title`, `subtitle` (nullable), `authors[].name`,
  `narrators[].name`, `runtime_length_min`, `format_type` (`unabridged`,
  `abridged`, `original_recording`), `content_delivery_type` (`SinglePartBook`,
  `MultiPartBook`, `PodcastParent`), `has_children`, `content_type`
  (`Product`, `Podcast`), `release_date`, `product_images["500"]`, `series[]`.
- The "ASIN" can be a ten-digit ISBN-style id (for example `8418037121`);
  `domain.ParseASIN` accepts those.
- Responses are large (immersive-reel assets etc.), so the fixture
  `testdata/search.json` keeps only the fields the mapper reads.

## Mapping decisions

- Podcast rows (`content_type` other than `Product`) and rows without a valid
  ASIN are dropped.
- Two editions of one title are two ASINs and so two rows (D031).
- "Series / multi-part" is shown when `content_delivery_type` is
  `MultiPartBook` or `has_children` is true. Audible marks many ordinary
  single-title books this way (for example Project Hail Mary), so the label
  means "Audible lists this as a parent with parts", and the row is never hidden.
- Unknown: whether Audible rate-limits heavy use; whether the endpoint requires
  anything beyond a User-Agent from other networks. Not probed beyond a handful
  of polite calls.
