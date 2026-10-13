# Nischoy Topo 0.4.7 Beta

Nischoy Topo lets ServiceNow control discovery of infrastructure that it
cannot reach directly. A stateless Topo worker connects outbound, discovers
only explicitly approved targets, and returns normalized observations for the
ServiceNow application to reconcile through IRE.

## Beta scope

This beta supports ServiceNow-managed Linux computer/interface discovery over
verified SSH/22 and Windows computer/interface discovery over verified WinRM
HTTPS/5986 with NTLMv2. Separate protocol profiles select approved IPv4 addresses
or CIDRs with explicit exclusions, capped at 1,024 selected addresses per run.
Password2 credentials are released only for authorized task attempts; local
worker allowlists and trust files independently constrain access.

Workers are distributed for Linux and macOS. Real mixed managed acceptance uses
Linux workers; macOS mixed runtime is not independently exercised. Windows
worker/installer publication remains separate. See the [tested scope](docs/evidence/mixed-inventory.md).

CMDB publication supports only `cmdb_ci_computer`, `cmdb_ci_network_adapter`,
and `host_has_interface` mapped to `Owns::Owned by`. This beta does not provide
the broad estate coverage needed to replace Device42, for either Linux or Windows estates. Other discovery components in the source tree have separate validation
limits; see the [roadmap](ROADMAP.md).

## Start ServiceNow discovery in three steps

### 1. Install the Nischoy Topo app from the XML package

[Download Topo 0.4.7 Beta](https://github.com/Nischoy-ai/topo/releases/tag/servicenow-0.4.7-beta.1).
The package includes the app, all **32 required indexes**, and CMDB source setup in one native XML batch.

For the combined package:

1. Download and unzip the package; verify its signature and checksums using the
   [installation guide](docs/servicenow-update-set.md#customer-installation).
2. As a ServiceNow administrator, open **System Update Sets → Retrieved
   Update Sets → Import Update Set from XML** and upload
   `nischoy-topo-0.4.7-combined.xml`.
3. Open **Nischoy Topo 0.4.7 — Application**, choose **Preview Update Set Batch**, review any
   problems, then choose **Commit Update Set Batch**. Confirm all three sets and 474 metadata updates are committed. Run **Nischoy Topo — Register Discovery Source** from **Fix
   Scripts**, with **Unloadable** cleared, then complete the guide's checks.

No terminal, source checkout, SDK, or separate index import is needed for the
ServiceNow app. Start on a development instance and follow your organization’s
change process. Continue with the
[Linux/Windows setup guide](docs/mixed-inventory-setup.md#2-separate-human-access-from-worker-credentials)
to configure access, credentials and approved targets.

### 2. Install and start the Topo worker

The signed **`v0.4.7-beta.1`** worker is available through the Linux Beta
repositories and official Mac tap.

On macOS:

```sh
brew install nischoy-ai/tap/topo-beta
```

On Linux, complete the one-time [signed repository setup](docs/distribution.md#debian-and-ubuntu),
then install with your package manager:

```sh
sudo apt-get install topo  # Debian/Ubuntu
# or: sudo dnf install topo  # Fedora
```

See [package installation and upgrades](docs/distribution.md)
for repository trust checks, prerequisites and existing installations.
Confirm `topo version` prints **`v0.4.7-beta.1`** before configuring credentials.

Configure the OAuth token file, separate protocol allowlists, verified SSH
`known_hosts` and trusted WinRM CA PEM as applicable. Run `topo worker check`,
then `topo worker run` with identical approved flags. Follow the
[worker setup](docs/mixed-inventory-setup.md#4-check-and-start-the-matching-worker).
The packaged Linux service enables SSH only; use the explicit foreground setup
for mixed targets or a reviewed service configuration with both protocols.
Installation alone does not start scans.

### 3. Start a scan from ServiceNow

Open **Nischoy Topo → Discovery Profiles**, select the profile, and choose
**Run now**. Follow the run in **Runs**, confirm that IRE created or reconciled
the expected CIs, repeat the scan to check reconciliation, and only then enable
its schedule.

Start with one non-admin target per protocol, repeat the scan to confirm stable
CI/relationship identities, then expand only within the approved bounds. Windows
inventory excludes software, disks, services and patches. The complete trust,
credential, scope and troubleshooting path is the [Linux/Windows setup guide](docs/mixed-inventory-setup.md).
For an SSH-only deployment, use the [Linux setup](docs/pilot-quickstart.md).

The published combined XML was compared offline with reviewed native inputs.
Component restoration and index evidence are separately attributed; this is not
a fresh combined-byte or pristine-instance installation claim. Populated upgrade
testing is deferred, and independent unique enforcement tests were excluded;
neither is represented as passed. See the [validation record](docs/servicenow-validation.md).

## Documentation and verification

| Need | Guide |
| --- | --- |
| Install and operate discovery | [Linux/Windows setup](docs/mixed-inventory-setup.md) · [Worker installation](docs/distribution.md) |
| Check the published package | [XML validation](docs/servicenow-validation.md) · [Release verification](docs/releases.md) |
| Review credential controls and report a vulnerability | [Deployment security](docs/deployment-security.md) · [Security policy](SECURITY.md) · [Security review record](docs/security-review.md) |
| Explore components or contribute | [Documentation index](docs/README.md) · [Developer quickstart](docs/development.md) · [Contributing](CONTRIBUTING.md) |
| See beta scope, validation limits and plans | [Product roadmap](ROADMAP.md) |

Nischoy Topo is licensed under the [Apache License 2.0](LICENSE).
