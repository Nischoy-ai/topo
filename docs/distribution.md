# Package-manager distribution

M2.5 promotes one already-published, fully verified GitHub Release into native
package-manager channels. Promotion never invokes `go build`, nFPM, WiX, or
Helm packaging. The GitHub Release remains the immutable source of the exact
DEB, RPM, MSI, raw archive, and chart bytes referenced by every channel.

The manual `promote package-manager channels` workflow accepts a semantic
release tag, `beta` or `stable`, and (for stable) the previous stable tag. It
performs these operations in order:

1. Verify `SHA256SUMS`, its keyless release-workflow identity, GitHub
   attestations, and the native RPM signatures.
2. Generate APT, RPM, Homebrew, WinGet, and OCI Helm inputs twice and reject
   any byte drift.
3. Sign APT `Release`/`InRelease` and RPM `repomd.xml` in the protected
   distribution environment.
4. Exercise clean Ubuntu and Fedora installs. Stable additionally installs the
   supplied N-1 release first and upgrades it through the generated channel.
5. Audit/install/test the exact Homebrew formula on macOS, including Developer
   ID and notarization assessment; validate the WinGet manifest with pinned
   Microsoft's WingetCreate and exercise its exact MSI URL and digest.
6. Push/pull-compare the existing chart through GHCR, then publish static APT/
   RPM metadata, the Homebrew formula, and (stable only) a WinGet pull request.

Any failure occurs before external publication. Stable and beta use separate
protected environments and one shared serialization lock because they mutate
some of the same repositories. Repeating a partially completed promotion is
safe: immutable OCI bytes are compared, unchanged Git commits are skipped, and
an existing WinGet submission is reused.

## One-time organization setup

Provision these public repositories before the first promotion:

- `Nischoy-ai/topo-packages`, with GitHub Pages serving the `main` branch at
  `https://nischoy-ai.github.io/topo-packages`;
- `Nischoy-ai/homebrew-tap`, with an initial `Formula/` directory;
- an organization fork named `Nischoy-ai/winget-pkgs` of
  `microsoft/winget-pkgs`.

Protect the `native-package-signing`, `distribution-beta`, and
`distribution-stable` GitHub environments. Require reviewer approval for both
distribution environments and restrict them to the `main` branch; the workflow
also rejects dispatches from any other ref. Store:

| Environment | Secret | Purpose |
| --- | --- | --- |
| `native-package-signing` | `RPM_SIGNING_PRIVATE_KEY` / `RPM_SIGNING_FINGERPRINT` | Sign RPM bytes before they enter the GitHub Release. The export may be unencrypted because GitHub encrypts the environment secret and the key exists only in the ephemeral signing keyring. |
| `native-package-signing` | `WINDOWS_SIGNING_PFX_BASE64` / `WINDOWS_SIGNING_PFX_PASSWORD` | Authenticode-sign both MSI installers. |
| `native-package-signing` | `APPLE_DEVELOPER_ID_P12_BASE64`, `APPLE_DEVELOPER_ID_P12_PASSWORD`, `APPLE_DEVELOPER_ID_IDENTITY` | Import the Developer ID Application identity into an ephemeral macOS keychain. |
| `native-package-signing` | `APPLE_NOTARY_ISSUER_ID`, `APPLE_NOTARY_KEY_ID`, `APPLE_NOTARY_PRIVATE_KEY` | Submit both signed macOS binaries to Apple's notary service. |
| each distribution environment | `REPOSITORY_SIGNING_PRIVATE_KEY` / `REPOSITORY_SIGNING_FINGERPRINT` | Clear-sign APT metadata and sign RPM repository metadata. This must be the same identity used to sign release RPMs for one coherent repository trust root. |
| each distribution environment | `DISTRIBUTION_GITHUB_TOKEN` | Fine-grained token limited to contents write on the three distribution repositories and pull-request creation for the WinGet fork. It has no Topo source write permission. |
| optional during rotation | `REPOSITORY_ADDITIONAL_PUBLIC_KEY` / `REPOSITORY_ADDITIONAL_PUBLIC_KEY_FINGERPRINT` | Publish an old/new overlap keyring without granting the additional key signing authority in that run. |

Make the `ghcr.io/nischoy-ai/charts/topo` package public after its first
workflow-created publication. The repository-scoped `GITHUB_TOKEN` receives
only `packages: write` in the final publication job.

These production repositories and secrets do not exist as of 2026-08-28. No
official public tag or package channel should be represented as available until
this setup, a beta promotion, and then the first N-1-gated stable promotion
have succeeded.

## Development-only Homebrew pilot

With explicit operator authorization, a separate public development tap was
published at <https://github.com/Nischoy-ai/homebrew-topo-dev>. Its
`v0.0.0-dev.1` prerelease contains raw archives built from merged Topo commit
`32733488a704114e3a805c6313aae4257cade7d4` twice with exact Go 1.25.13 from
separate source paths; the two outputs matched byte-for-byte. Every published
asset was downloaded again and verified against the published `SHA256SUMS`.
The formula passed `brew style`, strict online `brew audit --new`, a real
Apple Silicon `brew install`, and `brew test`.

Install this development build with:

```sh
brew install nischoy-ai/topo-dev/topo
```

The formula and executable are both named `topo`. These artifacts have only
Go's ad-hoc linker signature on macOS: no Apple Developer
ID identity, notarization ticket, Sigstore bundle, GitHub build provenance,
SBOM, or protected promotion evidence. The GitHub prerelease is mutable. This
pilot is not the future official `Nischoy-ai/homebrew-tap`, is not supported
for production, and does not satisfy either the real-beta or N-1 stable
promotion gate.

## Release and promotion

Create the reviewed release tag using [the release procedure](releases.md).
That workflow now fails closed unless RPM, Authenticode, Developer ID, and
notarization credentials are available. RPM signing and macOS signing run in
isolated jobs; the final job refreshes release metadata and checksums after
native signatures are applied, then creates Sigstore/GitHub evidence over the
final bytes.

After the GitHub Release exists, dispatch `promote package-manager channels`:

- use `beta` only for a prerelease tag such as `v0.2.0-beta.1`;
- use `stable` only for a normal release tag and supply a distinct prior stable
  tag. The workflow downloads and installs that actual N-1 release before
  upgrading; it never fabricates an older version.

Promotion metadata uses the workflow run's immutable creation timestamp as an
explicit input, so all retries of one approved run reproduce the same unsigned
repository inputs. APT indices use `Acquire-By-Hash` and a 30-day
`Valid-Until`; rerun promotion for the current release at least monthly to
refresh an active channel even when no new Topo version is ready.

## User installation

APT uses a repository-scoped keyring and Deb822 source definition, never
global `apt-key` trust:

```sh
curl -fsSLo /tmp/nischoy-topo-archive.gpg \
  https://nischoy-ai.github.io/topo-packages/keys/nischoy-topo-archive.gpg
sudo install -D -m 0644 /tmp/nischoy-topo-archive.gpg \
  /etc/apt/keyrings/nischoy-topo.gpg
curl -fsSLo /tmp/nischoy-topo.sources \
  https://nischoy-ai.github.io/topo-packages/apt/nischoy-topo-stable.sources
sudo install -m 0644 /tmp/nischoy-topo.sources \
  /etc/apt/sources.list.d/nischoy-topo.sources
sudo apt update
sudo apt install topo
```

Fedora/RHEL-compatible systems verify both RPM packages and repository
metadata. The `.repo` file references the ASCII-armored copy of the same trust
root that APT receives as a binary scoped keyring:

```sh
sudo curl -fsSLo /etc/yum.repos.d/nischoy-topo.repo \
  https://nischoy-ai.github.io/topo-packages/rpm/nischoy-topo-stable.repo
sudo dnf install topo
```

Other channels:

```sh
brew install nischoy-ai/tap/topo
brew install nischoy-ai/tap/topo-beta # prerelease channel; conflicts with topo
winget install --id Nischoy.Topo -e
helm install topo oci://ghcr.io/nischoy-ai/charts/topo \
  --version 0.2.0 --set apiKeySecret.name=topo-api-key
```

The Helm chart still requires an externally created API-key Secret. Host
packages still install a dormant service definition and never generate a
credential, configuration, or enabled service.

## Key rotation and incident response

Repository keys never appear in ordinary CI. Inventory the full fingerprint
and expiry in the release runbook, keep the offline recovery copy separately,
and rotate before expiry or immediately after suspected exposure.

For planned rotation, first publish the new public key alongside the current
key with the optional overlap secrets while metadata remains signed by the old
key. Announce the new fingerprint and require operators to refresh their scoped
keyring. After the overlap window, configure both native signing and repository
signing with the new key, promote a beta, then stable; retain both public keys
for the documented support window before removing the old one. A compromised
key skips the overlap: revoke trust, stop promotions, replace the public
keyring out of band, re-sign repository metadata and RPMs in a new release,
and publish an incident advisory. Never silently reuse a tag or replace a
GitHub Release asset.

## Deliberate boundaries

- WinGet catalog availability begins only after Microsoft's validation and
  review merge the generated pull request. The workflow cannot declare that
  external state successful itself.
- Homebrew/core is not a launch requirement; the organization tap is the
  supported initial channel.
- Chocolatey, Scoop, AUR, Snap, and other ecosystems follow demonstrated
  demand.
- The first real beta and N-1 stable promotions remain required evidence.
  Pull-request CI proves deterministic generation and syntax only; it has no
  production signing keys and performs no external publication. External-
  security-review preparation does not waive or simulate this gate; provision
  the repositories and production signing credentials only with explicit user
  authorization.
