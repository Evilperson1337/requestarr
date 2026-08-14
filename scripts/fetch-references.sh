#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
out="$root/references/source/checkouts"
mkdir -p "$out"

clone_ref() {
  local id="$1" url="$2" branch="$3"
  local dest="$out/$id"
  if [[ -d "$dest/.git" ]]; then
    echo "[$id] already exists; fetching latest $branch"
    git -C "$dest" fetch --depth 1 origin "$branch"
    git -C "$dest" checkout -q "$branch"
    git -C "$dest" reset --hard "origin/$branch"
  else
    echo "[$id] cloning $url ($branch)"
    git clone --depth 1 --single-branch --branch "$branch" "$url" "$dest"
  fi
  printf '%-14s %s\n' "$id" "$(git -C "$dest" rev-parse HEAD)"
}

clone_ref seerr https://github.com/seerr-team/seerr.git develop
clone_ref radarr https://github.com/Radarr/Radarr.git develop
clone_ref sonarr https://github.com/Sonarr/Sonarr.git develop
clone_ref lidarr https://github.com/Lidarr/Lidarr.git develop
clone_ref ggrequestz https://github.com/XTREEMMAK/ggrequestz.git main
clone_ref romm https://github.com/rommapp/romm.git master
clone_ref apprise https://github.com/caronc/apprise.git master
clone_ref apprise-api https://github.com/caronc/apprise-api.git master

echo "Reference checkouts are ready under $out"
