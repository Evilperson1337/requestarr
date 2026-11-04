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
has "$rel" "type=semver,pattern=v\{\{version\}\}" "the vX.Y.Z tag rule"
has "$rel" "type=semver,pattern=v\{\{major\}\}\.\{\{minor\}\}" "the vX.Y tag rule"
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
