# Release artifacts and verification

Topo releases are built only from semantic tags (`vMAJOR.MINOR.PATCH`, with an
optional prerelease suffix) whose commit is already reachable from `main`.
`.github/workflows/release.yml` uses the exact Go 1.26.8 toolchain and
commit-pinned actions. It creates one GitHub Release containing:

- deterministic raw archives for Linux, macOS, and Windows on amd64 and arm64;
- DEB and OpenPGP-signed RPM packages for Linux amd64/arm64,
  Authenticode-signed MSI installers for Windows amd64/arm64,
  Developer-ID-signed/notarized macOS archives, a Helm chart, a validated
  installable ServiceNow scoped-application ZIP, and a deterministic offline
  bundle;
- `release-metadata.json`, recording the source commit, toolchain, build flags,
  target matrix, and each archive's SHA-256 digest;
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

To reproduce a release locally:

```sh
git checkout v0.1.0
GOTOOLCHAIN=go1.26.8 scripts/build-release.sh \
  v0.1.0 "$(git rev-parse HEAD)" dist-local
```

Compare `dist-local/SHA256SUMS` with the manifest downloaded from the release.
The build needs network access only when the pinned Go modules are not already
in the local module cache.

## Verify a downloaded release

Download one archive plus `SHA256SUMS` and its Sigstore bundle from the same
GitHub Release. First verify ordinary content integrity:

```sh
sha256sum -c SHA256SUMS --ignore-missing
```

On macOS, the equivalent is `shasum -a 256 -c SHA256SUMS` after downloading
all files named by the manifest.

Then verify that the Nischoy Topo tag workflow signed the checksum manifest:

```sh
cosign verify-blob \
  --bundle topo_0.1.0_checksums.sigstore.json \
  --certificate-identity \
    'https://github.com/Nischoy-ai/topo/.github/workflows/release.yml@refs/tags/v0.1.0' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  SHA256SUMS
```

The bundle contains the short-lived signing certificate, signature, and public
transparency-log proof; Topo keeps no long-lived general release-signing key in
GitHub Actions. The identity and issuer checks are essential—verifying only
that *someone* used Sigstore is not sufficient.

Finally, verify GitHub's signed build provenance for the archive itself:

```sh
gh attestation verify topo_0.1.0_linux_amd64.tar.gz \
  --repo Nischoy-ai/topo
```

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
   OpenPGP signatures on RPMs, Authenticode signatures on both MSIs, and
   Developer ID signatures plus notarization on both macOS payloads. It
   refreshes metadata for those final signed bytes, creates the SBOM/signatures/
   attestations, verifies them, and only then creates the GitHub Release with
   all evidence in one upload. All native keys live in the protected
   `native-package-signing` environment, not ordinary build jobs.
4. Verify one archive independently with both commands above before promoting
   the release to any package repository.

Do not create a replacement release for an existing tag or overwrite a release
asset. Correct a bad release with a new version. GitHub environment protection
or tag-protection rules should restrict who may create release tags.

## Scope boundary

The Sigstore checksum signature and GitHub attestations authenticate the full
final artifact set across platforms. Native signing additionally covers RPM,
MSI, and macOS trust. That evidence does not replace signed APT/RPM repository
metadata or repository-key rotation; protected package promotion adds those
controls. No production-readiness claim is made before a real beta, a real N-1
stable promotion, and the external security review.
