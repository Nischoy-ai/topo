# Nischoy Topo 0.4.6 Beta

Nischoy Topo lets ServiceNow control discovery of infrastructure that it
cannot reach directly. A stateless Topo worker connects outbound, discovers
only explicitly approved targets, and returns normalized observations for the
ServiceNow application to reconcile through IRE.

## Start ServiceNow discovery in three steps

### 1. Install the Nischoy Topo app from the XML package

[Download Topo 0.4.6 Beta](https://github.com/Nischoy-ai/topo/releases/tag/servicenow-0.4.6-beta.2).
The package includes the app, all **32 required indexes**, and CMDB source setup in one native XML batch.

For the combined package:

1. Download and unzip the package; verify its signature and checksums using the
   [installation guide](docs/servicenow-update-set.md#customer-installation).
2. As a ServiceNow administrator, open **System Update Sets → Retrieved
   Update Sets → Import Update Set from XML** and upload
   `nischoy-topo-0.4.6-combined.xml`.
3. Open **Nischoy Topo**, choose **Preview Update Set Batch**, review any
   problems, then choose **Commit Update Set Batch**. Confirm both sets are
   committed. Run **Nischoy Topo — Register Discovery Source** from **Fix
   Scripts**, with **Unloadable** cleared, then complete the guide's checks.

No terminal, source checkout, SDK, or separate index import is needed for the
ServiceNow app. Start on a development instance and follow your organization’s
change process. Continue with the
[setup guide](docs/pilot-quickstart.md#2-create-the-least-privilege-servicenow-identities)
to configure access, credentials and approved targets.

### 2. Install and start the Topo worker

The signed **`v0.4.6-beta.1`** worker is available through the Linux Beta
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
Confirm `topo version` prints **`v0.4.6-beta.1`** before configuring credentials.

Configure the OAuth token file, target allowlist, and verified SSH `known_hosts`;
run `topo worker check`, then start the worker. The [worker setup
guide](docs/pilot-quickstart.md#4-install-and-configure-the-worker) covers the
Linux service and foreground Mac worker. Installation alone does not start scans.

### 3. Start a scan from ServiceNow

Open **Nischoy Topo → Discovery Profiles**, select the profile, and choose
**Run now**. Follow the run in **Runs**, confirm that IRE created or reconciled
the expected CIs, repeat the scan to check reconciliation, and only then enable
its schedule.

Password2 discovery supports explicitly listed IPv4 Linux hosts over
SSH port 22. It is not a subnet scanner. Use non-privileged test credentials
and begin with a disposable target. The complete setup, validation, upgrade,
and cleanup procedure is in the [ServiceNow setup guide](docs/pilot-quickstart.md).

## Documentation and verification

| Need | Guide |
| --- | --- |
| Install and operate discovery | [ServiceNow setup](docs/pilot-quickstart.md) · [Worker installation](docs/distribution.md) |
| Check the published package | [XML validation](docs/servicenow-validation.md) · [Release verification](docs/releases.md) |
| Review credential controls and report a vulnerability | [Deployment security](docs/deployment-security.md) · [Security policy](SECURITY.md) · [Security review record](docs/security-review.md) |
| Explore components or contribute | [Documentation index](docs/README.md) · [Developer quickstart](docs/development.md) · [Contributing](CONTRIBUTING.md) |
| See implemented and planned capabilities | [Product roadmap](ROADMAP.md) |

Nischoy Topo is licensed under the [Apache License 2.0](LICENSE).
