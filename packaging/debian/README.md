# Debian and Ubuntu archive preparation

This is an **unreleased, unqualified review candidate**, not a default-repository
package or a replacement for the signed Nischoy APT channel. The full CLI is
retained. The source input is public commit
`3adbdb36ad886396ad6267cd706c84bea6d35b99`, separately qualified for Fedora;
that does not qualify its Debian dependencies. Fedora's filed SRPM stays intact.

Debian packaging uses archive-provided Go libraries rather than Fedora's vendor
archive. `dependency-map.json` records candidates for all 66 declared modules;
package names, import roots and versions must be checked against each archive.
The October 15 preliminary index audit found older AWS/Azure/govmomi libraries
and three unresolved roots. Candidate names are not dependency closure.

The `debian/` directory contains a `3.0 (quilt)` full-CLI source package, native
Go/debhelper dependencies, a manpage, unchanged reviewed systemd units, separate
service users and an installed CLI smoke test. Installation neither enables nor
starts discovery. `debian/copyright` is explicitly pending its complete DEP-5
inventory. This candidate must not be uploaded as a finished package.

## Reproduce the audit

Use the `debian-ubuntu-archive-audit` workflow on disposable GitHub runners. It
pins official Debian unstable and Ubuntu 26.10/26.04 image digests, uses normal
signature-verified APT repositories, saves their signed InRelease roots, installs
available candidate libraries and records exact installed package/import paths. Known single-module package
minimums include the unchanged x/crypto and x/net security baselines; nested
AWS/Azure module compatibility still requires source-level review.

`native-check.sh` produces an unsigned source candidate with `prepare-source.py`,
runs `dpkg-checkbuilddeps`, then attempts a diagnostic build with all external
network access denied by a network namespace and Go proxy/checksum lookups off.
The diagnostic uses `-d` to expose import/API failures even when declared
Build-Depends are missing. **It is not a clean build or an acceptance gate.**
`STATUS.json` always keeps `upload_ready: false`; a green audit job means that
results were collected, including failures. No missing dependency or security pin
is silently replaced, bundled or downgraded. No code executes against real targets.

Once dependencies and licensing are closed, qualification requires a fresh
`sbuild` without `-d`, full tests/race/vulnerability checks using the chosen archive
libraries, Lintian, autopkgtest and disposable systemd install/setup/upgrade/removal
checks. Those gates have not passed for this candidate. The same-source package
transaction test must not be confused with deferred release/N-1 upgrade evidence.

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
