# Package-channel validation evidence

Versions, dates and test scopes below identify the tested artifacts; they are
not installation steps. Use the [installation guide](../distribution.md) for
current package commands.

## 0.4.6 Beta worker

The signed [v0.4.6-beta.1 worker](https://github.com/Nischoy-ai/topo/releases/tag/v0.4.6-beta.1)
was published on 2026-10-09 UTC from
`5f7aecf08cb9b66c8724f4b1d4f399decab31543`.
[Release run 37862256387](https://github.com/Nischoy-ai/topo/actions/runs/37862256387)
passed reproducible archive/package builds, protected RPM signing, Linux
package lifecycle and Homebrew execution/removal on Intel and Apple Silicon.

Independent downloads matched all 25 GitHub asset digests and every entry in
`SHA256SUMS`. Cosign verified the manifest against the exact tagged
`release.yml` identity and GitHub OIDC issuer. Linux amd64 and macOS arm64
provenance verified the source commit above; build information confirmed
Go 1.26.9, `x/crypto` 0.57.0 and `x/net` 0.60.0. The downloaded Mac binary
returned `v0.4.6-beta.1`. These checks authenticate the worker release;
public-channel installation is a separate gate. The
[security record](../security-review.md#binary-scanner-interpretation) retains
the binary scanner warning and its precise scope.

[Promotion 37865026033](https://github.com/Nischoy-ai/topo/actions/runs/37865026033)
passed protected preparation and publication reviews, signed-repository
installation/removal and both Homebrew architecture gates. It published package
commit `77e958e6e60170511b2f8ee9676775423c97cdc9` and tap commit
`9531659747f4fc5f7767261d3f092f33a1311428`. The
[Pages deployment](https://github.com/Nischoy-ai/topo-packages/actions/runs/37867623357)
completed. Public HTTPS APT metadata for both architectures and RPM metadata
for x86_64/aarch64 matched the promoted commit's bytes. The published formula
SHA-256 is `400c179507ceba775c107fb2e2b05b249e4fc8011455a22bdabf6a82332ec5bd`;
its archive URLs, checksums and version test identify `v0.4.6-beta.1`.

[Public-channel acceptance, attempt 2](https://github.com/Nischoy-ai/topo/actions/runs/37865681687/attempts/2)
checks ten fresh installations: APT/RPM on amd64/arm64 through both manual
repository setup and the Linux helper, plus Homebrew on Intel/Apple Silicon.
Each Linux check requires native signature verification, the exact version,
an installed binary matching the authenticated release archive, local
discovery, a dormant worker service and removal preserving an operator file.
Mac checks require the reviewed formula hash, audit, exact version, discovery
and removal. Consult the linked run for job conclusions. The first attempt
rejected the old channels before promotion; those failures are retained.
These are installation checks, separate from ServiceNow runtime acceptance.

## First beta operational evidence

The historical [v0.1.0-beta.1 source](https://github.com/Nischoy-ai/topo/tree/v0.1.0-beta.1)
was built from `57671b5407daabddd7ae08d14dd25395e0b9431f`.
[Release attempt 2](https://github.com/Nischoy-ai/topo/actions/runs/34929267383/attempts/2)
passed reproducible builds, Linux package lifecycle, protected RPM signing,
and Intel/ARM64 Homebrew installation, execution and removal. Independent
downloads matched the checksum manifest; Sigstore identity matched the tagged
release workflow, and Linux amd64/macOS arm64 provenance matched the repository
and source commit.
Its release downloads have since been withdrawn; the original tag and source
history remain. These results do not validate the newer worker.

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
[separately](../servicenow-validation.md).

Earlier promotion attempts stopped before APT/RPM/Homebrew publication:

| Run | Result and correction |
| --- | --- |
| [35243074007](https://github.com/Nischoy-ai/topo/actions/runs/35243074007) | Invalid Fedora image pin blocked the RPM gate; the pin and shell-command regression guards were corrected. |
| [35388277718](https://github.com/Nischoy-ai/topo/actions/runs/35388277718) | Homebrew audit failed; formula spacing, stanza order, version and conflict declarations were corrected. |
| [35467069999](https://github.com/Nischoy-ai/topo/actions/runs/35467069999) | OCI publication passed, but Git authentication blocked repository/tap publication; explicit GitHub CLI credential-helper setup was added. |


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
