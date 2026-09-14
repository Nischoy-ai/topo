#!/bin/bash
# Pre-publication fixture, only on disposable GitHub-hosted macOS runners.
# The real promotion separately tests the exact public HTTPS formula.
set -euo pipefail

if [[ $# != 2 || "${GITHUB_ACTIONS:-}" != true || "${RUNNER_ENVIRONMENT:-}" != github-hosted || "$(uname -s)" != Darwin ]]; then
  echo "usage: $0 <artifact-directory> <prerelease-version> (GitHub-hosted macOS only)" >&2
  exit 2
fi
artifacts=$1
version=$2
fixture=$(mktemp -d "${RUNNER_TEMP:?}/topo-homebrew.XXXXXX")
tap=nischoy-ci/topo-fixture
formula=$tap/topo-beta
if brew list --formula topo >/dev/null 2>&1 || brew list --formula topo-beta >/dev/null 2>&1 ||
   brew tap | grep -Fxq "$tap"; then
  echo "refusing to overwrite an existing Topo installation or fixture tap" >&2
  exit 1
fi
cleanup() {
  brew uninstall --formula "$formula" >/dev/null 2>&1 || true
  brew untap "$tap" >/dev/null 2>&1 || true
  rm -rf "$fixture"
}
trap cleanup EXIT

go run ./internal/distributiontool -mode homebrew-fixture \
  -artifacts "$artifacts" -out "$fixture/rendered" -version "$version"
brew tap-new "$tap"
tap_path=$(brew --repository "$tap")
install -m 0644 "$fixture/rendered/homebrew/Formula/topo-beta.rb" "$tap_path/Formula/topo-beta.rb"
brew install --formula "$formula"
brew test "$formula"
test "$(topo version)" = "$version"
if [[ "$(uname -m)" == arm64 ]]; then
  # Go emits the mandatory ad-hoc ARM64 signature; this is not Developer ID.
  codesign --verify --strict --verbose=2 "$(command -v topo)"
fi
brew uninstall --formula "$formula"
if brew list --formula "$formula" >/dev/null 2>&1; then
  echo "Homebrew uninstall left Topo installed" >&2
  exit 1
fi
echo "Homebrew fixture install, local discovery, and uninstall passed; no Apple notarization claim"
