# 09: Stable v0.1.0 and the template moves to latest

**What to build:** The first stable release is tagged and the template defaults to `latest`, so Unraid's normal update flow works.

**Blocked by:** 08

**Status:** ready-for-human

- [ ] The remaining release-readiness gaps the maintainer considers blocking (see the project status document) are closed or consciously accepted, noted under `## Comments`
- [ ] `v0.1.0` is exported and pushed; the workflow publishes the image and moves `latest` to it
- [ ] The template's default tag becomes `latest`, the lint is updated so only a floating tag is allowed once a stable release exists, and the compose file's pinned tag moves to `v0.1.0`
- [ ] The guide and the release documentation no longer describe a pre-release-only state
- [ ] A fresh install from the template on the maintainer's Unraid server pulls `latest` and passes the checklist
- [ ] A Community Applications submission is recorded as the follow-up it is, not started here
