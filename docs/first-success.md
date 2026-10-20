# Your first ServiceNow discovery run

Start with **one Linux target and a Linux worker**, then add Windows or a subnet.
This is the exercised path for Topo **0.4.7**: computer and network-interface
inventory reconciled into CMDB through IRE. Read this overview in about five
minutes; administrator, OAuth and target preparation take additional time.

## Before you start

Arrange these with your ServiceNow administrator and target owner:

- An authorized development instance with custom-table/application entitlement
  and CMDB/IRE available. Keep schedules inactive and workers stopped during setup.
- A Linux worker with outbound HTTPS to ServiceNow and SSH/22 access to one
  approved Linux IPv4 address. No inbound worker listener is needed.
- A non-admin SSH account/password and a host key verified through an independent
  trusted source. `ssh-keyscan` alone does not verify identity.
- Separate app administrator, operator and credential custodian access, plus a
  dedicated worker integration user and restricted OAuth client (step 2).

Examples use `192.0.2.10/32`, `pilot-linux` and `pilot-site`. Replace the example
address, instance and paths with approved values. Never paste passwords or tokens
into commands, labels or logs.

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

## 2. Set up access and five records

Use the [identity and seven-resource OAuth checklist](pilot-quickstart.md#2-create-the-least-privilege-servicenow-identities).
Humans receive their respective `admin`, `operator`, `viewer` or
`credential_admin` roles under `x_664635_topo`. The machine user receives only
`x_664635_topo.worker`; its OAuth policy allows only the seven listed POST
resources, with no wildcards, generic Table API, IRE or credential-table access.
Transfer its token directly into a restricted worker file through your secure
OAuth workflow. The credential custodian enters the target password in
**Remote Credentials**, using the protected Password2 field.

In **Nischoy Topo**, create these active records in order. Give each a descriptive
**Name**; fields not listed retain their defaults:

| Record | First-run values |
| --- | --- |
| **Worker Pools** | Pool `pilot-linux`, site `pilot-site`, dedicated worker user; lease seconds `120`, maximum task seconds `300`, maximum leases `2`. |
| **Target Scopes** | ID `pilot-linux-targets`, revision `1`, same pool/site; approved target `192.0.2.10/32`, IPv4 partition prefix `32`. |
| **Remote Credentials** | ID `pilot-linux-password`, non-admin SSH username and protected password. |
| **Credential Bindings** | ID `pilot-linux-binding`, revision `1`, protocol **SSH password** (`ssh_password`); credential above and the revision-`1` scope record, allowed profile `pilot-linux`, profile revision `1`. |
| **Discovery Profiles** | ID `pilot-linux`, revision `1`, operation `ssh_linux.v1`, schema `v1alpha1`; matching pool, scope and binding. |

IDs/revisions form the authorization contract. For later changes, use new
revisions and matching bindings; see [scope and binding rules](mixed-inventory-setup.md#3-create-one-profile-per-protocol).

## 3. Install, check and start the worker

Set up the [signed APT or RPM repository](distribution.md#user-installation),
then install using **one** matching command. To skip the repository, download
the binary or package directly from [Get Topo](get-topo.md#the-worker).

```sh
sudo apt-get install topo  # Debian/Ubuntu
# Fedora: sudo dnf install topo
topo version
```

**Expected:** a version starting with `v0.4.7`. Installation alone does not configure or start
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

## 4. Run, inspect and repeat

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
