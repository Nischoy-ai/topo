# Debian and Ubuntu archive preparation

This is an unsigned review candidate for the full Topo CLI, using official
Debian archive libraries. It is not accepted into Debian or Ubuntu's default
repositories. The pinned public upstream input is
`3adbdb36ad886396ad6267cd706c84bea6d35b99`; original Go sources and module
manifests remain unchanged. Fedora's submitted sources stay intact.

On 2026-10-18 the owner directed this candidate to use the official NTLM,
Azure SDK and MSAL packages despite the recorded archive security findings.
The qualification path no longer builds or injects three separate dependency
updates. `prepare-ntlmssp.py`, `prepare-msal.py` and `prepare-azure.py` remain
historical review tooling and are not called by the current qualification.

Debian packaging uses module-aware `dh-go >= 1.69`, a native Go 1.26.9/1.27.2
compiler, and unbundled library source. Legacy go-flowrate module metadata and
nested json-patch/v5 mappings are repaired in private build copies. Native
compiler evaluation does not change the upstream signed-release baseline.
`dependency-compatibility.md` records archive versions and evidence by scope;
`dependency-map.json` retains upstream requirements for reference, not a claim
that each is a minimum compatible Debian API version.

The package retains DEP-5 coverage, complete actual linked-source/runtime
notices, Static-Built-Using, reviewed units and separate service users. Initial
installation does not enable or start discovery. Qualification still requires
clean native build/test, package/lifecycle checks and security-analysis results.
The owner-accepted findings are reported as findings, not marked fixed or clean.

## Reproduce the audit

Use the `debian-ubuntu-archive-audit` workflow on disposable GitHub runners. It
pins official Debian unstable and Ubuntu 26.10/26.04 image digests, uses normal
signature-verified APT repositories, saves their signed InRelease roots, installs
available candidate libraries and records exact installed package/import paths. Known single-module package
minimums include the unchanged x/crypto and x/net security baselines; nested
AWS/Azure module compatibility still requires source-level review.

`native-check.sh` produces an unsigned source candidate with `prepare-source.py`,
runs `dpkg-checkbuilddeps`, then attempts a diagnostic build with all external
network access denied by a network namespace (loopback enabled for local protocol
simulators) and Go proxy/checksum lookups off. Tests use dh-golang’s own build
environment across every Topo package.
The diagnostic uses `-d` to expose import/API failures even when declared
Build-Depends are missing. **It is not a clean build or an acceptance gate.**
`STATUS.json` always keeps `upload_ready: false`; a green audit job means that
results were collected, including failures. No missing dependency or security pin
is silently replaced, bundled or downgraded. No code executes against real targets.

The `debian-native-qualification` workflow builds the complete CLI in fresh
AMD64 and ARM64 `sbuild` chroots. Dependencies come only from normal signed
Debian unstable repositories: no local library DEBs, `--extra-package` injection,
vendor archive or network-enabled build phase. Build phases run as a nonroot
account with only loopback networking for local protocol simulators.

Native vet/race tests, Topo Lintian, installed-package autopkgtests and real
systemd install/setup/same-source revision/remove/purge/cleanup remain required.
The explicit simulator performs no real ServiceNow discovery. Historical
release/N-1 upgrade, reboot and live estate compatibility are not implied.

Security analysis runs unmodified pinned govulncheck over byte-verified native
sources and actual installed versions. For this Debian review candidate only,
`GO-2024-2918` at azidentity `v1.5.0-beta.3` and `GO-2026-5543` at the NTLM
`754e69321358` snapshot are owner-accepted on 2026-10-18. They remain in logs and
`security-result.json`; `BUILD-RESULT.json` records a findings-accepted status
and a false clean-security boolean when present. Tool failures, mismatched
source versions and other reachable findings still fail qualification. This
exception does not weaken upstream or Fedora security/release gates.

Source/audit CI success is evidence collection, not archive qualification.
Any previous successful three-dependency-update matrix is historical and cannot
qualify this changed archive-only input. Exact-head results are required.

## Submission route

1. Check Debian WNPP, Mentors, Salsa and Ubuntu's existing package requests for a
   duplicate. File a Debian ITP for Topo; link any dependency-update/new-package
   work if needed rather than suggesting the full package is already upload-ready.
2. Complete dependency packaging and DEP-5 licensing, then clean package tests.
3. Soumik signs the source upload with his own OpenPGP key registered at Debian
   Mentors. A Debian Developer sponsor reviews it and uploads to Debian NEW;
   archive administrators decide acceptance. Team maintenance requires agreement,
   so this candidate does not claim Debian Go Team membership or a Salsa team repo.
4. Link Ubuntu's `needs-packaging` request to the Debian ITP. Prefer the Debian
   unstable import/sync route into an open Ubuntu development series. Ubuntu
   26.10 was released October 15, 2026; acceptance into that finished release is
   not promised. A PPA is not inclusion in the default archive.

The owner has no Mentors/Salsa/Launchpad accounts or signing key yet. Accounts,
email verification and personal key custody are owner actions; private keys,
passwords and codes must stay out of chat and Git. Source uploads and sponsorship
requests are pending, not filed or accepted.

References:
- [Debian Go packaging](https://go-team.pages.debian.net/packaging.html)
- [Debian Mentors maintainer guide](https://mentors.debian.net/intro-maintainers/)
- [Debian WNPP](https://www.debian.org/devel/wnpp/)
- [Ubuntu new-package process](https://ubuntu.com/project/docs/how-ubuntu-is-made/processes/new-packages/)

The service maintainer scripts preserve an already running worker during package
upgrades (`--no-stop-on-upgrade`) and never enable/start it on initial install.
An operator must explicitly restart the service when ready to use a newly
installed executable. Package removal still stops its services and retains
operator configuration and service identities. The lifecycle fixture tests a
same-source higher package revision; it does not establish release/N-1 upgrade
compatibility or reboot behavior.
