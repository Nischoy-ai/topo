# Your first ServiceNow discovery run

This guide gets one Linux machine into your CMDB, using a Topo worker on Linux.
Once that works, you can add Windows machines and whole subnets.

## Before you start

You need:

- A ServiceNow **development instance** where you can install apps.
- A **Linux machine for the worker**. It needs outbound HTTPS to ServiceNow and
  SSH (port 22) to the machine you want to discover. Nothing connects in to it.
- One **Linux machine to discover**, with a non-admin SSH account and password.
- That machine's **SSH host key**, checked against a source you trust, such as
  its owner. Don't rely on `ssh-keyscan` alone.

The examples use the address `192.0.2.10`, the worker pool `pilot-linux` and
the site `pilot-site`. Replace them with your own. Never put passwords or
tokens in commands or logs.

## 1. Install the ServiceNow app

1. Download `nischoy-topo-0.4.7-combined.xml` from [Get Topo](get-topo.md#the-servicenow-app).
2. In ServiceNow, open **Retrieved Update Sets → Import Update Set from XML**
   and import it. Preview it, review any problems, then choose **Commit
   Update Set Batch**.
3. Open **System Definition → Fix Scripts → Nischoy Topo — Register Discovery
   Source**. Clear **Unloadable**, then choose **Run Fix Script → Proceed**.

**Check:** the app shows version **0.4.7**. The
[installation checklist](servicenow-update-set.md#customer-installation) lists
the exact counts to confirm (3 update sets, 474 updates, 32 indexes). One
update set is named **Nischoy Topo 0.4.6 Index Repair**; that old name is
expected.

## 2. Create the worker user and five records

**Users.** Create a dedicated ServiceNow user for the worker, with only the
`x_664635_topo.worker` role, and an OAuth client for it. Follow the
[user and OAuth checklist](pilot-quickstart.md#2-create-the-least-privilege-servicenow-identities)
exactly; it limits the worker to the seven API calls it needs. Save the
worker's OAuth token in a file on the worker machine. People get the app's
`admin`, `operator`, `viewer` or `credential_admin` role; keep entering
passwords (`credential_admin`) separate from running scans.

**Records.** In **Nischoy Topo**, create these five records in this order.
Leave any field not listed at its default.

| Record | Values |
| --- | --- |
| **Worker Pools** | Pool `pilot-linux`, site `pilot-site`, your worker user. Lease seconds `120`, maximum task seconds `300`, maximum leases `2`. |
| **Target Scopes** | ID `pilot-linux-targets`, revision `1`, the same pool and site. Target `192.0.2.10/32`, IPv4 partition prefix `32`. |
| **Remote Credentials** | ID `pilot-linux-password`, the SSH username, and the password in the protected password field. |
| **Credential Bindings** | ID `pilot-linux-binding`, revision `1`, protocol **SSH password**. Pick the credential and scope above. Allowed profile `pilot-linux`, profile revision `1`. |
| **Discovery Profiles** | ID `pilot-linux`, revision `1`, operation `ssh_linux.v1`, schema `v1alpha1`. Pick the pool, scope and binding above. |

To change a record later, create a new revision rather than editing it; see
[scope and binding rules](mixed-inventory-setup.md#3-create-one-profile-per-protocol).

## 3. Install and start the worker

**Install** it on the worker machine, either from the
[package repository](distribution.md#user-installation) or by
[downloading it directly](get-topo.md#the-worker):

```sh
sudo apt-get install topo   # Debian/Ubuntu; on Fedora: sudo dnf install topo
topo version                # prints a version starting with v0.4.7
```

**Prepare three files** in a private directory that only the worker's account
can read:

- `worker-token`: the OAuth token from step 2, with mode `0600`
- `targets.allow`: one line, `192.0.2.10/32`
- `known_hosts`: the verified host key for `192.0.2.10`

See [file preparation](mixed-inventory-setup.md#4-check-and-start-the-matching-worker)
for details.

**Check** the worker can reach ServiceNow. Replace the instance URL and paths:

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

It should print JSON with `status: "ready"`. This only checks the connection
to ServiceNow; it doesn't contact the target yet.

**Start** the worker by running the same command with `worker run` in place of
`worker check`. Leave it running; Ctrl-C stops it. If you change any of the
files, stop it, check again and restart it.

## 4. Run discovery

1. Open **Nischoy Topo → Discovery Profiles → pilot-linux** and choose
   **Run now**.
2. In **Runs**, open the run. Look at its tasks, any collection errors and
   **IRE Deliveries**.
3. In the CMDB, you should see the computer, its network adapters, and an
   **Owns::Owned by** relationship between them.
4. Choose **Run now** once more. It should update the same records, not create
   duplicates.

When both runs look right, you can create a **Schedule** for the profile. If
something fails, see [troubleshooting](mixed-inventory-setup.md#troubleshooting).

## Next steps

- **Run the worker as a service:** [packaged Linux setup](pilot-quickstart.md#4-install-and-configure-the-worker).
  The service enables SSH only.
- **Add Windows machines:** [Windows target setup](mixed-inventory-setup.md#1-establish-the-installation-and-target-prerequisites)
  (WinRM over HTTPS with NTLMv2).
- **Scan a subnet:** use a CIDR with exclusions, up to 1,024 addresses per run;
  see [selection and pacing](mixed-inventory-setup.md#3-create-one-profile-per-protocol).
- **Use macOS for the worker:** [Homebrew installation](distribution.md#macos-homebrew).
  This is a command-line install only, and Linux is the tested path.

Before wider use, read [credential storage and rotation](deployment-security.md),
[tested scope and limits](evidence/mixed-inventory.md) and
[upgrades and removal](pilot-quickstart.md#6-upgrade-and-remove).
