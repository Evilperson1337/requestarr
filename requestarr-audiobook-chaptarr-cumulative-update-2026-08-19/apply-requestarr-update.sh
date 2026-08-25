#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
patch="$root/.requestarr-update/requestarr-update.patch"

if [[ ! -f "$root/go.mod" || ! -d "$root/internal" ]]; then
  echo "error: extract this archive into the Requestarr repository root, then run this script there" >&2
  exit 2
fi
if ! command -v git >/dev/null 2>&1; then
  echo "error: git is required for merge-safe application; use .requestarr-update/manual-merge if necessary" >&2
  exit 2
fi
cd "$root"
if git apply --check "$patch"; then
  git apply "$patch"
  echo "Requestarr cumulative audiobook + Chaptarr update applied successfully."
  echo "Review: git diff --stat && git diff"
  echo "Then follow APPLY_UPDATE.md and run the project's normal checks."
else
  cat >&2 <<'MSG'
The cumulative update was NOT applied because one or more target files have diverged from the baseline snapshot.
No project source was modified by this script.
Use .requestarr-update/requestarr-update.patch for a manual/three-way merge and
.requestarr-update/manual-merge/ to inspect the intended final files.
MSG
  exit 1
fi
