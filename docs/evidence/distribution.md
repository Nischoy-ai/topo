# Package-channel validation evidence

Historical evidence captured from source commit
`5439ef5b441e1145172190db94aa24ba4d809599`. Versions, dates and test
scopes below identify the tested artifacts; they are not installation steps.

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
