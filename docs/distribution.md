# Install Topo with a package manager

Install the Topo Beta worker through signed Linux APT/RPM repositories or the
official macOS Homebrew tap on amd64 and arm64. For Topo 0.4.6 Beta discovery,
install the [combined application XML](servicenow-update-set.md), then configure
the worker. Run `topo version` to record the exact worker build for your deployment.

## User installation

The available channel is **Beta**. There is no
stable APT/RPM channel, stable Homebrew formula, or WinGet release yet.
Use a host with `curl`, CA certificates and GnuPG installed for Linux.
The reviewed package-signing fingerprint is
`6049C01BB18CE8EC395DA16F9C64F25B652F0673`; stop on any mismatch.

### Optional Linux setup helper

Add the signed repository once using the Debian/Ubuntu or Fedora/RHEL steps
below. After setup, install with `sudo apt-get install topo` or
`sudo dnf install topo`.

For a combined setup-and-install operation, the repository also includes
`scripts/install-linux.sh` for APT/DNF on amd64/arm64.
It needs curl, GnuPG, standard shell utilities, and root or sudo access. It
checks the pinned archive-key fingerprint before changing repository setup,
retains native package/metadata signature verification, installs the beta and
prints its version. It does not configure or start discovery.

Run:

```sh
curl --proto '=https' --tlsv1.2 -fsS https://raw.githubusercontent.com/Nischoy-ai/topo/main/scripts/install-linux.sh | sh
```

### Debian and Ubuntu

APT uses a repository-scoped keyring, never global `apt-key` trust. Run this
block in a shell; the subshell stops on any verification/setup failure:

```sh
(
  set -eu
  work=$(mktemp -d)
  trap 'rm -rf "$work"' EXIT
  curl -fsSLo "$work/key.asc" https://nischoy-ai.github.io/topo-packages/keys/nischoy-topo-archive.asc
  fingerprint=$(gpg --batch --show-keys --with-colons "$work/key.asc" | awk -F: '$1=="fpr" {print $10; exit}')
  test "$fingerprint" = 6049C01BB18CE8EC395DA16F9C64F25B652F0673
  gpg --batch --dearmor --output "$work/key.gpg" "$work/key.asc"
  sudo install -D -m 0644 "$work/key.gpg" /etc/apt/keyrings/nischoy-topo.gpg
  curl -fsSLo "$work/topo.sources" https://nischoy-ai.github.io/topo-packages/apt/nischoy-topo-beta.sources
  sudo install -m 0644 "$work/topo.sources" /etc/apt/sources.list.d/nischoy-topo.sources
  sudo apt update
  sudo apt -o 'Dpkg::Options::=--path-include=/usr/share/doc/topo' \
    -o 'Dpkg::Options::=--path-include=/usr/share/doc/topo/*' install topo
  topo version
)
```

The `path-include` options retain the worker configuration example even on
[minimized Ubuntu images that omit documentation](https://lists.ubuntu.com/archives/foundations-bugs/2022-February/468556.html).
They do not change APT's signature verification.

### Fedora and RHEL family

Both package and repository signatures remain enabled. Fedora is the tested
RPM environment; these commands are not a claim of validation on every RHEL
derivative.

```sh
(
  set -eu
  work=$(mktemp -d)
  trap 'rm -rf "$work"' EXIT
  curl -fsSLo "$work/key.asc" https://nischoy-ai.github.io/topo-packages/keys/nischoy-topo-archive.asc
  fingerprint=$(gpg --batch --show-keys --with-colons "$work/key.asc" | awk -F: '$1=="fpr" {print $10; exit}')
  test "$fingerprint" = 6049C01BB18CE8EC395DA16F9C64F25B652F0673
  sudo rpm --import "$work/key.asc"
  curl -fsSLo "$work/topo.repo" https://nischoy-ai.github.io/topo-packages/rpm/nischoy-topo-beta.repo
  sudo install -m 0644 "$work/topo.repo" /etc/yum.repos.d/nischoy-topo.repo
  sudo dnf install topo
  topo version
)
```

### macOS Homebrew

```sh
brew install nischoy-ai/tap/topo-beta
topo version
```

This is a CLI formula, not an Apple-notarized app. Do not disable Gatekeeper or
strip quarantine. It conflicts with another formula installing `topo`; existing
development-tap users must explicitly choose when to uninstall their old
formula before installing this one. Installation does not start a worker.

Linux packages install a dormant service, not credentials or configuration.
Continue with [worker configuration](pilot-quickstart.md#4-install-and-configure-the-worker).
Raw release files remain available for verified offline installation. The
experimental controller Helm chart is not required for ServiceNow workers and
is not part of the managed-worker installation path.

## Distribution controls

Package promotion verifies an existing release's checksums, Sigstore identity,
GitHub attestations and RPM signatures. It never rebuilds the worker. Generated
channel inputs must reproduce byte-for-byte, and clean install/removal tests
run before publication. Signing and promotion use protected environments with
review, self-review prevention and restricted refs. Keys remain outside ordinary
build jobs. See [release verification](releases.md) and the public
[promotion workflow](../.github/workflows/promote.yml).

The current `linux-homebrew-beta` profile excludes Windows artifacts and
accepts only prereleases. It requires signed Linux packages/metadata,
Sigstore/SBOM/provenance and Intel/ARM64 Homebrew execution tests. It does not
provide Apple Developer ID signing or notarization. The `linux-macos-beta`
profile keeps Apple signing gates; the full-platform profile additionally
requires Windows Authenticode. Missing keys never trigger an unsigned fallback.

Stable promotion requires an actual previous stable release and an install/
upgrade test through the generated channel. Stable/N-1 and Windows publication
remain planned. APT metadata uses Acquire-By-Hash and a 30-day Valid-Until;
active metadata needs regular signed refreshes. A partially completed promotion
can leave channels at different states, so verify each public channel result.

## Key rotation and incident response

Repository trust is scoped to the published OpenPGP fingerprint. Planned
rotation publishes the new public key alongside the current key, announces the
new fingerprint and gives operators time to refresh scoped keyrings before
signing changes. Keep both public keys for the documented overlap period.

After suspected compromise, revoke trust, stop promotions, distribute the new
key out of band, and publish a new signed release and incident advisory.
Never silently replace immutable worker release assets or reuse a release tag.

## ServiceNow application XML

[Topo 0.4.6 Beta](https://github.com/Nischoy-ai/topo/releases/tag/servicenow-0.4.6-beta)
is published as one native batch containing the application and 32 indexes.
Follow [XML installation](servicenow-update-set.md). The manually exported XML
has checksums; it is not covered by the signed worker's provenance attestations.
Check the release manifest for the exact component builds and download origin.

## Validation records

See [package-channel evidence](evidence/distribution.md) and
[XML validation](servicenow-validation.md) for dated results and tested scope.
