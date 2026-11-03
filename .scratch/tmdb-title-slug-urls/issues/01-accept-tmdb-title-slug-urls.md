# 01: Accept TMDB movie and TV links with title suffixes

**What to build:** Authenticated users can paste movie or TV links copied directly from TMDB, including the readable title suffix, and resolve the same canonical catalog item as the numeric-only link. Keep this a focused parsing fix with end-to-end regression coverage; no provider credential changes or broad error-message redesign.

**Blocked by:** None (can start immediately).

**Status:** done

## Confirmed reproduction

Paste this movie link into the request-entry flow:

https://www.themoviedb.org/movie/1762506-band-of-brothers-legacy

Observed user-facing result: "metadata URL could not be resolved". Reported request identifier: 94b0249a5600b6bbaa0ae1894453a7ff. No matching structured log record was found during diagnosis.

A controlled probe using the real TMDB resolver and a deterministic successful provider-response fixture reproduced:

- Title-suffixed link: rejected with "invalid TMDB ID" before making any provider request.
- Numeric-only link: resolved canonical TMDB ID 1762506 and made one provider request against the same fixture.

The resolver currently treats the entire ID-and-title segment as the ID, then rejects it using numeric-only validation. This parsing failure is independent of any provider authentication failure.

## Acceptance criteria

- [x] The reported movie URL resolves successfully in the request-entry flow with a valid deterministic provider fixture.
- [x] Movie and TV links accept both numeric-only IDs and IDs followed by a hyphen-separated readable title suffix, including multi-word suffixes.
- [x] Extract only the numeric ID for the provider request; never send the title suffix as part of the API ID.
- [x] Numeric-only and title-suffixed forms identify the same canonical catalog item and therefore share one Request rather than creating URL-dependent duplicates.
- [x] Preserve the movie/TV media-type distinction, supported-host restrictions, existing numeric-ID bounds, bounded outbound requests, and credential secrecy.
- [x] Empty IDs, nonnumeric ID prefixes, overlong numeric IDs, and unsupported URL forms remain rejected before outbound requests. Apply ID-length bounds to the extracted numeric ID, not the readable suffix.
- [x] Add deterministic resolver regressions for movie and TV forms, malformed-ID failure cases, and a regression through the metadata-resolution API boundary used by request entry. Tests must not depend on live TMDB data or real credentials.
- [x] Run the repository's required formatting, Go tests, vet, and frontend checks when dependencies are installed.

## Workaround and scope

Until fixed, remove the readable title suffix and use:

https://www.themoviedb.org/movie/1762506

This bypasses the parsing defect only. Live lookup still depends on valid TMDB credentials and provider availability; the fixture-based reproduction does not establish either. Do not rotate credentials, reset saved state, or restart the stack as part of merely reproducing this bug.

## Completion

Implemented numeric-ID extraction before title suffixes while preserving the
numeric-only validation and rejecting an empty suffix. Movie/TV fixtures and
resolver tests cover canonical identity, numeric-only compatibility, outbound
API paths, malformed IDs, and unsupported hosts. API regressions run the real
metadata service and resolver for viewer/requester/admin sessions, proving both
URL forms return one canonical catalog ID without exposing provider credentials.
The user guide now documents that copied TMDB title suffixes need not be removed.

TDD evidence: the exact movie-link regression first failed with "invalid TMDB
ID", then passed after numeric-ID extraction. The empty-suffix regression then
caught an outbound request for a malformed URL and passed after adding the
empty-suffix guard.

Validation passed: required formatting, full Go tests, vet, frontend typechecks,
targeted metadata/API race tests, and diff whitespace checks. No live provider
requests, stack restarts, credential changes, database changes, or commits were
made. Rebuild/redeploy the application separately to activate the fix in a
running container.
