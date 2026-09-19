#!/bin/bash
# Audit and exercise the production HTTPS formula using a pinned public release.
# This is a read-only release fixture, not channel publication or signing.
set -euo pipefail
if [[ $# != 0 || "${GITHUB_ACTIONS:-}" != true || "${RUNNER_ENVIRONMENT:-}" != github-hosted || "$(uname -s)" != Darwin ]]; then
  echo "usage: $0 (disposable GitHub-hosted macOS only)" >&2
  exit 2
fi

# Independently verified against the exact tag's Sigstore identity/provenance.
# Updating this fixture requires a reviewed version AND checksum-manifest pin.
version=v0.1.0-beta.1
manifest_sha256=2da670111c37f7ad3249f790d9e1ba97cc700e9a7e38729fbc986e0251ab55ca
tap=nischoy-ai/tap
formula=$tap/topo-beta
if brew list --formula topo >/dev/null 2>&1 || brew list --formula topo-beta >/dev/null 2>&1 ||
   brew tap | grep -Fxq "$tap"; then
  echo "refusing to overwrite an existing Topo installation or tap" >&2
  exit 1
fi
fixture=$(mktemp -d "${RUNNER_TEMP:?}/topo-homebrew-promotion.XXXXXX")
created_tap=false
cleanup() {
  if [[ "$created_tap" == true ]]; then
    brew uninstall --formula "$formula" >/dev/null 2>&1 || true
    brew untap "$tap" >/dev/null 2>&1 || true
  fi
  rm -rf "$fixture"
}
trap cleanup EXIT
gh release download "$version" --repo Nischoy-ai/topo --dir "$fixture/artifacts"
(
  cd "$fixture/artifacts"
  printf '%s  SHA256SUMS\n' "$manifest_sha256" | shasum -a 256 --check -
  shasum -a 256 --check SHA256SUMS
)
go run ./internal/distributiontool -artifacts "$fixture/artifacts" \
  -out "$fixture/distribution" -version "$version" -channel beta \
  -published-at 2026-09-19T00:00:00Z
brew tap-new "$tap"
created_tap=true
tap_path=$(brew --repository "$tap")
install -m 0644 "$fixture/distribution/homebrew/Formula/topo-beta.rb" "$tap_path/Formula/topo-beta.rb"
brew audit --strict --online "$formula"
brew install --formula "$formula"
brew test "$formula"
test "$(topo version)" = "$version"
if [[ "$(uname -m)" == arm64 ]]; then
  codesign --verify --strict --verbose=2 "$(command -v topo)"
fi
brew uninstall --formula "$formula"
if brew list --formula "$formula" >/dev/null 2>&1; then
  echo "Homebrew uninstall left Topo installed" >&2
  exit 1
fi
echo "Public-release formula strict online audit, installation, discovery, and removal passed"
