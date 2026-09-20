#!/bin/bash
# Exercise the published tap, not a locally rendered substitute formula.
set -euo pipefail
if [[ $# != 0 || "${GITHUB_ACTIONS:-}" != true || "${RUNNER_ENVIRONMENT:-}" != github-hosted || "$(uname -s)" != Darwin ]]; then
  echo "usage: $0 (disposable GitHub-hosted macOS only)" >&2
  exit 2
fi
tap=nischoy-ai/tap
formula=$tap/topo-beta
if brew list --formula topo >/dev/null 2>&1 || brew list --formula topo-beta >/dev/null 2>&1 ||
   brew tap | grep -Fxq "$tap"; then
  echo "refusing to overwrite an existing Topo installation or tap" >&2
  exit 1
fi
export HOMEBREW_NO_AUTO_UPDATE=1
created_tap=false
cleanup() {
  if [[ "$created_tap" == true ]]; then
    brew uninstall --formula "$formula" >/dev/null 2>&1 || true
    brew untap "$tap" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT
brew tap "$tap" https://github.com/Nischoy-ai/homebrew-tap
created_tap=true
tap_path=$(brew --repository "$tap")
# Pin reviewed live formula bytes, including every archive checksum, before execution.
printf '%s  %s\n' 6eb10b518cd84fbc2a98f656e6609ce7672675f47d920324e078224d5dc686fe \
  "$tap_path/Formula/topo-beta.rb" | shasum -a 256 --check -
brew audit --strict --online "$formula"
brew install --formula "$formula"
brew test "$formula"
test "$(topo version)" = v0.1.0-beta.1
if [[ "$(uname -m)" == arm64 ]]; then
  codesign --verify --strict --verbose=2 "$(command -v topo)"
fi
brew uninstall --formula "$formula"
if brew list --formula "$formula" >/dev/null 2>&1; then exit 1; fi
echo "Live official Homebrew tap audit, installation, local discovery and removal passed"
