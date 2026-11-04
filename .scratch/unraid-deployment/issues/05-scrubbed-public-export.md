# 05: The scrubbed public export

**What to build:** One command turns this private repository into a scrubbed public copy ready for GitHub, with the full commit history preserved, and refuses to publish if any trace of the private domain remains (D034). It also fixes the in-app link to the setup guide, which points at a wrong address, so it points at the public repository.

**Blocked by:** None (can start immediately)

**Status:** ready-for-agent

- [x] The export script takes a release tag and a destination, works on a fresh clone, and never modifies the source repository
- [x] It rewrites history with a history-filter tool using a rule set kept in the repository: the Go module path becomes `github.com/evilperson1337/requestarr` in every import and in the module file, every other private-domain string is replaced, and every author and committer is mapped to a public noreply identity
- [x] The rewrite is deterministic: a second run over the same input produces identical commit hashes
- [x] A gate scans every ref, commit message, author and committer, path and blob (binary files included) for the private domain, case-insensitively, and the script exits non-zero without pushing if anything is found
- [x] Pushing to the public repository is a separate, explicit step from exporting
- [x] A flag selects the fallback of one scrubbed snapshot commit per release instead of the full history
- [x] A fixture-repository test seeded with the domain in a file body, a file name, a commit message, an author and a binary file proves the output is clean and stable, and that the gate fails when a string is deliberately left behind; it skips with a clear message if the history-filter tool is absent (seam 4 of the spec)
- [x] Run against this repository, the export passes the gate, and the exported copy builds and passes its Go and web checks on its own
- [x] The setup wizard's guide link points at the public repository's setup guide
- [ ] `scripts/check.sh` passes
