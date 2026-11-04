#!/usr/bin/env bash
# Push a scrubbed export to the public repository (D034). This is a separate,
# explicit step from exporting: it re-runs the gate and refuses to push if
# anything is found. Usage: scripts/publish-public.sh <export-directory> <remote-url>
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
dir="${1:?usage: publish-public.sh <export-directory> <remote-url>}"
remote="${2:?usage: publish-public.sh <export-directory> <remote-url>}"
"$here/export-public-gate.sh" "$dir" || exit 1
git -C "$dir" push "$remote" refs/heads/main:refs/heads/main 'refs/tags/*:refs/tags/*'
