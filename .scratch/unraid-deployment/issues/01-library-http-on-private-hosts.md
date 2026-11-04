# 01: Library Sources accept http on a private host

**What to build:** A library Source address on a private host works without https, so a home server can reach its own Plex, Jellyfin, Navidrome, RomM or Audiobookshelf by LAN address or Docker name (D033). The same rule applies whether the address is set in the environment or applied from Setup. Public DNS names still need https. The public URL, the OIDC issuer and the Apprise URL stay https-only outside development. See `GLOSSARY.md` for **Private host**.

**Blocked by:** None (can start immediately)

**Status:** done

- [x] Outside development, a Source URL over `http://` is accepted for a loopback, RFC 1918, link-local or CGNAT (100.64.0.0/10) IP literal, a single-label name, and a name ending `.local`, `.lan`, `.internal` or `.home.arpa`
- [x] Outside development, a Source URL over `http://` on any other host (a public DNS name or public IP) is rejected, and the message says what would make it acceptable (https, or adding the network to the allowed list)
- [x] A new environment setting takes a comma-separated list of CIDRs and hostname suffixes and only ever adds to the built-in list; a malformed entry is a startup problem that names the entry
- [x] The rule is one validator shared by the environment path and the Setup "apply" path; there is no second copy
- [x] The public URL, OIDC issuer and Apprise URL still reject `http://` outside development; development behaviour is unchanged
- [x] Table-driven tests on the configuration validation cover every case above (seam 1 of the spec)
- [x] The integrations documentation states the new rule and that an `http://` library connection sends its token in cleartext on the network
- [x] `scripts/check.sh` passes (run on the integration branch tip, with the PostgreSQL integration tests and Playwright)
