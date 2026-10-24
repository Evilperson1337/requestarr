# 28: Release docs and final acceptance

**What to build:** Upgrade and backup notes, supply-chain and container policy are written, and a full acceptance run confirms V1 is complete.

**Blocked by:** All of 01 to 27

**Status:** done (accepted with explicit gaps; ticket 26 still needs its manual pass, see docs/research/PROJECT_STATUS.md section 4)

- [x] Upgrade and backup notes exist and reflect migrations
- [x] SBOM and container publishing policy written per docs/RELEASES.md (proposed as D023, awaiting maintainer approval)
- [x] make fmt test vet web-check, the integration suite, the Keycloak script and the frontend smoke tests all pass
- [x] docs/IMPLEMENTATION_PLAN.md and docs/research/PROJECT_STATUS.md are updated against docs/V1_SCOPE.md
- [x] Any remaining gaps are listed explicitly
