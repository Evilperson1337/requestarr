# 03: The release workflow builds the multi-arch, signed image

**What to build:** A pushed `v*` tag produces a published, verifiable container image on the GitHub container registry, as D023 describes: amd64 and arm64, an SBOM, a build-provenance attestation and a keyless cosign signature, with `latest` moving only on stable tags. The image applies migrations on start and runs as a non-root user.

**Blocked by:** 02 (the health check is part of the image)

**Status:** ready-for-human

- [ ] The release workflow builds and pushes `ghcr.io/evilperson1337/requestarr` for `linux/amd64` and `linux/arm64` from the repository Dockerfile
- [ ] Tags are `vX.Y.Z` and `vX.Y` for every release; `latest` is applied only to stable (non-pre-release) tags
- [ ] An SPDX SBOM, a provenance attestation and a keyless signature are produced for the image, and the release documentation says how to verify them
- [x] No image is built or published from a branch or a pull request
- [x] A local snapshot build (`make snapshot`) produces both architectures' images without publishing
- [x] The release documentation's D023 section and "remaining gaps" list reflect what is now implemented and what can only be confirmed after a real tag (publish, signing, attestation)
- [ ] `make release-check` and `scripts/check.sh` pass

Notes: criteria 1-3 are written in `release.yml` but only confirmable by a pushed tag (ticket 08), so they stay unticked. The last criterion is unticked because `scripts/check.sh` was not run in this parallel run (`make release-check` and `make fmt test vet` were).

## Comments

Code complete and checked locally: `make release-check` and the release-config check pass, and `make snapshot` builds both architectures. The three unticked criteria (publish, tag set, SBOM/provenance/signature) can only be confirmed when a real tag runs on GitHub; they are verified in ticket 08. Note for review: `vX.Y` is applied to stable tags only, so a pre-release gets only its exact tag.
