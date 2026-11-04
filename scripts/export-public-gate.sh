#!/usr/bin/env bash
# The publish gate (D034): exit non-zero if any trace of the private domain
# remains in a git repository, case-insensitively, in any ref name, tag or commit
# message, author or committer, path, or blob (binary files included).
# Usage: scripts/export-public-gate.sh <repository>
set -euo pipefail
repo="${1:?usage: export-public-gate.sh <repository>}"
# Assembled from parts so this file does not itself contain the domain.
pattern="allour""media"
found=0
report() { echo "gate: private domain found in $1" >&2; found=1; }

git -C "$repo" rev-parse --git-dir >/dev/null 2>&1 || { echo "gate: $repo is not a git repository" >&2; exit 2; }

# Ref names, plus annotated-tag messages and taggers.
if git -C "$repo" for-each-ref --format='%(refname)%0a%(taggername)%0a%(taggeremail)%0a%(contents)' | grep -ai -- "$pattern" >/dev/null; then
  report 'a ref name or tag'
fi
# Every object in the database, decompressed: commit messages and identities,
# tree entries (paths) and blob contents, including unreachable ones.
if git -C "$repo" cat-file --batch-all-objects --batch | grep -ai -- "$pattern" >/dev/null; then
  report 'a commit, path or file (object database)'
fi
# Remotes and config can carry the domain too; the export is meant to have none.
if grep -ai -- "$pattern" >/dev/null "$(git -C "$repo" rev-parse --absolute-git-dir)/config" 2>/dev/null; then
  report 'the repository config'
fi
if ((found)); then
  echo 'gate: refusing; nothing was published' >&2
  exit 1
fi
echo 'gate: clean'
