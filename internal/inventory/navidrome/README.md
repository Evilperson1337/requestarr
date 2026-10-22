# Navidrome/OpenSubsonic inventory adapter

The adapter implements `inventory.Source` for Navidrome and is authoritative for
albums and songs only. It enumerates visible music folders and paginated albums,
then reads each album's tracks. Album and song MusicBrainz IDs are normalized to
lowercase `musicbrainz` identifiers when present; title and year remain available when
an ID is absent. The adapter reads only and never changes Request status.

Configure `REQUESTARR_NAVIDROME_URL`, `REQUESTARR_NAVIDROME_USERNAME`, and
`REQUESTARR_NAVIDROME_PASSWORD` together. Use a dedicated, read-only Navidrome
account and HTTPS outside development. OpenSubsonic token authentication derives
a salted request token; the password itself is not sent. Navidrome credentials
are sent as standard API query parameters, so do not enable request-URL logging
at intermediaries. Responses are bounded and redirects are disabled.

Any failed, malformed, or incomplete enumeration returns an error without a
snapshot. This preserves last-known inventory and prevents outages from being
interpreted as removals. The shared `inventorytest` contract runs against the
sanitized fixtures in `testdata/`.
