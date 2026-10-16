# Fedora source and vendor preparation

This is build preparation for the full Topo CLI. It does not introduce a
worker-only executable, choose a new upstream release, produce a source RPM,
or establish Fedora repository availability.

## Measured candidate

Source `6a431a13b31fa10844cbf82d0cb0f12497974f7b` is an unreleased PR86
candidate containing guided worker setup. Exact Go 1.26.9 and official Go Vendor
Tools 0.13.0 generated a 66-module vendor archive. The default tidy operation
left both module files unchanged. Generating from two independently extracted
source paths produced identical archive bytes; the second pass had Go proxy
and checksum network access disabled.

The vendored tree cross-built Linux amd64 and arm64 with
`GOPROXY=off GOSUMDB=off CGO_ENABLED=0` and `go build -mod=vendor -trimpath`.
Those binaries were not executed. The host was macOS; these results are not
Fedora-native builds, mock/rpmlint results, distro install tests or target
compiler qualification.

Exact hashes, sizes, tool versions and outstanding gates are in
[fedora-source-preparation-2026-10-15.json](fedora-source-preparation-2026-10-15.json).
The source/vendor archives are preparation artifacts, not current customer
downloads. No binary archives are added to public Git.

## Reproduce source preparation

Use a disposable directory with exact Go 1.26.9, Go Vendor Tools 0.13.0 and
askalono available. The examples run packaging tools on a source copy only.
They do not install Topo or start a worker. Do not use the workstation Docker
installation or an existing deployed configuration.

First create two clean source copies from the exact candidate:

```sh
set -eu
TOPO_SOURCE_SHA=6a431a13b31fa10844cbf82d0cb0f12497974f7b
TOPO_SOURCE_CHECKOUT="$PWD"
TOPO_PREP_DIR="$(mktemp -d)"
git archive --format=tar "$TOPO_SOURCE_SHA" > "$TOPO_PREP_DIR/source.tar"
mkdir "$TOPO_PREP_DIR/first" "$TOPO_PREP_DIR/second"
tar -xf "$TOPO_PREP_DIR/source.tar" -C "$TOPO_PREP_DIR/first"
tar -xf "$TOPO_PREP_DIR/source.tar" -C "$TOPO_PREP_DIR/second"
export SOURCE_DATE_EPOCH="$(git show -s --format=%ct "$TOPO_SOURCE_SHA")"
export GOTOOLCHAIN=local
export GOMODCACHE="$TOPO_PREP_DIR/modcache"
export GOCACHE="$TOPO_PREP_DIR/gocache"
test "$(go env GOVERSION)" = go1.26.9
```

Generate sources with the reviewed packaging configuration. Initial preparation
may download module sources through the Go proxy/checksum service. It must not
change dependency pins or discard tests to make the closure smaller.

```sh
go_vendor_archive create \
  --config "$TOPO_SOURCE_CHECKOUT/packaging/distro/go-vendor-tools.toml" \
  --output "$TOPO_PREP_DIR/first-vendor.tar.bz2" "$TOPO_PREP_DIR/first"
cmp "$TOPO_PREP_DIR/first/go.mod" "$TOPO_PREP_DIR/second/go.mod"
cmp "$TOPO_PREP_DIR/first/go.sum" "$TOPO_PREP_DIR/second/go.sum"
GOPROXY=off GOSUMDB=off go_vendor_archive create --no-use-module-proxy \
  --config "$TOPO_SOURCE_CHECKOUT/packaging/distro/go-vendor-tools.toml" \
  --output "$TOPO_PREP_DIR/second-vendor.tar.bz2" "$TOPO_PREP_DIR/second"
cmp "$TOPO_PREP_DIR/first-vendor.tar.bz2" "$TOPO_PREP_DIR/second-vendor.tar.bz2"
cmp "$TOPO_PREP_DIR/first/go.mod" "$TOPO_PREP_DIR/second/go.mod"
cmp "$TOPO_PREP_DIR/first/go.sum" "$TOPO_PREP_DIR/second/go.sum"
```

A drift or failure is a failed preparation gate, not permission to modify
upstream sources automatically. Keep the source hash, tool versions,
configuration hash and logs with each generated archive.

Run the license report without ignoring unresolved files:

```sh
go_vendor_license -d askalono \
  --config "$TOPO_SOURCE_CHECKOUT/packaging/distro/go-vendor-tools.toml" \
  --directory "$TOPO_PREP_DIR/first" report all \
  --no-prompt --autofill off --write-json "$TOPO_PREP_DIR/licenses.json"
```

The initial candidate report **failed** on three files. The October 16
follow-up now passes locally with retained additional notices and classifications.
Use prepare-fedora.py with that complete follow-up source; the original candidate
lacks its newly retained attribution files. See [the exact license findings](fedora-license-review.md). The
partial expression printed on failure must not populate the spec. Do not use
the ignore/exclusion options to hide the remaining work.

## Before a Fedora review request

1. Complete source and vendored license/notice review, install all required
   terms/attributions, and replace the cumulative-license placeholder with the
   complete accepted expression.
2. Select a real Fedora target and qualify its distro compiler. The current
   source spec requires exact Go 1.26.9; the earlier Rawhide snapshot's default
   1.27.2 does not satisfy it. Preserve the upstream release/security baseline.
   A downstream toolchain policy change needs documented qualification.
3. Finish source-RPM version/source metadata from a reviewed release or an
   explicitly identified snapshot. Do not relabel the candidate as published
   0.4.7 or replace existing tags/assets.
4. Add distro-compliant account, unit and documentation ownership with safe
   install/upgrade/remove behavior. The current spec installs only the CLI;
   the guided setup needs the matching packaged unit and named service group.
   Preserve existing credentials, trust, configuration and service state.
5. Produce a source RPM, run clean offline mock builds, rpmlint and actual
   lifecycle tests on each target architecture, and retain logs.
6. Confirm the submitter's Fedora account/sponsorship and current review
   workflow, then provide the exact source/spec/build artifacts for independent
   review. Package review, repository import, distro builds and publication
   remain external steps.

The [Fedora Go SIG](https://gitlab.com/fedora/sigs/go/go2rpm) documents vendored
Go packaging, and [Go Vendor Tools](https://fedora.gitlab.io/sigs/go/go-vendor-tools/)
documents reproducible archives and license reports. The general Fedora
maintainer documentation returned an access-denied page to this research
client; verify the current filing endpoint before submitting. Do not infer
that an experimental review service replaced the documented process.

## Debian and Ubuntu remain separate

Debian's [Go packaging guide](https://go-team.pages.debian.net/packaging.html)
uses distro-provided source dependencies with dh-golang. This vendor archive
does not resolve the existing Debian dependency/version/alias gaps. Prepare
those library updates and a complete source package, then obtain a sponsor's
review/upload and archive review. Check WNPP/package-name conflicts before an
[ITP](https://www.debian.org/doc/manuals/developers-reference/pkgs.en.html#new-packages).

Ubuntu's [new-package process](https://ubuntu.com/project/docs/how-ubuntu-is-made/processes/new-packages/)
prefers Debian first. A later development-release sync does not promise the
package in existing Ubuntu LTS installations or in the `main` component.
