# Release artifacts and verification

Check [worker availability](distribution.md#release-availability) before downloading.
The signed [worker `v0.4.6-beta.1`](https://github.com/Nischoy-ai/topo/releases/tag/v0.4.6-beta.1)
is published with its binary archives, signed checksum manifest and provenance.
It is also available through the Beta Homebrew/APT/RPM channels.
The [ServiceNow XML package](servicenow-update-set.md) is a separate manual
artifact with its own signed checksum manifest and [validation record](servicenow-validation.md).
Worker provenance is separate from XML-package authentication.

## Verify the ServiceNow package

Download the ZIP, `SHA256SUMS` and `SHA256SUMS.sigstore.json` from the
[ServiceNow release](https://github.com/Nischoy-ai/topo/releases/tag/servicenow-0.4.6-beta.2).
The exact signing source commit is also recorded in the release notes:

```sh
cosign verify-blob \
  --bundle SHA256SUMS.sigstore.json \
  --certificate-identity https://github.com/Nischoy-ai/topo/.github/workflows/sign-servicenow-xml.yml@refs/heads/main \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-github-workflow-sha 773b9d510760db4dcde56a0b8189eb8882ccf18d \
  SHA256SUMS
```

Then unzip the package in the same directory, retain the ZIP, and run
`sha256sum -c SHA256SUMS` on Linux or `shasum -a 256 -c SHA256SUMS` on macOS.
The signature authenticates the checksum manifest and its exact package bytes;
it does not attest how the native XML was exported. The worker signature and
provenance instructions below cover different files.

## Worker release build

Topo worker releases are built only from semantic tags (`vMAJOR.MINOR.PATCH`, with an
optional prerelease suffix) whose commit is already reachable from `main`.
`.github/workflows/release.yml` uses the exact Go 1.26.9 toolchain and
commit-pinned actions.

The current reviewed workflow selects **`linux-homebrew-beta`**. It accepts
only prerelease tags, omits Windows artifacts, and explicitly defers Apple
Developer ID/notarization for the Homebrew CLI path. Intel and Apple Silicon
Homebrew installation, execution, and uninstall tests replace the Apple signer
gate for this profile only; no Gatekeeper setting or quarantine is bypassed.
Linux signing, Sigstore, attestations, and protected reviews remain mandatory.
The earlier `linux-macos-beta` profile still requires Apple signing, and `all`
still requires both Apple and Windows signing. Selection requires reviewed
source, never a fallback based on which secrets exist.

One release contains the following (Windows entries apply only to `all`;
Developer ID/notarization applies only to the two Apple-signed profiles):

- deterministic raw archives for Linux, macOS, and Windows on amd64 and arm64;
- DEB and OpenPGP-signed RPM packages for Linux amd64/arm64,
  Authenticode-signed MSI installers for Windows amd64/arm64,
  Developer-ID-signed/notarized macOS archives, a Helm chart, a validated
  installable ServiceNow scoped-application ZIP, and a deterministic offline
  bundle;
- `release-metadata.json`, recording the source commit, toolchain, build flags,
  target matrix, release profile, and each archive's SHA-256 digest;
- `package-metadata.json`, binding native package payloads to their source
  archive binary digests and identifying the pinned ServiceNow SDK assembler;
- `servicenow-app-metadata.json`, recording the exact app scope, version,
  tables, roles, worker resources, entry count, and normalized ZIP digest;
- `SHA256SUMS` for every raw and package artifact plus release metadata;
- a release-wide SPDX JSON software bill of materials generated with Syft;
- a keyless Sigstore signature bundle for `SHA256SUMS`;
- signed GitHub SLSA provenance and SBOM-attestation bundles.

The raw archives are the immutable inputs for package assembly, which never
invokes `go build`. Package-manager channels must promote the resulting package
bytes and checksums rather than rebuilding Topo themselves. See
[package artifacts and lifecycle](packages.md) and
[package-manager distribution](distribution.md).

## Reproducible build contract

`scripts/build-release.sh` exports the committed tree into two different
absolute source paths, runs `internal/releasetool` independently in each, and
requires every output byte to match before retaining either build. The tool:

- builds with `CGO_ENABLED=0`, `-trimpath`, and `-buildvcs=false`;
- injects only the semantic version, never the current time or runner path;
- fixes archive timestamps, uid/gid, modes, entry order, and gzip headers;
- emits archives and checksum lines in a fixed target/name order;
- refuses an existing output directory so stale files cannot enter a release.

The excluded VCS stamp is not a loss of traceability: the explicit source
commit is in `release-metadata.json`, and the signed provenance binds every
archive digest to the tagged repository commit and workflow invocation.

ServiceNow SDK `pack` output carries changing ZIP metadata. The release build
runs the pinned SDK twice, validates every package-inventory digest plus the
exact Topo app contract, canonicalizes ZIP metadata/order and the SDK-generated
BOM serial/timestamp, regenerates the inventory digests, and requires the two
normalized packages and metadata files to match byte-for-byte. It does not
rewrite application tables, ACLs, routes, scripts, navigation, or other
functional metadata.

To reproduce a published release locally, use its tag and the compiler recorded
in its authenticated `release-metadata.json`. The following reproduces raw archives from the published 0.4.6 worker tag.
It does not produce release signatures or signed native packages:

```sh
git checkout v0.4.6-beta.1
GOTOOLCHAIN=go1.26.9 scripts/build-release.sh \
  v0.4.6-beta.1 "$(git rev-parse HEAD)" dist-local linux-homebrew-beta
```

For a published release, compare reproduced raw-archive digests with its signed
manifest. Native signing changes package bytes; unsigned local package checksums
are not final signed-package checksums.
The build needs network access only when the pinned Go modules are not already
in the local module cache.

For the Homebrew beta, pass `linux-homebrew-beta` as the fourth argument and use
the exact prerelease tag. Use `linux-macos-beta` only for the Apple-signed
variant. Omitting the profile builds all six historical targets. Both restricted
profiles reject stable tags and unexpected Windows artifacts; unknown profiles
are rejected.

## Verify a downloaded release

Choose a **published worker release** with complete binary and verification
assets. Do not substitute a source-only tag or a ServiceNow XML release. With
GitHub CLI, `jq`, Cosign, and `sha256sum` installed, download the Linux amd64
archive and matching verification files into a new directory. The example uses the published `v0.4.6-beta.1` worker; when checking another
version, substitute its exact tag and stop if its assets are absent.

```sh
(
  set -eu
  tag=v0.4.6-beta.1
  repo=Nischoy-ai/topo
  version=${tag#v}
  archive="topo_${version}_linux_amd64.tar.gz"
  bundle="topo_${version}_checksums.sigstore.json"
  gh release view "$tag" --repo "$repo" --json isDraft,publishedAt,assets > release.json
  jq -e --arg archive "$archive" --arg bundle "$bundle" '
    .isDraft == false and .publishedAt != null and
    ([.assets[].name] | index($archive) != null and
      index($bundle) != null and index("SHA256SUMS") != null)
  ' release.json >/dev/null
  gh release download "$tag" --repo "$repo" \
    --pattern "$archive" --pattern SHA256SUMS --pattern "$bundle"
  cosign verify-blob \
    --bundle "$bundle" \
    --certificate-identity \
      "https://github.com/$repo/.github/workflows/release.yml@refs/tags/$tag" \
    --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
    SHA256SUMS
  awk -v name="$archive" '$2 == name {print; found=1} END {if (!found) exit 1}' \
    SHA256SUMS > archive.sha256
  sha256sum --check archive.sha256
  gh attestation verify "$archive" --repo "$repo"
)
```

On macOS replace `sha256sum --check archive.sha256` with
`shasum -a 256 -c archive.sha256`. Do not execute an archive before these checks
pass. `SHA256SUMS --ignore-missing` alone is insufficient: the command above
requires the selected archive to be listed and present.

The bundle contains the short-lived signing certificate, signature, and public
transparency-log proof; Topo keeps no long-lived general release-signing key in
GitHub Actions. The identity and issuer checks are essential—verifying only
that *someone* used Sigstore is not sufficient.

GitHub stores the provenance and SBOM attestations through its attestation API;
the release also retains their Sigstore bundles so the evidence is downloadable
beside the artifacts. See GitHub's
[artifact-attestation verification guide](https://docs.github.com/en/actions/how-tos/secure-your-work/use-artifact-attestations/use-artifact-attestations)
and Sigstore's
[CI identity verification guidance](https://docs.sigstore.dev/quickstart/quickstart-ci/)
for the trust semantics of those commands.

## Maintainer release procedure

1. Merge a green release-preparation PR to `main` and choose a semantic version.
2. Create and push one immutable semantic tag at that exact `main` commit.
3. Confirm the tagged commit's `main` CI run is green. The tag workflow verifies
   the commit is reachable from `origin/main`, reproduces the release archive
   and package sets, exercises native package lifecycles, requires and verifies
   OpenPGP signatures on RPMs, plus Authenticode and Developer ID/notarization
   when required by the selected profile. The Homebrew-only profile requires
   successful installation/local discovery/uninstall on both Mac architectures
   instead of Apple signing. A failed required signer or test blocks release. It
   refreshes metadata for those final signed bytes, creates the SBOM/signatures/
   attestations, verifies them, and only then creates the GitHub Release with
   all evidence in one upload. Persistent native key material is isolated from
   ordinary build jobs in the protected `native-package-signing` environment;
   Windows signing instead uses a short-lived GitHub OIDC token and a
   non-exportable Azure Artifact Signing profile.
4. Verify one archive independently with both commands above before promoting
   the release to any package repository.

Do not create a replacement release for an existing tag or overwrite a release
asset. Correct a bad release with a new version. GitHub environment protection
or tag-protection rules should restrict who may create release tags.

## Scope boundary

The Sigstore checksum signature and GitHub attestations authenticate the full
final artifact set across platforms. Native signing additionally covers RPM,
MSI, and (for Apple-signed profiles) macOS trust. The Homebrew beta has no Apple
publisher identity or notarization ticket; Go's ARM64 ad-hoc signature is not
publisher authentication. Browser-downloaded raw macOS binaries can be blocked
by Gatekeeper; this beta promises the tested Homebrew CLI path, not a GUI/cask
or direct-download launch experience. Do not disable Gatekeeper or strip
quarantine to install it. Release evidence does not replace signed APT/RPM repository
metadata or repository-key rotation; protected package promotion adds those
controls. The current beta promotion evidence is recorded in [distribution](evidence/distribution.md#046-beta-worker);
stable/N-1 promotion and independent security retest have their own acceptance
requirements in [the roadmap](../ROADMAP.md) and [review record](security-review.md).
