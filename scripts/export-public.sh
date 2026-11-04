#!/usr/bin/env bash
# Build the scrubbed public copy of this repository (D034). Works on a fresh
# clone, never touches the source repository, never pushes, and refuses (exit 1,
# no destination left behind) unless the publish gate finds no trace of the
# private domain. Pushing is the separate scripts/publish-public.sh.
#
# Usage: scripts/export-public.sh [--snapshot] [--rules FILE] [--source DIR] <release-tag> <destination>
#
#   --snapshot   one scrubbed commit per release tag instead of the full history
#   --rules      replace-text rule set (default: scripts/export-public.rules)
#   --source     repository to export (default: the repository holding this script)
#
# Needs git-filter-repo on PATH, or GIT_FILTER_REPO pointing at it.
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
source_repo="$(cd "$here/.." && pwd)"
rules="$here/export-public.rules"
snapshot=0
usage() { sed -n '2,/^set -e/p' "$0" | sed '$d;s/^# \{0,1\}//' >&2; exit 2; }

while (($#)); do
  case "$1" in
    --snapshot) snapshot=1 ;;
    --rules) rules="${2:?--rules needs a file}"; shift ;;
    --source) source_repo="${2:?--source needs a directory}"; shift ;;
    -h|--help) usage ;;
    --) shift; break ;;
    -*) echo "unknown option: $1" >&2; usage ;;
    *) break ;;
  esac
  shift
done
(($# == 2)) || usage
tag="$1"
dest="$2"

if [[ -n "${GIT_FILTER_REPO:-}" ]]; then
  filter_repo=("$GIT_FILTER_REPO")
elif command -v git-filter-repo >/dev/null 2>&1; then
  filter_repo=(git-filter-repo)
else
  echo 'export: git-filter-repo not found. Install it (for example in a virtual environment: pip install git-filter-repo) and put it on PATH, or set GIT_FILTER_REPO to its path.' >&2
  exit 2
fi
[[ -f "$rules" ]] || { echo "export: rule set not found: $rules" >&2; exit 2; }
git check-ref-format "refs/tags/$tag" || { echo "export: invalid release tag: $tag" >&2; exit 2; }
if [[ -e "$dest" ]] && [[ -n "$(ls -A "$dest" 2>/dev/null)" ]]; then
  echo "export: destination exists and is not empty: $dest" >&2
  exit 2
fi
head_sha="$(git -C "$source_repo" rev-parse --verify HEAD^{commit})"

# Build next to the destination so the final move is a rename, and discard
# everything on failure.
mkdir -p "$(dirname "$dest")"
build="$(mktemp -d "$(dirname "$dest")/.export-build.XXXXXX")"
trap 'rm -rf "$build"' EXIT
repo="$build/work/.git"
mkdir "$build/work"

# Fresh bare clone, then keep only main (the exported HEAD) and the tags.
git clone -q --bare --no-local "$source_repo" "$repo"
export GIT_DIR="$repo"
for ref in $(git for-each-ref --format='%(refname)' refs/heads refs/remotes refs/notes refs/stash 2>/dev/null); do
  git update-ref -d "$ref"
done
git update-ref refs/heads/main "$head_sha"
git symbolic-ref HEAD refs/heads/main
git rev-parse -q --verify "refs/tags/$tag" >/dev/null || git tag "$tag" "$head_sha"

# The replace-text rules cover blobs and commit messages; callbacks cover paths
# and identities. The pattern in the filename callback is a character class so
# this file does not match the gate.
public_name='Evilperson1337'
public_email='evilperson1337@users.noreply.github.com'
# git-filter-repo's --replace-text skips binary files, so the same rules are
# applied to every blob (binary included) by a callback built from the rule file.
blob_callback="$(python3 -I - "$rules" <<'PY'
import sys
rules = []
for line in open(sys.argv[1], encoding="utf-8"):
    line = line.rstrip("\n")
    if not line or line.startswith("#"):
        continue
    if not line.startswith("regex:") or "==>" not in line:
        sys.exit("export: unsupported rule (want regex:PATTERN==>REPLACEMENT): " + line)
    pat, rep = line[len("regex:"):].split("==>", 1)
    rules.append((pat.encode(), rep.encode()))
print("for pat, rep in %r:\n    blob.data = re.sub(pat, rep, blob.data)" % (rules,))
PY
)" || exit 1
identity="
commit.author_name = commit.committer_name = b'$public_name'
commit.author_email = commit.committer_email = b'$public_email'
"
(
  cd "$build"
  "${filter_repo[@]}" --force --quiet \
    --blob-callback "$blob_callback" \
    --replace-message "$rules" \
    --filename-callback "return re.sub(rb'(?i)allour[m]edia', b'example', filename)" \
    --commit-callback "$identity" \
    --tag-callback "tag.tagger_name = b'$public_name'
tag.tagger_email = b'$public_email'"
) || { echo 'export: git filter-repo failed' >&2; exit 1; }

if ((snapshot)); then
  # One commit per release tag, oldest first, with fixed identity and the date of
  # the release commit, so the result is stable.
  mapfile -t tags < <(git for-each-ref --format='%(refname:short)' 'refs/tags/v*' | sort -V)
  parent=''
  declare -A made=()
  for t in "${tags[@]}"; do
    when="$(git log -1 --format=%cI "$t^{commit}")"
    args=("$t^{tree}")
    [[ -n "$parent" ]] && args+=(-p "$parent")
    parent="$(GIT_AUTHOR_NAME=$public_name GIT_AUTHOR_EMAIL=$public_email GIT_AUTHOR_DATE=$when \
      GIT_COMMITTER_NAME=$public_name GIT_COMMITTER_EMAIL=$public_email GIT_COMMITTER_DATE=$when \
      git commit-tree "${args[@]}" -m "Release $t")"
    made[$t]="$parent"
  done
  [[ -n "$parent" ]] || { echo 'export: --snapshot found no release tags' >&2; exit 1; }
  for t in $(git for-each-ref --format='%(refname:short)' refs/tags); do git tag -d "$t" >/dev/null; done
  for t in "${tags[@]}"; do git tag "$t" "${made[$t]}"; done
  git update-ref refs/heads/main "$parent"
fi

git remote remove origin 2>/dev/null || true
git reflog expire --expire=now --all
git gc -q --prune=now

# The gate: no publishing (no destination) unless this passes.
"$here/export-public-gate.sh" "$repo" >&2 || { echo 'export: gate failed; no destination written' >&2; exit 1; }

# Give the destination a working tree.
git config core.bare false
unset GIT_DIR
git -C "$build/work" checkout -q -f main
if [[ -e "$dest" ]]; then rmdir "$dest"; fi
mv "$build/work" "$dest"
echo "export: wrote $dest ($(git -C "$dest" rev-parse --short main) on main, tag $tag)"
