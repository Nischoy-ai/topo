# Package-manager distribution

M2.5 promotes one already-published, fully verified GitHub Release into native
package-manager channels. Promotion never invokes `go build`, nFPM, WiX, or
Helm packaging. The GitHub Release remains the immutable source of the exact
DEB, RPM, MSI, raw archive, and chart bytes referenced by every channel.

**Current beta scope (2026-09-14): Linux APT/RPM and macOS/Homebrew CLI.**
The reviewed release workflow selects `linux-homebrew-beta`: four raw archives
(Linux/macOS, amd64/arm64), no Windows ZIPs or MSIs, and no WinGet manifests.
Windows code and full-platform tooling remain supported but Windows signing
provisioning is deferred. The unused Azure signing account was deleted with
owner approval. The ServiceNow application, offline bundle, and existing Helm
artifact path remain included; no discovery capability changes.

Apple Developer Program membership is not a prerequisite for this CLI formula.
The owner explicitly deferred Developer ID/notarization for the beta. It keeps
checksummed formula downloads, Sigstore-signed release checksums, GitHub
provenance/SBOM attestations, and protected reviews, but has **no Apple publisher
identity or notarization ticket**. Go's ARM64 ad-hoc signature is not a developer
identity. Homebrew install, local discovery, and uninstall must pass on Intel
and Apple Silicon without disabling Gatekeeper or stripping quarantine.
Browser-downloaded raw binaries may be blocked by Gatekeeper; use the tested
Homebrew path, not a security bypass. Managed Macs may impose stricter policy.
See [Homebrew's formula/cask trust distinction](https://docs.brew.sh/Homebrew-Security-and-Supply-Chain#casks-have-a-different-trust-model).

| Release profile | Platforms | Apple Developer ID/notarization |
| --- | --- | --- |
| `linux-homebrew-beta` (current) | Linux/macOS; prerelease only | Deferred; Homebrew execution tests required |
| `linux-macos-beta` | Linux/macOS; prerelease only | Required |
| `all` or omitted | Linux/macOS/Windows | Required; Windows Authenticode also required |

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
5. Audit/install/test the exact Homebrew formula on both Mac architectures.
   Developer ID/notarization assessment remains required for Apple-signed
   profiles. Stable additionally validates WinGet and its exact MSI URL/digest.
6. Push/pull-compare the existing chart through GHCR, then publish static APT/
   RPM metadata, the Homebrew formula, and (stable only) a WinGet pull request.

Validation failures block publication; a failure during publication can leave
some channels published and others pending. Stable and beta use separate
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
  `microsoft/winget-pkgs` (deferred; not needed for the Linux/macOS beta).

Protect the `native-package-signing`, `distribution-beta`, and
`distribution-stable` GitHub environments. Require reviewer approval and
prevent self-review for every environment. Allow exactly the `v*` tag pattern
to deploy to `native-package-signing`; the release workflow independently
rejects non-semantic tags and tags whose commit is not reachable from `main`.
Allow exactly the `main` branch to deploy to each distribution environment;
the promotion workflow also rejects dispatches from any other ref. Store:

| Environment | Secret | Purpose |
| --- | --- | --- |
| `native-package-signing` | `RPM_SIGNING_PRIVATE_KEY` / `RPM_SIGNING_FINGERPRINT` | Sign RPM bytes before they enter the GitHub Release. The export may be unencrypted because GitHub encrypts the environment secret and the key exists only in the ephemeral signing keyring. |
| `native-package-signing` | `AZURE_CLIENT_ID`, `AZURE_TENANT_ID`, `AZURE_SUBSCRIPTION_ID` | Identify the Microsoft Entra workload identity used by GitHub OIDC. No client secret is stored. |
| `native-package-signing` | `ARTIFACT_SIGNING_ENDPOINT`, `ARTIFACT_SIGNING_ACCOUNT_NAME`, `ARTIFACT_SIGNING_CERTIFICATE_PROFILE_NAME` | Select the Azure Artifact Signing account and public-trust certificate profile that Authenticode-sign both MSI installers. These are configuration identifiers, stored as environment secrets so they are released only after environment approval and the preflight can inspect names without retrieving values. |
| `native-package-signing` | `APPLE_DEVELOPER_ID_P12_BASE64`, `APPLE_DEVELOPER_ID_P12_PASSWORD`, `APPLE_DEVELOPER_ID_IDENTITY` | Import the Developer ID Application identity into an ephemeral macOS keychain. |
| `native-package-signing` | `APPLE_NOTARY_ISSUER_ID`, `APPLE_NOTARY_KEY_ID`, `APPLE_NOTARY_PRIVATE_KEY` | Submit both signed macOS binaries to Apple's notary service. |
| each distribution environment | `REPOSITORY_SIGNING_PRIVATE_KEY` / `REPOSITORY_SIGNING_FINGERPRINT` | Clear-sign APT metadata and sign RPM repository metadata. This must be the same identity used to sign release RPMs for one coherent repository trust root. |
| each distribution environment | `DISTRIBUTION_GITHUB_TOKEN` | Fine-grained token limited to contents write on the three distribution repositories and pull-request creation for the WinGet fork. It has no Topo source write permission. |
| optional during rotation | `REPOSITORY_ADDITIONAL_PUBLIC_KEY` / `REPOSITORY_ADDITIONAL_PUBLIC_KEY_FINGERPRINT` | Publish an old/new overlap keyring without granting the additional key signing authority in that run. |

For `linux-homebrew-beta`, neither the six Apple nor six Azure/Artifact Signing
identifiers are required. `linux-macos-beta` still requires all six Apple names.
The beta distribution token needs **Contents: read and write** only
on `topo-packages` and `homebrew-tap`, not the source repository or WinGet fork.
Use an expiry and provision it directly into `distribution-beta`; do not paste
it into chat. Existing optional Windows identifiers are tolerated by the
preflight but never used in this profile. Optional Apple names are also ignored
only for `linux-homebrew-beta`. Linux OpenPGP entries remain mandatory; absence
never triggers an unsigned fallback.

### Apple signing identity

Deferred for the current Homebrew-only beta; required for the Apple-signed
profiles. Do not enroll or provision these credentials just to use this beta.

Use Nischoy's Apple Developer Program organization account. Its Account Holder
creates a **Developer ID Application** certificate (not Apple Development,
Mac App Distribution, or Developer ID Installer). Apple documents the CSR and
certificate flow in [Developer ID certificates](https://developer.apple.com/help/account/certificates/create-developer-id-certificates/).
Keep the private key local and protected; export the matching certificate and
private key as a password-protected P12 and place its base64 encoding, export
password, and exact signing identity directly in `native-package-signing`.
The remaining three entries hold the team's notary API key, key ID, and issuer
ID. See [Apple's notarization workflow](https://developer.apple.com/documentation/security/customizing-the-notarization-workflow).
The workflow signs both architectures and requires explicit `Accepted` notary
status before rearchiving either. This is an archive/Homebrew distribution,
not a Mac App Store submission or a new macOS PKG installer.

Make the `ghcr.io/nischoy-ai/charts/topo` package public after its first
workflow-created publication. The repository-scoped `GITHUB_TOKEN` receives
only `packages: write` in the final publication job.

### Windows signing identity

Deferred for the current beta; do not provision this service for Linux/macOS.

Create an Azure Artifact Signing account and a public-trust certificate
profile, then create a dedicated Microsoft Entra application or user-assigned
managed identity. Grant it only `Artifact Signing Certificate Profile Signer`
on the selected profile. Add one federated identity credential for GitHub's
issuer `https://token.actions.githubusercontent.com`, audience
`api://AzureADTokenExchange`, and the `native-package-signing` environment of
this repository. The environment-based subject prevents an unprotected job
from exchanging a token; use the exact immutable or legacy subject GitHub
reports for this repository rather than guessing it.

Place the six identifiers named in the table directly in the protected GitHub
environment. Do not create an Azure client secret and do not export a PFX to
GitHub. The Windows release job uses the official Azure login and Artifact
Signing actions, pinned to immutable commits, requests `id-token: write` only
for that job, timestamps both MSIs with Microsoft's RFC 3161 service, and then
uses Windows SignTool to verify the complete public trust chain.

### Read-only prerequisite preflight

Before creating a tag, run:

```sh
scripts/check-production-distribution.sh -profile linux-homebrew-beta
```

The preflight uses the already-authenticated GitHub CLI and emits one bounded
JSON report. It checks that the two beta distribution repositories are active
and public, Pages is HTTPS-only from `main` at the repository root, each
environment prevents self-review and has at least two reviewers, administrator
bypass is disabled, the native environment permits exactly `v*` tags, each
distribution environment permits exactly `main`, and the exact required
environment-secret names are present. GitHub's secret-list endpoint exposes
names only; the preflight never requests values, discards command stderr, and
does not mutate GitHub. A non-ready report exits nonzero.

Omitting `-profile` (or using `-profile all`) retains the full-platform checks.
Unknown profiles are rejected.

As of 2026-09-14, `Nischoy-ai/topo-packages` and
`Nischoy-ai/homebrew-tap` exist as public repositories, the package Pages site
is built with HTTPS enforcement, and `native-package-signing` plus
`distribution-beta` exist with self-review prevention, administrator bypass
disabled, and two eligible reviewers. The native environment permits only
`v*` tags while beta distribution permits only `main`. The OpenPGP private key
and fingerprint names are present in both environments. The fail-closed report
for the current profile needs only `DISTRIBUTION_GITHUB_TOKEN`; the absent Apple
names are no longer prerequisites under the owner-approved revision. Key-name
presence does not prove a usable OpenPGP key. Place credential values directly in the
environments—never in chat, source control, shell arguments, or ordinary CI.
`Nischoy-ai/winget-pkgs`,
`distribution-stable`, and stable secrets remain intentionally unprovisioned
until the separate N-1 stable slice.

That dated provisioning snapshot is superseded by the release evidence below.
Package channels must not be represented as available until a real beta
promotion succeeds. A production claim additionally requires the later
N-1-gated stable promotion and remaining security-review gates.

## First beta operational evidence

**Published (2026-09-20 UTC).** After PR #62 merged at `dd3c349`,
[promotion 35485290078](https://github.com/Nischoy-ai/topo/actions/runs/35485290078)
passed all required jobs after Prodyot's independent approvals. Package
repository commit `3d0a4fe15c9f7bd5dd40450f16ae0efafa4ba341` and official tap
commit `3ffdb92732fc6ebdcd6c2bd3f4a96fd3f417d5d7` publish `v0.1.0-beta.1`.
Pages built that package commit and anonymously serves the signed beta APT/RPM
metadata over HTTPS. Git write authority is now proven for both repositories.
Authenticated OCI chart pull/byte comparison passed; anonymous OCI access is
not claimed. Published release assets remain unchanged.

Post-publication acceptance uses `live beta channel acceptance`: four fresh
Linux container jobs (APT/RPM on amd64/arm64) plus Intel/ARM64 Mac runners
consume the actual public repositories/tap. Linux pins the OpenPGP fingerprint,
checks signatures, compares the installed binary to a source-pinned release
archive, exercises local discovery, checks dormant worker installation, and
removes the package while preserving an operator file. Macs verify the reviewed
live formula hash before audit/install/discovery/removal. These source pins
must be reviewed together when a new beta replaces this fixture. The workflow
is read-only externally, uses no signing secrets, and is not a promotion.
Execution results are recorded in the current project handoff; merely adding
these checks is not evidence they passed. Local Docker could not run them due
to laptop disk exhaustion; that failed attempt is not installation evidence.

The chronological attempts below explain the fixes leading to publication.

The owner provisioned the distribution token, and the `linux-homebrew-beta`
preflight passed. [Release attempt 2](https://github.com/Nischoy-ai/topo/actions/runs/34929267383/attempts/2)
published [v0.1.0-beta.1](https://github.com/Nischoy-ai/topo/releases/tag/v0.1.0-beta.1)
from commit `57671b5407daabddd7ae08d14dd25395e0b9431f`. Reproducible builds,
Linux package lifecycle, protected RPM signing, and Intel/ARM64 Homebrew
install/execution/removal passed. Independent downloads matched every listed
checksum; the manifest's Sigstore identity matched the exact tag workflow,
and Linux amd64/macOS arm64 provenance matched the repository and commit.
This is real release evidence, not simulator evidence or Apple notarization.

The first [beta promotion](https://github.com/Nischoy-ai/topo/actions/runs/35243074007)
passed release verification, repository signing, and the APT lifecycle gate,
then stopped before the RPM test could start: its Fedora digest differed from
the release package test's valid pin and returned `manifest unknown`.
Homebrew channel tests and channel publication did not run. Local validation
also reproduced an indented heredoc terminator that could swallow the RPM
installation commands and falsely exit successfully. The repair aligns the
image pins, uses `printf` for repository configuration, asserts binary removal,
and guards against both regressions; it changes no release asset, signature
requirement, or approval policy. After its PR merges, dispatch a new
promotion from the updated `main` with `version=v0.1.0-beta.1`, `channel=beta`;
rerunning the failed run would reuse the old workflow revision. Independent
`distribution-beta` approval and all remaining gates are still required.

After PR #60 merged, [promotion 35388277718](https://github.com/Nischoy-ai/topo/actions/runs/35388277718)
passed protected repository signing, release verification, and both real APT
and RPM install/remove gates. Both Mac runners then failed the strict online
formula audit: an extra blank line, incorrect stanza order, a redundant
explicit version, and an unqualified conflict against unpublished `topo`.
No channel was published. The follow-up renderer uses URL-derived versions,
correct spacing/order, and reciprocal `nischoy-ai/tap/topo` /
`nischoy-ai/tap/topo-beta` conflict declarations. It does not invent a stable
formula or suppress any audit rule.

Both Mac CI jobs now also render the production HTTPS formula from the existing
`v0.1.0-beta.1` release, whose checksum-manifest digest is pinned in source,
then run the strict online audit, install, version/local-discovery test, and
removal. This catches public formula failures before protected promotion;
the existing local-archive fixture still tests newly built binaries. Neither
test publishes a tap or accesses production signing keys. Changing the pinned
release fixture requires review of its version and authenticated manifest
digest together. The actual promotion continues to verify release signatures
and provenance and audit the exact formula it will publish.

After PR #61 merged at `c329e12`, [promotion 35467069999](https://github.com/Nischoy-ai/topo/actions/runs/35467069999)
passed protected repository signing, APT/RPM lifecycle tests, and strict audit
plus installation/execution/removal on both Mac architectures. Following the
independent publication approval, the OCI chart publication and authenticated
pull/byte comparison passed. Anonymous chart access remains unverified.
The subsequent first Git push failed with `could not read Username for
'https://github.com'`: `GH_TOKEN` authenticated GitHub CLI but Git had no
credential helper. The package repository remained at initial commit `6aff42e`
and the tap at `cf60cb3`; neither channel was published.

The repair runs `gh auth setup-git --hostname github.com` before repository
operations, letting Git obtain the existing step-scoped token through GitHub
CLI. The stable WinGet step uses the same explicit setup; beta still skips it.
No secret is placed in a remote URL or persisted in Git configuration, and no
token permission is broadened. Regression tests protect setup ordering and
exercise real Git/GitHub CLI credential handoff offline with a dummy token,
isolated configuration, and unrelated-host denial. They do not verify the
production token's write permissions. Merge the green repair PR, then dispatch
a new protected promotion from `main` for the unchanged beta release. Do not
rerun the old revision or replace release assets. APT/RPM/Homebrew publication
and public channel installation remain pending.

## Development-only Homebrew pilot

With explicit operator authorization, a separate public development tap was
published at <https://github.com/Nischoy-ai/homebrew-topo-dev>. Its
`v0.0.0-dev.3` prerelease contains raw archives built twice from merged Topo
commit `c2332fcbeec734b7d19ba07b1ef193881a2545fd`, recorded in
`release-metadata.json`, with exact Go 1.26.8 from separate source paths; the
two outputs matched byte-for-byte. Every published asset was downloaded again
and verified against the published `SHA256SUMS`. The formula passed
`brew style`, strict online `brew audit`, a real Apple Silicon upgrade from
`v0.0.0-dev.2`, and `brew test`. The installed binary reports
`v0.0.0-dev.3` and exposes the supported `topo publish servicenow` IRE
workflow.

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
promotion gate. The development build uses `golang.org/x/crypto` v0.55.0; the
earlier withdrawn build used v0.54.0, which the project security gate began
rejecting as reachable `GO-2026-6303` on 2026-08-28.

## Release and promotion

Create the reviewed release tag using [the release procedure](releases.md).
The selected Homebrew-only profile requires RPM signing and successful Mac
installation tests. `linux-macos-beta` additionally requires Developer ID and
notarization; full-platform releases also require Azure Artifact Signing.
Both beta profiles reject stable tags outright.
The profile is recorded in release, package, and promotion metadata and covered
by the authenticated manifest. A skipped Windows job permits publication only
when the validated profile explicitly excludes Windows. Apple signing may be
skipped only for the Homebrew-only profile, with both required Mac tests green;
signing failures never become an unsigned fallback. Native signing uses isolated jobs;
the final job refreshes release metadata
and checksums after native signatures are applied, then creates
Sigstore/GitHub evidence over the final bytes.

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

The available channel is **beta**, currently `v0.1.0-beta.1`. There is no
stable APT/RPM channel, stable Homebrew formula, or WinGet release yet.
Use a pilot host, with `curl`, CA certificates and GnuPG installed for Linux.
The reviewed package-signing fingerprint is
`6049C01BB18CE8EC395DA16F9C64F25B652F0673`; stop on any mismatch.

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
  sudo apt install topo
  topo version
)
```

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
- The first real beta is published; N-1 stable promotion remains required evidence.
  Pull-request CI proves deterministic generation and syntax only; it has no
  production signing keys and performs no external publication. External-
  security-review preparation does not waive or simulate this gate; provision
  the repositories and production signing credentials only with explicit user
  authorization.
