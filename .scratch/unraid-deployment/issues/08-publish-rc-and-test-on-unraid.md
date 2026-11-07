# 08: Publish the pre-release and test it on a real Unraid server

**What to build:** `v0.1.0-rc.1` is published to GitHub and passes the real-server checklist on the maintainer's Unraid server. This is the first time the release workflow, the export and the template run for real, so expect to fix what they expose. The steps only the maintainer can take are packaged as a guided script with `/wizard`.

**Blocked by:** 03, 05, 06, 07

**Status:** resolved

- [ ] A guided script walks through creating the public GitHub repository (no mirror settings, no issues or pull requests needed), adding the push credential, running the export and pushing it with the `v0.1.0-rc.1` tag
- [ ] The release workflow runs on that tag; the image appears at `ghcr.io/evilperson1337/requestarr:v0.1.0-rc.1` for both architectures, with an SBOM, a provenance attestation and a signature that verify
- [ ] `latest` has not moved (it is a pre-release)
- [ ] Whatever the first real run exposes in the workflow, the export or the template is fixed, each as its own commit
- [ ] The manual checklist passes on the maintainer's Unraid server using the compose file pinned to the rc, and again using the template installed from the Template Repositories URL
- [ ] The results, including anything surprising, are appended to this ticket under `## Comments`

## Comments

### 2026-11-05: v0.1.0-rc.3

- Image: `ghcr.io/evilperson1337/requestarr:v0.1.0-rc.3` (digest sha256:b0738482544554b10970d1890f03b2bcc4dc6fdfeabd6c8150b70bcea88e75d5)
- Release workflow: https://github.com/Evilperson1337/requestarr/actions/runs/42145497282
- Check: platforms: linux/amd64 and linux/arm64
- Check: tag latest not moved by the pre-release
- Check: tag v0.1 not moved by the pre-release
- Check: provenance attestation verifies
- Check: cosign signature: not checked (cosign not installed). Checked 2026-11-06: `cosign verify` with the release workflow identity for the rc.3 tag and the GitHub OIDC issuer passes, for digest sha256:b0738482544554b10970d1890f03b2bcc4dc6fdfeabd6c8150b70bcea88e75d5
- Unraid checklist:
  - Compose pass A: container healthy and first admin signs in: **pass**
  - Install from the Template Repositories URL: **pass**
  - Healthy container: **pass**
  - Bad configuration reported: **pass**
  - First-admin sign-in through Keycloak: **pass**
  - Library over a private http:// address: **pass**
  - Master key separation: **pass**

### 2026-11-06: v0.1.0-rc.4

- Image: `ghcr.io/evilperson1337/requestarr:v0.1.0-rc.4` (digest sha256:8e263efa4630ce60a79f1c2b76d703525c6c78dafb582824273cce9e4820c2be)
- Release workflow: https://github.com/Evilperson1337/requestarr/actions/runs/42319751356
- Check: platforms: linux/amd64 and linux/arm64
- Check: tag latest not moved by the pre-release
- Check: tag v0.1 not moved by the pre-release
- Check: provenance attestation verifies
- Check: cosign signature: not checked (cosign not installed)
- Unraid checklist:
  - Compose pass A: container healthy and the local administrator signs in: **pass**
  - Install from the Template Repositories URL: **pass**
  - Healthy container: **pass**
  - Bad configuration reported: **pass**
  - Local administrator sign-in and OIDC set up in Settings: **pass**
  - Library over a private http:// address: **pass**
  - Master key separation: **pass**
