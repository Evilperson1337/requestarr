# Audiobook media type and ASIN parsing

Status: ready-for-agent
Blocked by:

Add `audiobook` to `internal/domain/media.go` (square artwork) and a pure ASIN/Audible-URL parser.

- `ParseASIN(string)`: trim, upper-case, accept `B` + 9 alphanumerics or a 10-char ISBN-10-like value (research §3); reject anything else.
- `ParseAudibleURL(*url.URL)`: extract the ASIN from `audible.<tld>/pd/.../<ASIN>` and return the marketplace region from the TLD (us, ca, uk, au, fr, de, jp, it, in, es). Unknown TLD is rejected.
- Wire the type through request creation, OpenAPI enums and the web type unions without changing behaviour for other types. Update `GLOSSARY.md` if a term is added.
- Table tests including malformed, lower-case, trailing-slash and tracking-parameter URLs.

Acceptance: `audiobook` round-trips through the domain, store and OpenAPI; parsers are covered by table tests.
