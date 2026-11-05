#!/usr/bin/env bash
# Static checks on the release pipeline (D023, D034). The real publish, signing
# and attestation only run on GitHub for a pushed tag; this fails the build when
# the configuration drifts from the policy. It never touches the network.
set -euo pipefail
cd "$(dirname "$0")/.."
rel=.github/workflows/release.yml
status=0
fail() { echo "FAIL: $*" >&2; status=1; }
has() { grep -Eq -- "$2" "$1" || fail "$1: missing $3"; }

# Triggers: only a pushed v* tag.
has "$rel" "^on:" "a trigger block"
has "$rel" "tags: \['v\*'\]" "the v* tag trigger"
if sed -n '/^on:/,/^permissions:/p' "$rel" | grep -Eq 'branches|pull_request|workflow_dispatch|schedule'; then
  fail "$rel: triggers beyond tags"
fi

# No other workflow may log in to or push to a registry.
for wf in .github/workflows/*.yml; do
  [[ "$wf" == "$rel" ]] && continue
  if grep -Eq 'docker/login-action|push: *true|ghcr.io|cosign' "$wf"; then
    fail "$wf: registry login, push or signing outside the release workflow"
  fi
done

# Image: name, platforms, repository Dockerfile.
has "$rel" "ghcr.io/evilperson1337/requestarr" "the image name"
has "$rel" "platforms: linux/amd64,linux/arm64" "both platforms"
has "$rel" "file: \./Dockerfile" "the repository Dockerfile"
# docker/metadata-action drops a literal v from a semver pattern (the first release was published
# as 0.1.0-rc.1, not v0.1.0-rc.1), so the tags are the git tag itself plus a regex for vX.Y.
has "$rel" "type=ref,event=tag" "the exact git tag, with its v, as the vX.Y.Z tag"
if grep -Eq "type=semver" "$rel"; then fail "$rel: type=semver drops the v prefix; use type=ref and type=match"; fi
pattern="$(sed -n 's/^ *type=match,pattern=\(.*\),group=1$/\1/p' "$rel")"
if [[ -z "$pattern" ]]; then
  fail "$rel: missing the vX.Y match rule"
else
  python3 -I - "$pattern" <<'PY' || fail "$rel: the vX.Y match rule gives the wrong tags"
import re, sys
rule = re.compile(sys.argv[1])
def short(tag):
    found = rule.search(tag)
    return found.group(1) if found else None
cases = {"v0.1.0": "v0.1", "v1.12.3": "v1.12", "v0.1.0-rc.1": None, "v0.1.0-beta.2": None}
wrong = {tag: short(tag) for tag, want in cases.items() if short(tag) != want}
if wrong:
    sys.exit("vX.Y rule gave %r" % wrong)
PY
fi
has "$rel" "latest=false" "latest disabled by default"
has "$rel" "value=latest,enable=.*!contains\(github\.ref_name, *'-'\)" "latest only for stable tags"

# Supply chain.
has "$rel" "sbom: true" "an image SBOM"
has "$rel" "provenance: mode=max" "image provenance"
has "$rel" "sigstore/cosign-installer" "cosign"
has "$rel" "cosign sign --yes" "keyless signing"
has "$rel" "actions/attest-build-provenance" "build-provenance attestation"
has "$rel" "id-token: write" "OIDC token permission"
has "$rel" "attestations: write" "attestations permission"
has "$rel" "packages: write" "packages permission"

# Dockerfile: non-root, health check kept (ticket 02), cross-compiled.
has Dockerfile "USER nonroot" "a non-root user"
has Dockerfile '"/requestarr", "healthcheck"' "the healthcheck subcommand"
has Dockerfile 'GOARCH=\$TARGETARCH' "cross-compilation for the target architecture"

# GoReleaser: archives get an SPDX SBOM.
has .goreleaser.yaml "^sboms:" "an sboms section"
has .goreleaser.yaml "spdx" "SPDX output"

[[ $status == 0 ]] && echo "ok: release configuration matches D023"
exit $status
