# ServiceNow-managed Linux discovery pilot

This is the shortest supported early-adopter path for the current managed
mode: install the Nischoy Topo Fluent application on a **non-production**
ServiceNow instance, install Topo on a machine that can reach both ServiceNow
and the Linux targets, configure one Password2 SSH credential, and start a
manual or scheduled run from the control panel.

The pilot discovers only explicit IPv4 `/32` Linux targets over SSH port 22.
It maps only computers, network adapters, and `Owns::Owned by` through IRE. It
does not scan a subnet, try a list of credentials, accept a remote command, or
use ECC, MID Server, native Discovery schedules, probes, patterns, or sensors.
The worker is outbound-only and keeps no database, journal, spool, retry queue,
or observation history.

## 1. Install the scoped application

The customer distribution path is the [XML update-set guide](servicenow-update-set.md):
download, verify, import through Retrieved Update Sets, preview, resolve
issues and commit. Clean XML installation is validated on Australia Patch 3;
broader pilot acceptance and public XML publication remain pending. The
published beta SDK ZIP must not be renamed or imported as an update set.

For development on an approved instance, use the [SDK/source instructions](../integrations/servicenow/topo-control-plane/README.md#install).
Those instructions remain the previously validated developer workflow, not a
customer prerequisite. After the app is installed, continue below.

Have the instance administrator register the exact `Nischoy Topo` choice on
`cmdb_ci.discovery_source` before discovery; see [IRE prerequisites](servicenow.md).
This global customer setting is not shipped in the scoped application XML.

## 2. Create the least-privilege ServiceNow identities

Use separate human and machine identities:

- assign `x_664635_topo.admin` to the person who manages pools and app policy;
- assign `x_664635_topo.credential_admin` only to the person who enters or
  rotates Password2 credentials and bindings;
- assign `x_664635_topo.operator` to people who manage profiles/schedules and
  run discovery;
- assign `x_664635_topo.viewer` for read-only run visibility; and
- create a dedicated integration user with only `x_664635_topo.worker` for the
  worker process.

Create a dedicated OAuth inbound profile and an API access policy that grants
that worker client only these authenticated version-1 resources, all with
method `POST`:

```text
/x_664635_topo/v1/tasks/workers/register
/x_664635_topo/v1/tasks/workers/heartbeat
/x_664635_topo/v1/tasks/claim
/x_664635_topo/v1/tasks/{id}/renew
/x_664635_topo/v1/tasks/{id}/credential
/x_664635_topo/v1/tasks/{id}/results
/x_664635_topo/v1/tasks/{id}/complete
```

Disable resource, method, version, and global wildcards. Do not grant the
worker generic Table API, CMDB, IRE, application administration, schedule, or
credential-table access, and do not reuse a direct IRE publisher client. Obtain
the worker bearer token through the customer's normal owner-only OAuth
workflow and write it directly to the worker token file; never copy it into a
command line or the non-secret environment file.

## 3. Create the first discovery profile

Open **Nischoy Topo** in the ServiceNow application navigator and create the
records in this order. Replace the example IDs, site, target, and username;
IDs become immutable once referenced.

1. **Worker Pools** — pool ID `pilot-linux`, site ID `pilot-site`, the dedicated
   service user, active; start with lease seconds `120`, maximum task seconds
   `300`, and maximum leases `2`.
2. **Target Scopes** — scope ID `pilot-linux-targets`, revision `1`, the same
   pool/site, one approved canonical IPv4 target per line (for example
   `192.0.2.10/32`), IPv4 partition prefix `32`, active. The compiled partition
   count must be between 1 and 1,024.
3. **SSH Credentials** — credential ID `pilot-linux-password`, the SSH
   username and Password2 value, active. Enter this while acting as the
   credential administrator; do not test with a production privileged account.
4. **Credential Bindings** — binding ID `pilot-linux-binding`, revision `1`,
   protocol `SSH password`, allowed profile ID `pilot-linux`, allowed profile
   revision `1`, the target scope and credential above, active.
5. **Discovery Profiles** — profile ID `pilot-linux`, revision `1`, operation
   `ssh_linux.v1`, the same pool, target scope and binding, schema version
   `v1alpha1`, active.

For recurring execution, create a **Schedule** referencing that profile, set a
bounded interval and next-run time, and leave it inactive until the manual run
has succeeded.

## 4. Install and configure the worker

Use the worker version listed in the app release compatibility notes; the published beta is
`v0.1.0-beta.1`. Configure the signed [APT or RPM beta
repository](distribution.md#user-installation), then install `topo` with your
package manager. For offline installation, download matching release files and
verify `SHA256SUMS`, its Sigstore bundle and GitHub attestation as described in
[release verification](releases.md). DEB and RPM packages install a hardened
but dormant `topo-worker.service`; installation never creates config or secret
files and never enables or starts it.

```sh
# Debian/Ubuntu
sudo dpkg -i topo_<version>_amd64.deb

# Fedora/RHEL-family
sudo rpm -Uvh topo-<version>-1.x86_64.rpm
```

On macOS, install with `brew install nischoy-ai/tap/topo-beta`. This beta is not
Apple-notarized; do not bypass Gatekeeper. The older development tap is separate
and must be removed explicitly if its `topo` executable conflicts. No automatic
migration or background Mac service is provided.

On packaged Linux, create the read-only startup policy. `ssh-keyscan` output is
not proof of host identity by itself: compare each fingerprint with an
independent trusted source before installing it as `known_hosts`.

```sh
sudo install -d -o root -g topo-worker -m 0750 /etc/topo-worker
sudo install -o root -g topo-worker -m 0640 \
  /usr/share/doc/topo/topo-worker.env.example \
  /etc/topo-worker/topo-worker.env
sudo install -o root -g topo-worker -m 0640 /secure/path/worker-token \
  /etc/topo-worker/worker-token
sudo install -o root -g topo-worker -m 0640 /secure/path/targets.allow \
  /etc/topo-worker/targets.allow
sudo install -o root -g topo-worker -m 0640 /secure/path/verified_known_hosts \
  /etc/topo-worker/known_hosts
sudoedit /etc/topo-worker/topo-worker.env
```

The allowlist may contain canonical IPv4 CIDRs, one per line, but should be no
broader than the deployment requires. ServiceNow target `/32`s must fall within
it. The `known_hosts` file must contain the port-22 identity for every target.

## 5. Preflight, start, and run discovery

### Foreground macOS worker

Keep the OAuth token, target allowlist and verified `known_hosts` in an
owner-only directory; keep the token file mode `0600`. Use absolute paths and
never put token contents in command arguments. With the same ServiceNow pool,
site and identities configured above, run:

```sh
topo worker check \
  -servicenow-instance https://your-instance.service-now.com \
  -token-ref file:/absolute/private/path/worker-token \
  -worker-pool pilot-linux -site pilot-site \
  -allow-ssh-linux \
  -ssh-target-allowlist /absolute/private/path/targets.allow \
  -ssh-known-hosts /absolute/private/path/known_hosts \
  -max-concurrency 2
```

After `status: "ready"`, repeat that command with `worker run` instead of
`worker check`. Leave it running while you start the ServiceNow scan; Ctrl-C
stops it. Local discovery in package tests is separate from this authenticated
ServiceNow worker flow. The Linux service commands below do not apply to macOS.

### Packaged Linux worker

Run the same preflight that systemd uses. A successful response is bounded
JSON with `status: "ready"`, the ephemeral worker/boot IDs, pool/site, and
capabilities. `check` registers and heartbeats once; it never claims a task,
retrieves a credential, dials a target, submits a result, or starts a listener.

```sh
sudo -u topo-worker /usr/bin/topo worker check \
  -servicenow-instance https://your-instance.service-now.com \
  -token-ref file:/etc/topo-worker/worker-token \
  -worker-pool pilot-linux \
  -site pilot-site \
  -allow-ssh-linux \
  -ssh-target-allowlist /etc/topo-worker/targets.allow \
  -ssh-known-hosts /etc/topo-worker/known_hosts \
  -max-concurrency 2

sudo systemctl enable --now topo-worker.service
sudo systemctl status topo-worker.service
```

In ServiceNow, open the active discovery profile and select **Run now**. Inspect
**Runs**, then its tasks and IRE delivery. A successful run becomes terminal
and reports bounded asset, relationship, collection-error, and attempt counts.
Confirm the resulting CIs through CMDB views; Topo itself writes them only
through IRE. Repeat **Run now** and confirm reconciliation rather than duplicate
CIs/relationships before activating the schedule.

## 6. Upgrade and remove the pilot

Stop the worker before changing its local policy or upgrading the app. Upgrade
Topo through the same package family, rerun `worker check`, then restart it.
Upgrade XML-installed apps only through the [reviewed XML upgrade path](servicenow-update-set.md#upgrades-and-recovery).
Keep developer SDK instances separate; do not silently switch delivery methods.

Package removal leaves `/etc/topo-worker` untouched. To end a pilot, first
disable schedules and profiles, stop/disable the worker, revoke its OAuth
token/client access, deactivate the worker integration user and Password2
credential, remove the package, and only then delete operator-owned files if
the customer's retention policy permits it.

## Evidence boundary and known gaps

The architecture, worker/API denial matrix, manual and scheduled sanitized
Docker discovery, repeated IRE reconciliation, lease recovery, and raw-result
retention are already validated separately against `dev441060`; deterministic
scale results remain simulator-only. This onboarding slice adds packaging,
preflight, and install evidence—it does not reclassify simulator results as
ServiceNow throughput evidence.

Still required before a broad production claim: real clean-install/repeat/upgrade XML delivery evidence, N-1 stable upgrade evidence, external Vault bindings,
Password2 clone/backup operational
guidance, broader CI/protocol mappings, platform volume/upgrade testing, and
independent security-review retest. The shipped scoped app has no npm runtime
dependency tree, but the pinned ServiceNow SDK 4.9.0 build-only tree currently
reports nine moderate and two high transitive advisories; keep app builds on a
short-lived trusted builder while that upstream toolchain exposure is tracked.
Report pilot feedback without secrets, tokens, private hostnames/addresses,
raw observations, or credential values.
