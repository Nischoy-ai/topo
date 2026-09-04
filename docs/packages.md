# Package artifacts and lifecycle

Each semantic Topo release assembles packages only after the six raw release
archives reproduce byte-for-byte and pass their checksum verification. Package
assembly extracts those binaries; it never compiles Topo again. The release set
contains:

- DEB and RPM packages for Linux amd64 and arm64;
- Authenticode-signed MSI installers for Windows amd64 and arm64;
- the raw macOS, Linux, and Windows archives;
- a versioned Helm chart;
- the validated, installable Nischoy Topo ServiceNow Fluent application ZIP
  plus its bounded contract metadata; and
- one deterministic offline bundle containing the complete artifact set,
  operator documentation, and an internal `OFFLINE-SHA256SUMS` manifest.

`package-metadata.json` records each package digest, architecture, format,
source archive, and source-binary digest. `SHA256SUMS`, the release-wide SBOM,
Sigstore signature, and GitHub attestations cover the final package set. Verify
those controls as described in [release verification](releases.md) before using
any installer.

## Reproduce package assembly

nFPM 2.47.0 is pinned by version and by the official release archive digest in
`scripts/fetch-nfpm.sh`. To assemble Linux packages and the Helm chart locally:

```sh
GOTOOLCHAIN=go1.26.8 scripts/build-release.sh \
  v0.1.0 "$(git rev-parse HEAD)" dist-raw
GOTOOLCHAIN=go1.26.8 scripts/build-packages.sh \
  v0.1.0 dist-raw dist-packages
```

The second command copies and verifies the raw input twice, assembles from two
different absolute paths, and rejects any byte difference across the DEB, RPM,
Helm, metadata, or checksum outputs. MSI creation uses pinned WiX 6.0.2 on a
Windows runner. MSI database creation includes unavoidable Windows Installer
package metadata, so its acceptance property is exact payload identity rather
than byte reproduction: CI silently installs it and proves that the installed
`topo.exe` digest matches the verified Windows archive.

## Linux packages

Both DEB and RPM install:

- `/usr/bin/topo`;
- `/usr/lib/systemd/system/topo-agent.service`;
- `/usr/lib/systemd/system/topo-worker.service`;
- license, readme, `topo-agent.env.example`, and
  `topo-worker.env.example` under
  `/usr/share/doc/topo/`.

Installation creates the unprivileged `topo-agent` and `topo-worker` system
users when needed and reloads systemd metadata. It deliberately does not create
live configuration, generate a secret, enable either unit, or start either
service. Configure the desired service explicitly; see the
[ServiceNow pilot quickstart](pilot-quickstart.md) for the worker path.

```sh
sudo install -d -o root -g topo-agent -m 0750 /etc/topo-agent
sudo install -o root -g topo-agent -m 0640 \
  /usr/share/doc/topo/topo-agent.env.example \
  /etc/topo-agent/topo-agent.env
sudo install -o root -g topo-agent -m 0640 /path/to/api-key \
  /etc/topo-agent/api-key
sudo install -o root -g topo-agent -m 0640 /path/to/spool-key \
  /etc/topo-agent/spool-key
sudo systemctl enable --now topo-agent.service
```

Edit the environment example for the real controller URL before enabling the
unit. Ordinary package removal deletes package-owned files but preserves
operator-created agent and worker configuration, secrets, state, and system
identities. Remove those separately only when intentionally purging the
deployment.

## Windows MSI

The MSI installs `topo.exe`, `LICENSE`, and `README.md` under
`%ProgramFiles%\Nischoy\Topo` and registers that directory in the machine PATH.
It uses one stable upgrade identity per architecture and a new deterministic
product identity per semantic version. It does not register or start the Topo
Agent service; after supplying credential references, use `topo agent install`
explicitly as described in [Topo Agent](topo-agent.md).

Silent enterprise installation and removal use standard Windows Installer
commands:

```powershell
msiexec.exe /i topo_0.1.0_windows_amd64.msi /qn /norestart
msiexec.exe /x topo_0.1.0_windows_amd64.msi /qn /norestart
```

The tag workflow refuses to publish if the protected
`WINDOWS_SIGNING_PFX_BASE64` or `WINDOWS_SIGNING_PFX_PASSWORD` secret is
missing. It timestamps, Authenticode-signs, and verifies both installers before
they can reach the publishing job. Pull-request CI builds and exercises
unsigned test MSIs with no access to the production certificate.

## Helm chart

The chart never accepts the operator API key as a Helm value. Create a Secret
outside Helm, then pass only its name:

```sh
kubectl create secret generic topo-api-key --from-file=api-key=/path/to/api-key
helm install topo topo-0.1.0.tgz --set apiKeySecret.name=topo-api-key
```

The pod runs as a fixed non-root identity, drops every capability, uses a
read-only root filesystem, disables service-account-token automounting, and has
default CPU/memory requests and limits. SQLite persistence is enabled by
default with a one-GiB PVC. CI exercises lint, the required-Secret failure,
install, upgrade, rollback, and uninstall against an ephemeral Kubernetes
cluster.

## Offline bundle

After downloading the offline bundle, verify the release-wide signature and
attestation first. Extract it, then verify every contained file without network
access:

```sh
tar -xzf topo_0.1.0_offline.tar.gz
cd topo_0.1.0_offline
sha256sum --check OFFLINE-SHA256SUMS
```

The bundle is a transport convenience, not a separate trust root. Its internal
manifest detects corruption after extraction; the signed outer `SHA256SUMS`
authenticates the bundle itself.

## Upgrade and rollback

Before replacing a controller package or Helm release, stop writes and create a
verified SQLite backup with `topo storage backup`. Install the new version,
start it against the existing database, and validate observations, assets,
relationships, audit entries, schedules, and revocations. Package upgrades
preserve `/etc/topo-agent` and `/var/lib/topo-agent`; MSI upgrades preserve
operator state outside `%ProgramFiles%`.

If validation fails, stop the new binary. Install the prior package version,
restore the pre-upgrade backup to a new database path, and start the old binary
against that restored path. Topo never reverse-migrates or overwrites the failed
database in place. See [backup, restore, and upgrade procedures](storage.md#backup-and-restore).

## Package-manager promotion

The next release stage promotes these exact bytes through signed APT/RPM
repositories, the Nischoy Homebrew tap, Microsoft's WinGet catalog, and a GHCR
OCI Helm registry. It adds native signing, stable/beta policy, key rotation,
and clean-machine gates without rebuilding Topo. See
[package-manager distribution](distribution.md). These channels are not public
until their one-time repositories/credentials are provisioned and a real beta
and N-1 stable promotion complete.
