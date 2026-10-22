# 04: AGPL-3.0 license and real module path

**What to build:** The repo carries AGPL-3.0 and the Go module path matches the real remote, so the project can be released and imported. This is one mechanical change across the whole tree, landed in a single green step.

**Blocked by:** 03 Rename default branch to main

**Status:** done

- [x] LICENSE.md contains AGPL-3.0 and the pending notice is gone (D013)
- [x] The module path is github.com/evilperson1337/requestarr and every import is updated
- [x] make fmt test vet build and web-check pass
- [x] References in README, CONTRIBUTING and docs/RELEASES.md reflect the licence decision
