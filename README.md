# Nischoy Topo

Nischoy Topo lets ServiceNow control discovery of infrastructure that it
cannot reach directly. A stateless Topo worker connects outbound, discovers
only explicitly approved targets, and returns normalized observations for the
ServiceNow application to reconcile through IRE.

## Start ServiceNow discovery in three steps

### 1. Install the Nischoy Topo app from the XML package

[ServiceNow 0.4.6 preview 2](https://github.com/Nischoy-ai/topo/releases/tag/servicenow-0.4.6-preview.2)
includes the app and all **32 required indexes** in one native XML batch.
Use this package for new installations; preview 1 is the earlier app-only
artifact. The release is currently a development preview.

For the combined package:

1. Download and unzip the package; verify its checksum using the
   [installation guide](docs/servicenow-update-set.md#customer-installation).
2. As a ServiceNow administrator, open **System Update Sets → Retrieved
   Update Sets → Import Update Set from XML** and upload
   `nischoy-topo-0.4.6-combined.xml`.
3. Open **Nischoy Topo**, choose **Preview Update Set Batch**, review any
   problems, then choose **Commit Update Set Batch**. Confirm both sets are
   committed and complete the guide's installation checks.

No terminal, source checkout, SDK, or separate index import is needed for the
ServiceNow app. Start on a development instance and follow your organization’s
change process. Continue with the
[setup guide](docs/pilot-quickstart.md#2-create-the-least-privilege-servicenow-identities)
to configure access, credentials and approved targets.

### 2. Install and start the Topo worker

Install **v0.1.0-beta.1** on a host that can reach ServiceNow and your approved
targets. Use the compatible ServiceNow app version listed in its release manifest.

```sh
# macOS
brew install nischoy-ai/tap/topo-beta
topo version
```

On Linux, first add the Nischoy signed repository using the one-time
[Debian/Ubuntu setup](docs/distribution.md#debian-and-ubuntu) or
[Fedora/RHEL setup](docs/distribution.md#fedora-and-rhel-family). Then install:

```sh
# Debian / Ubuntu
sudo apt-get install topo

# Fedora / RHEL
sudo dnf install topo
```

Linux packages support amd64 and arm64. Repository setup is needed only once
per host; subsequent installs and updates use your package manager.
Stable and Windows channels are unavailable; the Mac beta is not Apple-notarized.

Configure the OAuth token file, target allowlist, and verified SSH `known_hosts`;
run `topo worker check`, then start the worker. The [worker setup
guide](docs/pilot-quickstart.md#4-install-and-configure-the-worker) covers the
Linux service and foreground Mac worker. Installation alone does not start scans.

### 3. Start a scan from ServiceNow

Open **Nischoy Topo → Discovery Profiles**, select the profile, and choose
**Run now**. Follow the run in **Runs**, confirm that IRE created or reconciled
the expected CIs, repeat the scan to check reconciliation, and only then enable
its schedule.

The current Password2 pilot discovers explicitly listed IPv4 Linux hosts over
SSH port 22. It is not a subnet scanner. Use non-privileged test credentials
and begin with a disposable target. The complete setup, validation, upgrade,
and cleanup procedure is in the [pilot quickstart](docs/pilot-quickstart.md).

## Documentation and verification

| Need | Guide |
| --- | --- |
| Install and operate discovery | [ServiceNow setup](docs/pilot-quickstart.md) · [Worker installation](docs/distribution.md) |
| Check the published package | [XML validation](docs/servicenow-validation.md) · [Release verification](docs/releases.md) |
| Review credential controls and report a vulnerability | [Security policy](SECURITY.md) · [Security review record](docs/security-review.md) |
| Explore components or contribute | [Documentation index](docs/README.md) · [Developer quickstart](docs/development.md) · [Contributing](CONTRIBUTING.md) |
| See implemented and planned capabilities | [Product roadmap](ROADMAP.md) |

Nischoy Topo is licensed under the [Apache License 2.0](LICENSE).
