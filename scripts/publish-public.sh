#!/usr/bin/env bash
# Push a scrubbed export to the public repository (D034). This is a separate,
# explicit step from exporting: it re-runs the gate and refuses to push if
# anything is found.
#
# Usage: scripts/publish-public.sh <export-directory> <remote-url> [release-tag]
#
# GitHub creates no workflow events for a tag pushed in the same push as a
# branch (or with more than two other tags), so a release never starts. Every
# ref is therefore its own push: main first, then the tags, then the release
# tag last. With a release tag, other version tags (v*) are not published: each
# new one would start a release of its own.
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
dir="${1:?usage: publish-public.sh <export-directory> <remote-url> [release-tag]}"
remote="${2:?usage: publish-public.sh <export-directory> <remote-url> [release-tag]}"
release="${3:-}"
"$here/export-public-gate.sh" "$dir" || exit 1

git -C "$dir" push "$remote" refs/heads/main:refs/heads/main
while IFS= read -r tag; do
  [[ "$tag" == "$release" ]] && continue
  if [[ -n "$release" && "$tag" == v* ]]; then
    echo "publish: not pushing version tag $tag (it would start its own release); only $release is released" >&2
    continue
  fi
  git -C "$dir" push "$remote" "refs/tags/$tag:refs/tags/$tag"
done < <(git -C "$dir" for-each-ref --format='%(refname:short)' refs/tags)
if [[ -n "$release" ]]; then
  git -C "$dir" rev-parse -q --verify "refs/tags/$release" >/dev/null || { echo "publish: no tag $release in $dir" >&2; exit 1; }
  git -C "$dir" push "$remote" "refs/tags/$release:refs/tags/$release"
fi
