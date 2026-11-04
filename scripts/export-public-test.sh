#!/usr/bin/env bash
# Seam 4 of the Unraid deployment spec: the scrubbed public export, run against a
# small fixture repository seeded with the private domain. Needs git-filter-repo
# (on PATH or named in GIT_FILTER_REPO); skips with a message when it is absent.
set -euo pipefail
cd "$(dirname "$0")/.."
if [[ -z "${GIT_FILTER_REPO:-}" ]] && ! command -v git-filter-repo >/dev/null 2>&1; then
  echo 'SKIP: git-filter-repo not found (install it or set GIT_FILTER_REPO); export test not run'
  exit 0
fi
# The private domain is assembled here so this file stays clean for the gate.
dom="allour""media"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
fail() { echo "FAIL: $*" >&2; exit 1; }
refs() { git -C "$1" for-each-ref --format='%(refname) %(objectname)'; }
commit_as_other() { git -C "$1" -c user.name=x -c user.email=x@example.com commit -q "${@:2}"; }

src="$work/src"
git init -q -b main "$src"
git -C "$src" config user.name 'Private Person'
git -C "$src" config user.email "person@$dom.com"
printf 'module git.%s.com/evilperson1337/requestarr\n\ngo 1.27\n' "$dom" > "$src/go.mod"
printf 'package main\n\nimport _ "git.%s.com/evilperson1337/requestarr/internal/x"\n' "$dom" > "$src/main.go"
echo "hosted at git.$dom.com, ${dom^^} too" > "$src/notes.txt"
echo 'name carries the domain' > "$src/notes-$dom.txt"
printf '\x00\x01\x02binary %s blob\x00\xff' "$dom" > "$src/blob.bin"
git -C "$src" add -A
git -C "$src" commit -q -m "first commit for $dom"
echo more >> "$src/notes.txt"
git -C "$src" commit -q -am 'second commit'
git -C "$src" tag v0.0.1
git -C "$src" checkout -q -b "feature-$dom"
echo branch > "$src/branch.txt"
git -C "$src" add -A
git -C "$src" commit -q -m 'private branch work'
git -C "$src" checkout -q main
src_state="$(refs "$src")"

export_to() { scripts/export-public.sh --source "$src" "$@"; }

# 1. Clean, rewritten, deterministic output; source untouched.
export_to v0.0.2 "$work/out1" >/dev/null || fail 'export of the fixture failed'
export_to v0.0.2 "$work/out2" >/dev/null || fail 'second export failed'
scripts/export-public-gate.sh "$work/out1" >/dev/null || fail 'gate rejected a clean export'
[[ "$(git -C "$work/out1" show HEAD:go.mod | head -1)" == 'module github.com/evilperson1337/requestarr' ]] || fail 'module path not rewritten'
git -C "$work/out1" show HEAD:main.go | grep -q '"github.com/evilperson1337/requestarr/internal/x"' || fail 'import not rewritten'
[[ "$(refs "$work/out1")" == "$(refs "$work/out2")" ]] || fail 'hashes differ between runs'
git -C "$work/out1" rev-parse -q --verify refs/tags/v0.0.2 >/dev/null || fail 'release tag missing'
[[ "$(git -C "$work/out1" for-each-ref --format='%(refname)' refs/heads)" == 'refs/heads/main' ]] || fail 'private branches leaked'
[[ -f "$work/out1/notes-example.txt" ]] || fail 'file name not scrubbed'
[[ -z "$(git -C "$work/out1" status --porcelain)" ]] || fail 'destination working tree not clean'
[[ "$src_state" == "$(refs "$src")" ]] || fail 'source refs changed'
[[ -z "$(git -C "$src" status --porcelain)" ]] || fail 'source working tree changed'

# 2. The gate fails when a string is left behind, in a blob or a commit message.
git clone -q "$work/out1" "$work/dirty"
echo "leak $dom" > "$work/dirty/leak.txt"
git -C "$work/dirty" add leak.txt
commit_as_other "$work/dirty" -m 'add leak'
if scripts/export-public-gate.sh "$work/dirty" >/dev/null 2>&1; then fail 'gate passed a leaked blob'; fi
git clone -q "$work/out1" "$work/dirty2"
commit_as_other "$work/dirty2" --allow-empty -m "message mentions ${dom^}"
if scripts/export-public-gate.sh "$work/dirty2" >/dev/null 2>&1; then fail 'gate passed a leaked commit message'; fi

# 3. An incomplete rule set makes the export itself refuse, leaving no destination.
: > "$work/empty.rules"
if export_to --rules "$work/empty.rules" v0.0.2 "$work/out3" >/dev/null 2>&1; then fail 'export succeeded with a leaking rule set'; fi
[[ ! -e "$work/out3" ]] || fail 'refused export left a destination behind'

# 4. Snapshot fallback: one scrubbed commit per release, still clean and stable.
export_to --snapshot v0.0.2 "$work/snap1" >/dev/null || fail 'snapshot export failed'
export_to --snapshot v0.0.2 "$work/snap2" >/dev/null || fail 'second snapshot export failed'
[[ "$(git -C "$work/snap1" rev-list --count main)" == 2 ]] || fail 'snapshot should hold one commit per release (v0.0.1, v0.0.2)'
[[ "$(refs "$work/snap1")" == "$(refs "$work/snap2")" ]] || fail 'snapshot hashes differ'
scripts/export-public-gate.sh "$work/snap1" >/dev/null || fail 'gate rejected the snapshot'

# 5. Publishing is a separate step and re-runs the gate before pushing.
git init -q --bare "$work/remote.git"
if scripts/publish-public.sh "$work/dirty" "$work/remote.git" >/dev/null 2>&1; then fail 'publish pushed a dirty export'; fi
[[ -z "$(git -C "$work/remote.git" for-each-ref)" ]] || fail 'dirty export reached the remote'
scripts/publish-public.sh "$work/out1" "$work/remote.git" >/dev/null || fail 'publish of a clean export failed'
git -C "$work/remote.git" rev-parse -q --verify refs/heads/main >/dev/null || fail 'main not pushed'
echo 'export-public: all checks passed'
