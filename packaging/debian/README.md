# Debian packaging

`debian/` here is the packaging for the `topo` Debian source package. It
builds the `topo` command against libraries from Debian unstable, with no
vendored dependencies. The services are installed but not enabled or started.

The ITP is [#1151249](https://bugs.debian.org/1151249).

## Building

The upstream tarball comes from the `v<version>` tag that matches
`debian/changelog`:

    python3 packaging/debian/prepare-source.py --output /tmp/topo-source
    sbuild -d unstable /tmp/topo-source/topo_*.dsc

Set `TOPO_UPSTREAM_REF` to build from another commit before a release is tagged.

## CI

- `debian-native-qualification` (`qualify.sh`): sbuild in a Debian unstable
  chroot with no network during the build, then Lintian, autopkgtest, a
  govulncheck scan of the linked sources and a systemd install/remove test.
  Scripts used only by CI live in `ci/`, not in `debian/`.
- `debian-ubuntu-archive-audit` (`native-check.sh`): checks which build
  dependencies are available in Debian unstable and Ubuntu.

## Open items before upload

- Known vulnerabilities in Debian library versions that topo links:
  `GO-2026-5543` in golang-github-azure-go-ntlmssp
  (0.0~git20221128.754e69321358-2) and `GO-2024-2918` in the azidentity module
  of golang-github-azure-azure-sdk-for-go (68.0.0+git20231220.f497335+dfsg1-7).
  These need to be fixed in those packages, working with the Debian Go team.
- `debian/rules` carries two workarounds for Debian library metadata: the
  go-flowrate source has no `go.mod`, and client-go imports json-patch v5 while
  its `go.mod` lists v4. Both should be reported as bugs against those packages.
- `Architecture` is limited to amd64 and arm64, the only architectures tested.
- The packaging should move to Salsa under the Debian Go team.
