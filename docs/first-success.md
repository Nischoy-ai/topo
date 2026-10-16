# Your first ServiceNow discovery run

Start with **one Linux target and a Linux worker**, then add Windows or a subnet.
This is the exercised path for Topo **0.4.7**: computer and network-interface
inventory reconciled into CMDB through IRE. Read this overview in about five
minutes; administrator, OAuth and target preparation take additional time.

## Before you start

Use an authorized development instance and start with one approved Linux IPv4
target. After importing the app, have the administrator and credential custodian
complete the separate [one-time administrator preparation](administrator-preparation.md):
restricted OAuth, pool/site, target scope, protected remote credential, binding
and profile. Have a protected worker token file, approved allowlist and
independently verified SSH host keys ready before step 2. Installation does not
perform this preparation or authorize discovery.

Examples use `192.0.2.10/32`, `pilot-linux` and `pilot-site`. Replace the example
address, instance and paths with approved values.

## 1. Install the ServiceNow app

Follow the [XML installation checklist](servicenow-update-set.md#customer-installation):
verify the signed package, import `nischoy-topo-0.4.7-combined.xml` through
**Retrieved Update Sets → Import Update Set from XML**, then preview, review
problems and commit the complete batch.

**Expected:** app version **0.4.7**, all **three sets / 474 updates** committed,
and all **32 required ordered physical index definitions** verified. The index child still has its historical
**0.4.6 Index Repair** name. Open **System Definition → Fix Scripts → Nischoy
Topo — Register Discovery Source**, clear **Unloadable**, then **Run Fix Script →
Proceed**. Confirm the active `Nischoy Topo` discovery-source choice before scanning.
The download's `START-HERE.txt`, `INSTALLATION.md` and `INDEXES.md` are the
version-matched checklists; no SDK or source checkout is needed.

<a id="2-set-up-access-and-five-records"></a>

Before connecting the server, complete [one-time administrator preparation](administrator-preparation.md).
It follows app import and stays separate from the operator installation steps.

<a id="3-install-check-and-start-the-worker"></a>

## 2. Install, check and start the worker

The published 0.4.7 worker still uses the manual setup below.
The [guided setup command](worker-setup.md) is source work; it is not included
in that release. Default-distribution repository installation is not yet available.

Set up the [signed APT or RPM repository](distribution.md#user-installation),
then install using **one** matching command:

```sh
sudo apt-get install topo  # Debian/Ubuntu
# Fedora: sudo dnf install topo
topo version
```

**Expected:** `v0.4.7-beta.1`. Installation alone does not configure or start
scans. Prepare an absolute private directory, OAuth token file, `targets.allow`
containing only the approved target CIDR, and verified `known_hosts` identifying
that IP on port 22. Use an owner-only directory and token mode `0600` for this
foreground run; every file must be readable by the account running the worker.
See [file and trust preparation](mixed-inventory-setup.md#4-check-and-start-the-matching-worker).

Replace the placeholders, then run:

```sh
topo worker check \
  -servicenow-instance https://your-instance.service-now.com \
  -token-ref file:/absolute/private/worker-token \
  -worker-pool pilot-linux -site pilot-site \
  -allow-ssh-linux \
  -ssh-target-allowlist /absolute/private/targets.allow \
  -ssh-known-hosts /absolute/private/known_hosts \
  -remote-start-interval 1s -max-concurrency 2
```

**Expected:** JSON with `status: "ready"`. This checks registration and one
heartbeat; it does **not** contact the target, retrieve its password or prove
IRE works. Repeat the same command with **`worker run`** replacing **`worker
check`** and leave it running. Ctrl-C stops it. Startup files and token changes
require stopping, checking and restarting the worker.

<a id="4-run-inspect-and-repeat"></a>

## 3. Run, inspect and repeat

Open **Nischoy Topo → Discovery Profiles → pilot-linux → Run now**. In **Runs**,
open the run and inspect tasks, collection errors and **IRE Deliveries**.

**Success means:** no unexplained collection/IRE errors, the expected computer
and network adapters in CMDB, and their **Owns::Owned by** relationships.
A ready worker or terminal task alone is insufficient. Repeat **Run now** and
confirm the same CI/source and relationship identities reconcile, rather than
creating duplicates. Only then create and activate a **Schedule** for the profile.
For failures, use the [troubleshooting table](mixed-inventory-setup.md#troubleshooting).

## Next: choose only what you need

- **Linux background service:** use the [packaged Linux setup](pilot-quickstart.md#4-install-and-configure-the-worker).
  Its service enables SSH only; a foreground run is not persistent across reboot.
- **Windows:** first complete [target trust and permissions](mixed-inventory-setup.md#1-establish-the-installation-and-target-prerequisites):
  non-admin local account, Remote Management Users/WMI read access, HTTPS/5986,
  NTLMv2, independently trusted CA and a certificate with the selected IP in its
  SAN. Keep Basic/unencrypted authentication disabled. Then create separate
  Windows credentials, scope, binding and `winrm_windows.v1` profile and use
  the [explicit mixed worker command](mixed-inventory-setup.md#4-check-and-start-the-matching-worker).
  Adding files to the shipped Linux service does not enable Windows.
- **More targets:** use separate protocol scopes with explicit CIDR exclusions,
  at most **1,024 selected IPv4 addresses** after exclusions. Network/broadcast
  addresses are not automatically removed. Review [selection and pacing](mixed-inventory-setup.md#3-create-one-profile-per-protocol).
- **macOS:** [Homebrew installation](distribution.md#macos-homebrew) provides a
  CLI formula, not a background service. Managed mixed runtime on macOS remains
  unexercised; use Linux for the exercised path.

Review [credential storage/rotation](deployment-security.md),
[evidence and limits](evidence/mixed-inventory.md) and
[upgrades/removal](pilot-quickstart.md#6-upgrade-and-remove) before wider use.
This guide adds no new installation, novice-usability or production-readiness
proof. Published combined XML comparison and prior component-install evidence
have distinct scopes; populated upgrade and independent unique-enforcement tests
are not passed. Windows installers, domain compatibility and broader inventory
remain outside this first-success path.
