#!/usr/bin/env bash
#
# Publish a release without prompts: the non-interactive twin of scripts/wizard-publish-release.sh.
#
#   scripts/publish-release.sh v0.1.0-rc.12
#
# It tags the current commit, exports the scrubbed history, runs the private-domain gate, pushes
# to the public GitHub repository (the tag last and alone, which starts release.yml), waits for
# the workflow, then checks the image anonymously as Unraid pulls it. Every check stops the run
# with a non-zero exit; nothing is forced and an existing tag is never moved. The one thing it
# cannot do is make a first-ever package public: the wizard walks through that once.

set -euo pipefail

GH_REPO="Evilperson1337/requestarr"
GH_OWNER="Evilperson1337"
IMAGE="ghcr.io/evilperson1337/requestarr"   # GHCR names are lower-case

die() { printf 'publish-release: %s\n' "$*" >&2; exit 1; }
say() { printf '%s\n' "$*"; }

[[ $# -eq 1 ]] || die "usage: scripts/publish-release.sh vX.Y.Z[-rc.N]"
RELEASE_TAG="$1"
[[ "$RELEASE_TAG" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$ ]] || die "a tag looks like v0.1.0 or v0.1.0-rc.1, not '$RELEASE_TAG'"

cd "$(dirname "${BASH_SOURCE[0]}")/.."
REPO_ROOT="$PWD"

# gh's login for one git command; no global git config is changed.
gh_git() {
  GIT_CONFIG_COUNT=2 \
    GIT_CONFIG_KEY_0=credential.helper GIT_CONFIG_VALUE_0= \
    GIT_CONFIG_KEY_1=credential.helper GIT_CONFIG_VALUE_1='!gh auth git-credential' \
    "$@"
}

# anon_manifest_status TAG prints the HTTP status of an anonymous manifest request, which is what
# Unraid's pull sees: 200 means the package is public and the tag exists.
anon_manifest_status() {
  local tag="$1" repo="${IMAGE#ghcr.io/}" token=""
  token=$(curl -fsS "https://ghcr.io/token?scope=repository:${repo}:pull" 2>/dev/null \
    | python3 -c 'import sys, json; print(json.load(sys.stdin).get("token", ""))' 2>/dev/null) || token=""
  curl -s -o /dev/null -w '%{http_code}' \
    -H "Authorization: Bearer $token" \
    -H 'Accept: application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.oci.image.manifest.v1+json' \
    "https://ghcr.io/v2/${repo}/manifests/${tag}" || true
}

say "== Preflight"
for t in git gh docker python3 curl git-filter-repo; do
  command -v "$t" >/dev/null 2>&1 || die "missing tool: $t"
done
scopes=$(gh auth status 2>&1 | grep -i 'token scopes' || true)
[[ "$scopes" == *"'repo'"* && "$scopes" == *"'workflow'"* ]] || die "gh needs the repo and workflow scopes: gh auth refresh -h github.com -s repo,workflow"
login=$(gh api user --jq .login 2>/dev/null || true)
[[ "${login,,}" == "${GH_OWNER,,}" ]] || die "gh is logged in as '${login:-nobody}', but the public repository belongs to $GH_OWNER"
gh repo view "$GH_REPO" >/dev/null 2>&1 || die "$GH_REPO does not exist; run scripts/wizard-publish-release.sh once to create it"

[[ "$(git branch --show-current)" == main ]] || die "release from main (on '$(git branch --show-current)')"
[[ -z "$(git status --porcelain --untracked-files=no)" ]] || die "uncommitted changes; only committed work is released"
git fetch -q origin main
[[ "$(git rev-parse HEAD)" == "$(git rev-parse origin/main)" ]] || die "HEAD is not origin/main; push main first"
pinned=$(sed -n 's:.*<Tag>\(.*\)</Tag>.*:\1:p' deploy/unraid/requestarr.xml | head -n1)
[[ "$pinned" == "$RELEASE_TAG" ]] || die "deploy/unraid/requestarr.xml is pinned to '$pinned', not $RELEASE_TAG (see 'Cutting a release' in docs/RELEASES.md)"
say "Release commit: $(git log -1 --format='%h %s')"

say "== Tag"
if git rev-parse -q --verify "refs/tags/$RELEASE_TAG" >/dev/null; then
  [[ "$(git rev-list -n1 "$RELEASE_TAG")" == "$(git rev-parse HEAD)" ]] || die "tag $RELEASE_TAG already exists at a different commit; it is never moved, use the next number"
  say "Tag $RELEASE_TAG already exists at this commit."
else
  git tag -a "$RELEASE_TAG" -m "Requestarr $RELEASE_TAG"
fi
git push -q origin "refs/tags/$RELEASE_TAG"

say "== Export (rewrites history and runs the private-domain gate)"
EXPORT_DIR="${TMPDIR:-/tmp}/requestarr-public-export-$RELEASE_TAG"
rm -rf -- "$EXPORT_DIR"   # a fixed prefix plus the validated tag
"$REPO_ROOT/scripts/export-public.sh" "$RELEASE_TAG" "$EXPORT_DIR" || die "the export or the gate failed; nothing has left this machine"

say "== Push to GitHub"
gh_git "$REPO_ROOT/scripts/publish-public.sh" "$EXPORT_DIR" "https://github.com/$GH_REPO.git" "$RELEASE_TAG"

find_run() {
  local waited=0 id=""
  while ((waited < $1)); do
    id=$(gh run list -R "$GH_REPO" --workflow release.yml --limit 5 --json databaseId,headBranch \
      --jq "map(select(.headBranch==\"$RELEASE_TAG\"))[0].databaseId // empty" 2>/dev/null || true)
    [[ -n "$id" ]] && { printf '%s' "$id"; return 0; }
    sleep 4; waited=$((waited + 4))
  done
  return 1
}

say "== Release workflow"
run_id=$(find_run 120) || run_id=""
if [[ -z "$run_id" ]]; then
  # A tag pushed in the same push as a branch can be dropped by GitHub; re-push it alone.
  say "No run after two minutes; re-pushing the tag alone."
  gh_git git -C "$EXPORT_DIR" push "https://github.com/$GH_REPO.git" ":refs/tags/$RELEASE_TAG"
  gh_git git -C "$EXPORT_DIR" push "https://github.com/$GH_REPO.git" "refs/tags/$RELEASE_TAG"
  run_id=$(find_run 90) || die "still no release run; check https://github.com/$GH_REPO/actions"
fi
say "Run: https://github.com/$GH_REPO/actions/runs/$run_id"
if ! gh run watch "$run_id" -R "$GH_REPO" --exit-status >/dev/null; then
  gh run view "$run_id" -R "$GH_REPO" --log-failed 2>/dev/null | tail -n 80 || true
  die "the release workflow failed; fix it on main and release the next number"
fi
say "The release workflow succeeded."

say "== Verify the published image"
failed=0

[[ "$(anon_manifest_status "$RELEASE_TAG")" == 200 ]] \
  && say "ok   anonymous pull works for $RELEASE_TAG" \
  || { say "FAIL anonymous pull of $IMAGE:$RELEASE_TAG did not return 200 (is the package public?)"; failed=1; }

inspect=$(docker buildx imagetools inspect "$IMAGE:$RELEASE_TAG" 2>&1 || true)
platforms=$(printf '%s\n' "$inspect" | awk '/Platform:/ {print $2}' | sort -u | tr '\n' ' ')
if [[ "$platforms" == *linux/amd64* && "$platforms" == *linux/arm64* ]]; then say "ok   platforms: $platforms"
else say "FAIL platforms: expected linux/amd64 and linux/arm64, found '${platforms:-none}'"; failed=1; fi
say "     digest: $(printf '%s\n' "$inspect" | awk '/^Digest:/ {print $2; exit}')"

short=$(printf '%s' "${RELEASE_TAG%%-*}" | cut -d. -f1,2)
for t in latest "$short"; do
  status=$(anon_manifest_status "$t")
  if [[ "$RELEASE_TAG" == *-* ]]; then
    # a pre-release must not move latest or vX.Y; they may exist from an older stable release
    say "note tag '$t' is HTTP $status (a pre-release does not move it)"
  elif [[ "$status" == 200 ]]; then say "ok   tag '$t' exists"
  else say "FAIL tag '$t' is missing (HTTP $status)"; failed=1; fi
done

if gh attestation verify "oci://$IMAGE:$RELEASE_TAG" --repo "$GH_REPO" >/dev/null 2>&1; then say "ok   build-provenance attestation verifies"
else say "FAIL build-provenance attestation did not verify"; failed=1; fi

if command -v cosign >/dev/null 2>&1; then
  if cosign verify "$IMAGE:$RELEASE_TAG" \
      --certificate-identity-regexp "^https://github.com/${GH_REPO,,}/\\.github/workflows/release\\.yml@refs/tags/v" \
      --certificate-oidc-issuer https://token.actions.githubusercontent.com >/dev/null 2>&1; then say "ok   cosign signature verifies"
  else say "FAIL cosign signature did not verify"; failed=1; fi
else
  say "skip cosign signature (cosign is not installed; see 'Verifying a release image' in docs/RELEASES.md)"
fi

((failed == 0)) || die "published, but at least one check failed (above)"
say ""
say "Released $RELEASE_TAG. On Unraid use $IMAGE:$RELEASE_TAG"
