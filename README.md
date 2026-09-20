# Nischoy Topo

Nischoy Topo lets ServiceNow control discovery of infrastructure that it
cannot reach directly. A stateless Topo worker connects outbound, discovers
only explicitly approved targets, and returns normalized observations for the
ServiceNow application to reconcile through IRE.

## Start ServiceNow discovery in three steps

### 1. Install the Nischoy Topo app

The customer XML update-set path is being prepared for non-production pilots.
Follow the [XML installation guide](docs/servicenow-update-set.md) for download,
import, preview, commit and customer-owned access setup. **No validated XML
release is available yet**; the existing beta's SDK ZIP is not an XML update set.
Developer/source installation remains in the [developer app guide](integrations/servicenow/topo-control-plane/README.md#install).

### 2. Install and start the Topo worker

Install **v0.1.0-beta.1** on a host that can reach ServiceNow and your approved
targets. Use the compatible ServiceNow app version listed in its release manifest.

```sh
# macOS
brew install nischoy-ai/tap/topo-beta
topo version
```

For Linux, follow the signed [APT](docs/distribution.md#debian-and-ubuntu) or
[RPM](docs/distribution.md#fedora-and-rhel-family) repository setup, then install
`topo`. Stable and Windows channels are not available; the Mac beta is not
Apple-notarized.

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

## Learn more

- [Developer quickstart and component guide](docs/development.md)
- [Architecture](docs/architecture.md)
- [ServiceNow control-plane design](docs/servicenow-control-plane.md)
- [ServiceNow worker behavior and evidence](docs/servicenow-worker.md)
- [ServiceNow IRE publishing](docs/servicenow.md)
- [Security policy and deployment posture](SECURITY.md)
- [Project roadmap and current status](ROADMAP.md)
- [Current milestone plan and handoff](docs/project-plan.md)
- [Contributing](CONTRIBUTING.md)

Nischoy Topo is licensed under the Apache License 2.0.
