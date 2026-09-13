# Nischoy Topo

Nischoy Topo lets ServiceNow control discovery of infrastructure that it
cannot reach directly. A stateless Topo worker connects outbound, discovers
only explicitly approved targets, and returns normalized observations for the
ServiceNow application to reconcile through IRE.

## Start ServiceNow discovery in three steps

### 1. Install the Nischoy Topo app

Use a non-production ServiceNow instance for the pilot. The currently
validated installation path uses the ServiceNow SDK and an OAuth alias:

```sh
scripts/install-servicenow-app.sh <sdk-oauth-alias>
```

In ServiceNow, create a worker pool, target scope, Password2 SSH credential,
credential binding, and discovery profile. Follow the exact record order and
least-privilege role setup in the [ServiceNow Linux pilot
quickstart](docs/pilot-quickstart.md#2-create-the-least-privilege-servicenow-identities).

### 2. Install and start the Topo worker

Install the Topo package from the same release as the ServiceNow app on a Linux
server that can reach both ServiceNow and the approved targets:

The first signed beta targets Linux APT/RPM and macOS/Homebrew; publication is
still pending. See [channel availability and setup](docs/distribution.md).

```sh
# Debian or Ubuntu
sudo dpkg -i topo_<version>_amd64.deb

# Fedora or RHEL family
sudo rpm -Uvh topo-<version>-1.x86_64.rpm
```

Add the worker OAuth token file, local target allowlist, and verified SSH
`known_hosts`; run `topo worker check`; then enable `topo-worker.service`. Copy
the ready-to-run commands from [install and configure the
worker](docs/pilot-quickstart.md#4-install-and-configure-the-worker).

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
