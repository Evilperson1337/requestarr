# 08: Publish the pre-release and test it on a real Unraid server

**What to build:** `v0.1.0-rc.1` is published to GitHub and passes the real-server checklist on the maintainer's Unraid server. This is the first time the release workflow, the export and the template run for real, so expect to fix what they expose. The steps only the maintainer can take are packaged as a guided script with `/wizard`.

**Blocked by:** 03, 05, 06, 07

**Status:** ready-for-human

- [ ] A guided script walks through creating the public GitHub repository (no mirror settings, no issues or pull requests needed), adding the push credential, running the export and pushing it with the `v0.1.0-rc.1` tag
- [ ] The release workflow runs on that tag; the image appears at `ghcr.io/evilperson1337/requestarr:v0.1.0-rc.1` for both architectures, with an SBOM, a provenance attestation and a signature that verify
- [ ] `latest` has not moved (it is a pre-release)
- [ ] Whatever the first real run exposes in the workflow, the export or the template is fixed, each as its own commit
- [ ] The manual checklist passes on the maintainer's Unraid server using the compose file pinned to the rc, and again using the template installed from the Template Repositories URL
- [ ] The results, including anything surprising, are appended to this ticket under `## Comments`
