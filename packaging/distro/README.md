# Distribution repository submission preparation

Status on **2026-10-16**: Fedora-first packaging validation in progress. Topo has not been submitted to or
accepted by Debian, Ubuntu or Fedora in this work. The existing signed Nischoy
APT/RPM repositories remain unchanged. Installing from those repositories does
not establish availability in a distribution's default repositories.

The requested eventual experience is `sudo apt install topo` or
`sudo dnf install topo` using the distribution's enabled repositories. Those
commands become valid default-repository instructions only after the relevant
distribution accepts, builds and publishes the package. Repository inclusion
does not configure ServiceNow, target authorization, credentials or trust.

## Source and prepared artifacts

- Readiness baseline: main `d55f3e1abd8768f4442b008507999c25e72df1fb`.
- Published worker source: `c70c5813895fcae20c54ee1fac54e0e44e9e531f`, tag
  `v0.4.7-beta.1`. Preserve that prerelease tag in source/version metadata.
- `audit-debian.py` reproducibly maps all 66 declared `go.mod` modules to a
  supplied Debian index snapshot. It does not resolve or compile dependencies.
- `debian-sid-2026-10-15.json` records module pins, actual provider package
  metadata, index hashes and unmatched paths. Package versions for multi-module
  SDKs are not assumed to equal module versions.
- `fedora-rawhide-2026-10-15.json` records a public Rawhide package metadata
  snapshot, including compiler and packaging tools. Its compressed primary
  checksum matches the downloaded `repomd.xml` entry.
- `topo.spec.in` is a **Fedora source-build draft**, based on the Go SIG vendor
  template. Its complete cumulative license, source-pinned vendor archives and maintainer changelog are implemented; native acceptance results remain required. It now includes the CLI, matching worker/agent units, dynamic sysusers and
  Fedora lifecycle macros; native build/lint/lifecycle validation remains a gate.
- `go-vendor-tools.toml` supplies explicit vendor-generation input with no
  dependency overrides or license exclusions.
- `submission-fields.json` records known upstream facts and the owner-confirmed
  public maintainer, Soumik Sarkar <soumiks@gmail.com>. Sponsor, account, ticket
  and artifact URLs remain unset; no downstream status is inferred.

The separate new `topo worker setup` work is unpublished and cannot be attributed
to the existing 0.4.7 worker. `prepare-fedora.py` renders a commit-pinned,
unsigned review snapshot, verifies downloaded source against every public Git
blob and rejects unresolved tokens or license differences. It does not upload
or publish anything. Existing tags and released asset bytes remain unchanged.

Native Fedora 44 and Rawhide source packaging is tested on disposable hosted
amd64/arm64 runners. The upstream signed-release compiler stays at Go 1.26.9;
the explicitly qualified Fedora matrix adds the distro Go 1.27.2 compiler.
`GOTOOLCHAIN=local` prevents a package build from downloading a compiler.
No workstation Docker, installed Topo or live lab is used.

`check-preparation.py` remains a static metadata check, not a native build
verdict. Its dated `preparation-checks.json` reports the earlier preparation
state. Real results must be read from the exact source snapshot's CI artifacts.

## Debian: dependency closure before an uploadable source package

The [Debian Go team's instructions](https://go-team.pages.debian.net/packaging.html)
use `dh-make-golang`, `dh-golang`, declared distro Build-Depends and source
libraries in `/usr/share/gocode/src`. The binary package can be named after the
application without a `golang-` prefix. A generated scaffold alone is not a
working Debian source package.

Fresh sid/main amd64 metadata, downloaded over HTTPS on this date, reports:

| Requirement | Current provider | Readiness |
| --- | --- | --- |
| Go 1.26.9 | `golang-1.26-go` 1.26.9-1 | Available; explicitly select `/usr/lib/go-1.26/bin`, because `golang-go` defaults to 1.27. |
| x/crypto 0.57.0 | `golang-golang-x-crypto-dev` 1:0.57.0-2 | Version available. |
| x/net 0.60.0 | `golang-golang-x-net-dev` 1:0.60.0-1 | Version available. |
| govmomi 0.52.0 | `golang-github-vmware-govmomi-dev` 0.24.2-4 | Provider is below the required module version. |
| AWS SDK root 1.43.7 | `golang-github-aws-aws-sdk-go-v2-dev` 1.41.7-2 | Provider is below the required root-module version; nested SDK modules need separate checks. |
| smithy-go 1.27.8 | `golang-github-aws-smithy-go-dev` 1.25.1-3 | Below the declared module version. |
| MS authentication library 1.7.2 | `golang-github-azuread-microsoft-authentication-library-for-go-dev` 1.0.0-3 | Below the declared module version. |
| modernc SQLite 1.39.0 | `golang-modernc-sqlite-dev` 1.40.0+dfsg-2 | Different/repacked source; compatibility and source review untested. |

These are version/availability observations, not assertions that older distro
packages are vulnerable. Preserve Topo's security pins; do not rewrite `go.mod`
to make an untested build pass. Dependency updates/new library packages need
their own maintainers and review before an unbundled build can satisfy them.

The module-root audit found 57 exact `go.mod` providers, six additional source
file candidates (including two with stripped major-version paths), and three
unresolved import roots: `github.com/ncruces/go-strftime`,
`gopkg.in/evanphx/json-patch.v4` and `gopkg.in/yaml.v3`. An index miss does not
prove the library is absent: aliases, symlinks and test-only dependencies require
inspection. Direct NTLM's source provider exists but is a 2022 snapshot rather
than the pinned `v0.1.1`. Resolve these paths and all provider-version differences
before generating a complete `debian/control`; do not claim dependency closure.

Index sources:
[Packages.xz](https://deb.debian.org/debian/dists/sid/main/binary-amd64/Packages.xz)
and [Contents-all.gz](https://deb.debian.org/debian/dists/sid/main/Contents-all.gz).
The JSON records their exact SHA-256 values. These HTTPS snapshots are not
signature-verified apt metadata and may have different generation times. Cached
package web pages showed older compiler/crypto versions and were not used as
the authoritative current version observation.

Reproduce after downloading those two public files into a disposable directory:

```sh
python3 packaging/distro/audit-debian.py \
  --go-mod go.mod --packages /path/to/Packages.xz \
  --contents /path/to/Contents-all.gz \
  --base-url https://deb.debian.org/debian/dists/sid/main/ \
  --source-commit d55f3e1abd8768f4442b008507999c25e72df1fb
```

The exact `topo` binary name was absent from this main index. This is not
clearance across all suites, components, binary file ownership or existing
requests. Before filing, check WNPP and existing packages for duplicate effort.
[WNPP](https://www.debian.org/devel/wnpp/) describes the ITP fields: package,
version, upstream author/contact, source URL, license and description. The owner
confirmed Soumik Sarkar <soumiks@gmail.com> as the public maintainer contact.
This does not establish sponsorship or Debian Go team maintenance.

After dependency closure, generate packaging with `dh-make-golang`, retain
UNRELEASED while preparing it, complete copyright/source metadata, build with
`sbuild`, run lintian/autopkgtest and lifecycle checks on disposable builders,
then have the source package reviewed. [Debian Mentors](https://mentors.debian.net/intro-maintainers/)
requires a source package and configured signing identity for uploads; sponsor
review and Debian NEW acceptance remain external gates. No key is provisioned
by this preparation.

## Ubuntu: follow the Debian source path

[Ubuntu's new-package process](https://ubuntu.com/project/docs/how-ubuntu-is-made/processes/new-packages/)
prefers Debian first. New Debian unstable packages can sync before Debian Import
Freeze; later requests need explicit sync handling and review. A direct Ubuntu
request uses `needs-packaging`, source/license details and a linked Debian WNPP
issue. This does not guarantee publication in current stable/LTS versions or in
Ubuntu's `main` component. A PPA is not the requested default archive inclusion.

Target Ubuntu release/component, package-name clearance, full dependency closure
and exact 1.26.9 compiler availability are not yet established. The
[official Go availability table](https://ubuntu.com/developers/docs/reference/availability/go/)
lists 1.26 for 26.04 but older compiler families for older releases; it is not
proof of the precise patch in a build pocket. Verify signed target-suite
metadata and clean source builds rather than lowering the required compiler.

## Fedora: vendored source build and license review

The [current Go SIG go2rpm project](https://gitlab.com/fedora/sigs/go/go2rpm)
states that Fedora changed to vendored Go packaging with Fedora 43. Do not use
the old unbundled-by-default wiki as current policy. The
[vendor template](https://gitlab.com/fedora/sigs/go/go2rpm/-/raw/main/go2rpm/templates/vendor.spec)
and [Go Vendor Tools scenarios](https://fedora.gitlab.io/sigs/go/go-vendor-tools/scenarios/)
provide the source/vendor archive and license-report workflow. Generated license
results still require maintainer review; `Apache-2.0` alone is not the combined
license expression for all code statically linked into Topo.

The October 15 Rawhide metadata snapshot contains Go 1.27.2-1.fc46,
go2rpm 1.19.0-3.fc45 and go-vendor-tools 0.13.0-1.fc45, with no exact `topo`
package. This is not release-wide name clearance. Native Fedora 44 additionally
provides Go 1.26.9 with Fedora's `nodwarf5` build variant. The compiler matrix is
explicit in `ROADMAP.md`; unknown versions fail rather than downloading a new
compiler or changing upstream dependency pins.

`prepare-fedora.py --commit FULL_PUBLIC_SHA --output NEW_DIRECTORY` generates
`SOURCES`, `SPECS` and reports. It verifies all downloaded source against the
Git tree, uses official Go Vendor Tools with the Askalono detector, preserves
module files and adds the fully attributed license notices. Every license file
is checked, including hash-pinned manual classifications for composite texts.
See [license review](fedora-license-review.md) for source/header/artwork details.

The native workflow builds an unsigned source RPM and uses `mock` with an
offline build phase. The spec retains Fedora Go flags, runs `%gotest` and the
vendor license check, installs a man page and both reviewed units, and uses
sysusers plus standard systemd macros. Separate disposable systemd containers
exercise actual RPM install, protected worker setup, a higher-release RPM
upgrade and removal against a synthetic loopback server. The higher release is
a CI transaction fixture of the same code, not a second product release.
Native vet/race/vulnerability checks and rpmlint are required as well.
No actual ServiceNow, target, reboot or production-readiness proof follows.

The [submission and account checklist](fedora-submission.md) contains the
prepared review text and current official process. After successful local/CI
qualification, public Copr hosting, a Fedora username, review and sponsorship
are still required. No external request has been filed.

## Source preparation follow-up

The full-CLI candidate now has a reproducible 66-module vendor archive and
network-disabled Linux amd64/arm64 cross-build evidence. No module pins changed.
See [source preparation](fedora-source-preparation.md) and its exact receipts.
The historical initial license report failed on three files after three checksum-pinned
composite-license classifications; [license review](fedora-license-review.md)
records each one. These are not Fedora source-RPM builds or acceptance.
The subsequent classification and native packaging work supersedes those initial gaps; final native results must be read separately.

## Remaining gates

| Gate | Current owner/input |
| --- | --- |
| Public maintainer name/email and sustained downstream maintenance | Soumik Sarkar <soumiks@gmail.com> confirmed; downstream maintenance/sponsor arrangements remain open. |
| Debian dependency updates, aliases and complete source package | Packaging work plus downstream library maintainers/sponsor. |
| Fedora compiler policy, vendor/license archive, service parity and clean builds | Packaging work plus approved target/toolchain decision. |
| Distro reviewer/sponsor and archive acceptance | External distribution decisions, not promised dates. |
| Ubuntu release/component and sync/backport route | Downstream maintainers and archive policy. |

This packaging preparation does not change the current published install path,
signed releases, repositories, signing policy or supported discovery scope.

The October 16 follow-up completes those classifications and retains full musl,
artwork and copied-documentation notices; its local report passes without
exclusions. Native source-RPM/mock/lint/lifecycle validation is in progress.
Earlier dated receipts remain historical. The source snapshot spec now owns
matching units, sysusers and a man page; the complete expression is populated.
