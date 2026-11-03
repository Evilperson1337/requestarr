#!/usr/bin/env bash
# Records the working tree, untracked files included, as a commit under
# refs/agent-snapshots/ without touching the index, the tree or the stash.
# Run at the start of an agent session (a Claude Code SessionStart hook) so
# script-driven edits, which bypass editor history and file checkpoints, can
# always be undone. Restore one file with:
#   git show refs/agent-snapshots/<stamp>:<path> > <path>
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
tmp_index="$(mktemp)"
trap 'rm -f "$tmp_index"' EXIT
export GIT_INDEX_FILE="$tmp_index"
git read-tree HEAD
git add -A
tree="$(git write-tree)"
if [[ "$tree" == "$(git rev-parse 'HEAD^{tree}')" ]]; then
	exit 0 # clean tree: HEAD already is the snapshot
fi
stamp="$(date -u +%Y%m%dT%H%M%SZ)"
commit="$(git commit-tree "$tree" -p HEAD -m "agent snapshot $stamp")"
git update-ref "refs/agent-snapshots/$stamp" "$commit"
echo "{\"systemMessage\": \"Working tree snapshot: refs/agent-snapshots/$stamp\"}"
