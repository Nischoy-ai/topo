# Debian and Ubuntu archive preparation

This is an **unreleased, unqualified review candidate**, not a default-repository
package or a replacement for the signed Nischoy APT channel. The full CLI is
retained. The source input is public commit
`3adbdb36ad886396ad6267cd706c84bea6d35b99`, separately qualified for Fedora;
that does not qualify its Debian dependencies. Fedora's filed SRPM stays intact.

Debian packaging uses archive-provided Go libraries rather than Fedora's vendor
archive. `dependency-map.json` records candidates for all 66 declared modules;
package names, import roots and versions must be checked against each archive.
The preliminary index audit was followed by actual offline builds.
`dependency-compatibility.md` records the archive versions exercised, metadata
repairs, and independently packaged library updates required by qualification.

Native downstream Go 1.26.9/1.27.2 evaluation is staged in `ROADMAP.md`;
this does not change the upstream signed-release compiler baseline. Ubuntu
26.10 has a 1.27 toolchain to evaluate; its older 1.26.8 is not a fallback.

The candidate requires module-aware `dh-go >= 1.69` and explicitly enables
modules; legacy `dh-golang`/GOPATH mode changes Go runtime defaults, including
the HTTP mux behavior Topo’s controllers and simulators require. No runtime
compatibility knob is used to hide that mismatch. The source Lintian override
is limited to its obsolete addon-provider check: `dh-go` supplies that addon,
but current Lintian still insists on legacy `dh-golang`. Other lint results
and all dependency/build gates remain visible.

The `debian/` directory contains a `3.0 (quilt)` full-CLI source package, native
Go/debhelper dependencies, a manpage, unchanged reviewed systemd units, separate
service users and an installed CLI smoke test. Installation neither enables nor
starts discovery. `debian/copyright` now covers the full pinned source inventory and retained legal
documents. Complete actual linked-library and Go runtime notices are collected
into the DEB; see `copyright-review.md`. Qualification is still required before
this candidate can be uploaded as a finished package.

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

The `debian-native-qualification` workflow creates independent clean `sbuild`
chroots on AMD64 and ARM64. Build phases run without root or external networking;
loopback remains available for protocol simulators. It prepares separate NTLM,
MSAL and Azure SDK source-update candidates, builds each before Topo, and supplies
their DEBs using `sbuild --extra-package`. Remaining dependencies come from the
normal signed Debian unstable archive. No Fedora vendor archive is used.

The Azure update replaces complete azidentity1.6.0, azcore1.11.1 and internal1.8.0
modules and requires MSAL1.2.2; it addresses GO-2024-2918 found in the archive's
azidentity1.5.0-beta.3. NTLM0.1.1 retains its malformed-challenge fix and uses the
system x/crypto MD4 implementation. These are library-maintainer review candidates,
not updates already accepted into Debian. Checksums and full license notices are
retained with each source package.

Qualification requires native vet/race tests, actual-version vulnerability
analysis, Lintian, installed-package autopkgtest and real systemd install/setup/
revision/removal tests. The security check uses unmodified pinned govulncheck and
a frozen official vulnerability database against byte-verified native sources;
local replacement paths must not hide library identities. The disposable systemd
fixture only registers against a synthetic loopback server and receives no work.

The same-source higher-revision package transaction is distinct from historical
release/N-1 upgrade evidence. Only final exact-head results qualify a candidate;
source-audit success or intermediate builds do not substitute for those results.

## Submission route

1. Check Debian WNPP, Mentors, Salsa and Ubuntu's existing package requests for a
   duplicate. File a Debian ITP for Topo; link any dependency-update/new-package
   work rather than suggesting the full package is already upload-ready.
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
