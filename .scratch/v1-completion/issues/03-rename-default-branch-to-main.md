# 03: Rename default branch to main

**What to build:** The default branch is main, and CI, the release workflow and docs agree, so push-triggered CI actually runs. Touches the remote, so it needs the maintainer's go-ahead at the moment it runs.

**Blocked by:** None (can start immediately)

**Status:** in-progress (remote default switch + master deletion pending)

- [~] The default branch on the remote is main and local master is renamed
- [x] CI and release workflows trigger on main; a push runs CI
- [x] Docs referring to master or main are consistent
- [x] The maintainer explicitly approved the rename before it was executed
