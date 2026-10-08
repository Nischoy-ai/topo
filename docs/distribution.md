# Install Topo with a package manager

The current worker release is **v0.1.0-beta.1**, available through signed Linux
APT/RPM repositories and the official macOS Homebrew tap on amd64 and arm64.
Start with the installation steps below. For ServiceNow-managed discovery,
install the [combined application XML](servicenow-update-set.md) separately;
its application version is **0.4.6**.

## User installation

The available channel is **beta**, currently `v0.1.0-beta.1`. There is no
stable APT/RPM channel, stable Homebrew formula, or WinGet release yet.
Use a pilot host, with `curl`, CA certificates and GnuPG installed for Linux.
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

The helper passed fresh hosted-container installation on APT and RPM, on
both amd64 and arm64, including signed-package installation, binary comparison,
local discovery and removal. See the [acceptance run](https://github.com/Nischoy-ai/topo/actions/runs/37809561514).

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
is not the recommended pilot installation path.

## First beta operational evidence

The [v0.1.0-beta.1 release](https://github.com/Nischoy-ai/topo/releases/tag/v0.1.0-beta.1)
was built from `57671b5407daabddd7ae08d14dd25395e0b9431f`.
[Release attempt 2](https://github.com/Nischoy-ai/topo/actions/runs/34929267383/attempts/2)
passed reproducible builds, Linux package lifecycle, protected RPM signing,
and Intel/ARM64 Homebrew installation, execution and removal. Independent
downloads matched the checksum manifest; Sigstore identity matched the tagged
release workflow, and Linux amd64/macOS arm64 provenance matched the repository
and source commit.

[Promotion 35485290078](https://github.com/Nischoy-ai/topo/actions/runs/35485290078)
passed protected signing and publication reviews on 2026-09-20. Package commit
`3d0a4fe15c9f7bd5dd40450f16ae0efafa4ba341` and tap commit
`3ffdb92732fc6ebdcd6c2bd3f4a96fd3f417d5d7` published the signed beta channels.
HTTPS Pages served the signed APT/RPM metadata. Authenticated OCI chart
pull/byte comparison passed; anonymous OCI access has not been established.

[Live acceptance 35487126595](https://github.com/Nischoy-ai/topo/actions/runs/35487126595)
passed all six fresh public-channel jobs: APT/RPM on amd64/arm64 and Homebrew
on Intel/Apple Silicon. Linux checked the pinned OpenPGP fingerprint and
signatures, compared the installed binary with a source-pinned release archive,
ran local discovery, verified dormant worker installation and removed the
package while preserving an operator file. Macs checked the reviewed formula
hash, audited, installed, ran local discovery and removed the formula. These
are package installation checks; ServiceNow runtime evidence is recorded
[separately](servicenow-validation.md).

Earlier promotion attempts stopped before APT/RPM/Homebrew publication:

| Run | Result and correction |
| --- | --- |
| [35243074007](https://github.com/Nischoy-ai/topo/actions/runs/35243074007) | Invalid Fedora image pin blocked the RPM gate; the pin and shell-command regression guards were corrected. |
| [35388277718](https://github.com/Nischoy-ai/topo/actions/runs/35388277718) | Homebrew audit failed; formula spacing, stanza order, version and conflict declarations were corrected. |
| [35467069999](https://github.com/Nischoy-ai/topo/actions/runs/35467069999) | OCI publication passed, but Git authentication blocked repository/tap publication; explicit GitHub CLI credential-helper setup was added. |

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

## Legacy development tap

The earlier [development tap](https://github.com/Nischoy-ai/homebrew-topo-dev)
published `v0.0.0-dev.3` from `c2332fcbeec734b7d19ba07b1ef193881a2545fd`.
Its reproducible archives, checksums and Apple Silicon upgrade/formula tests
were development evidence. That mutable prerelease had no Sigstore signature,
GitHub provenance, SBOM, Apple Developer ID identity or notarization ticket.
Use the official beta channel above for new installations. An earlier withdrawn
build used `x/crypto` v0.54.0 and was rejected by the security gate for reachable
`GO-2026-6303`; the development successor used v0.55.0. Those historical checks
do not establish current vulnerability status.

## ServiceNow application XML

[ServiceNow 0.4.6 preview 2](https://github.com/Nischoy-ai/topo/releases/tag/servicenow-0.4.6-preview.2)
is published as one native batch containing the application and 32 indexes.
It passed fresh/repeat installation and a focused 0.4.5→0.4.6 configuration
upgrade. See [XML installation](servicenow-update-set.md) and the
[validation record](servicenow-validation.md) for exact scope. The manual XML
preview has checksums but no cryptographic signature or provenance attestation;
its version and verification evidence are separate from the signed worker beta.
