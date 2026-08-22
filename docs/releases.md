# Release artifacts and verification

Topo releases are built only from semantic tags (`vMAJOR.MINOR.PATCH`, with an
optional prerelease suffix) whose commit is already reachable from `main`.
`.github/workflows/release.yml` uses the exact Go 1.23.12 toolchain and
commit-pinned actions. It creates one GitHub Release containing:

- deterministic raw archives for Linux, macOS, and Windows on amd64 and arm64;
- `release-metadata.json`, recording the source commit, toolchain, build flags,
  target matrix, and each archive's SHA-256 digest;
- `SHA256SUMS` for every archive and the release metadata;
- an SPDX JSON software bill of materials generated with Syft;
- a keyless Sigstore signature bundle for `SHA256SUMS`;
- signed GitHub SLSA provenance and SBOM-attestation bundles.

These raw archives are the immutable inputs for the later DEB, RPM, MSI/MSIX,
Homebrew, Helm, offline-bundle, and package-repository slices. Those channels
must promote the same bytes and checksums rather than rebuild Topo themselves.

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

To reproduce a release locally:

```sh
git checkout v0.1.0
GOTOOLCHAIN=go1.23.12 scripts/build-release.sh \
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
   set, creates the SBOM/signatures/attestations, verifies them, and only then
   creates the GitHub Release with all evidence in one upload.
4. Verify one archive independently with both commands above before promoting
   the release to any package repository.

Do not create a replacement release for an existing tag or overwrite a release
asset. Correct a bad release with a new version. GitHub environment protection
or tag-protection rules should restrict who may create release tags.

## Scope boundary

The Sigstore checksum signature and GitHub attestations authenticate the raw
release artifacts across platforms. They do not replace native repository or
installer trust: APT/RPM OpenPGP keys, macOS code signing/notarization, Windows
Authenticode, and repository key-rotation procedures belong to the package and
distribution slices that consume these archives. No production-readiness claim
is made by this release-evidence slice alone.
