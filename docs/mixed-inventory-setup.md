# Managed Linux and Windows candidate setup

**Review guide for the unpublished 0.4.7 candidate.** The public app is still
0.4.6 Beta and the public worker is `v0.4.6-beta.1`: follow the
[Linux setup](pilot-quickstart.md) for those releases. Package-manager installation
currently obtains that Linux-only managed release, not this candidate.

This guide is pinned to integrated [PR #79](https://github.com/Nischoy-ai/topo/pull/79)
source `1c6ddb6b7f684db749fe0180c34de0d2e81af2d9`, including the Windows
runtime from [PR #80](https://github.com/Nischoy-ai/topo/pull/80) source
`5a94445e5765b8176a5eec8566c930eb4a6a9743`. Candidate app and worker must
come from the reviewed matching source. A version label alone does not identify
that source. Record the binary checksum and build source alongside `topo version`.
There is no candidate public download or signed channel cutover instruction here.

## 1. Establish the installation and target prerequisites

Use an authorized disposable ServiceNow environment with a source-verified
candidate app and all 32 required ordered indexes. The
[XML installation procedure](servicenow-update-set.md) describes the native
workflow, but its download is the published 0.4.6 package. The older candidate
base export lacks the final lease correction: do not treat it as this pinned
candidate. A matching native export, empty-instance clean installation and
populated-data upgrade remain acceptance gates. Do not reset an existing
installation to manufacture clean-install evidence. Register the `Nischoy Topo`
discovery source through the reviewed Fix Script before publishing.

Use a Linux worker for the exercised managed path. Linux/macOS workers can
implement these operations; macOS managed mixed-runtime acceptance remains
open. Windows worker publication and Windows service registration are separate.
The worker needs outbound HTTPS to ServiceNow and network access to approved
SSH/22 and WinRM HTTPS/5986 targets. No inbound worker listener is required.

For Linux, use a non-admin SSH account able to read the reviewed computer and
interface inventory. Verify each SSH host-key fingerprint through an independent
trusted path before creating the worker's `known_hosts`; keyscan alone does not
establish identity. Keys must identify the selected IP on port 22.

For Windows, have a target administrator provision a non-admin scan account,
Remote Management Users membership and read/remote access to WMI `root/cimv2`.
Use the [Windows compatibility guidance](winrm-discovery.md) to check the
required fixed queries. Enable an HTTPS/5986 listener and firewall access only
from approved workers. Keep Basic and unencrypted authentication disabled.
NTLMv2 requires verified TLS for server identity; provision a valid server
certificate whose IP SAN includes each selected target IP. Establish CA trust
independently and install the trusted PEM bundle on the worker. DNS-only SANs
are insufficient for this IP-selected workflow. No TrustedHosts or TLS bypass
is a supported remedy. Changing an IP requires certificate/trust and local
policy review; cross-address identity acceptance remains open.

Windows collection includes computer, BIOS/OS identity and IP-enabled adapters.
It runs no WinRS shell, arbitrary PowerShell, disk, software, patch or service
inventory. Publication is only `cmdb_ci_computer`, `cmdb_ci_network_adapter`
and `host_has_interface` mapped to `Owns::Owned by` through IRE.

## 2. Separate human access from worker credentials

Create the [least-privilege identities and exact seven POST resource grants](pilot-quickstart.md#2-create-the-least-privilege-servicenow-identities):
app administrator, operator, viewer, credential custodian
(`x_664635_topo.credential_admin`) and a dedicated machine user with only
`x_664635_topo.worker`. Disable API policy wildcards and generic Table API
access. The worker receives an OAuth token file, not CMDB/IRE administration
rights or credential-table access.

The custodian enters target usernames/passwords in **Remote Credentials** using
protected Password2 fields. The underlying legacy SSH-named table is unchanged;
its label does not imply SSH-only candidate support. Use a separate credential
and binding for each protocol. Never put target passwords or OAuth tokens in
CLI values, labels, scope records, observation attributes or logs. Transfer the
OAuth token directly into a restricted file through the normal secure workflow.
Review [Password2 storage and rotation](deployment-security.md#where-ssh-passwords-live).
The broker releases a password only for the authorized live attempt, lease,
profile revision, scope revision and protocol; the worker checks local target
policy before requesting it.

## 3. Create one profile per protocol

In **Nischoy Topo**, create records in this order. Start with a single approved
`/32` per protocol and keep schedules inactive.

1. **Worker Pools**: choose a pool/site and dedicated worker service user.
   Start with lease seconds `120`, maximum task seconds `300`, maximum leases `2`.
2. **Target Scopes**: create distinct Linux and Windows scope IDs at revision
   `1`, tied to the pool/site. Enter canonical IPv4 CIDRs and explicit CIDR
   exclusions; set **IPv4 partition prefix** to `32`.
3. **Remote Credentials**: enter the non-admin SSH or Windows account and
   protected password as the credential custodian.
4. **Credential Bindings**: create separate immutable bindings, revision `1`,
   each pointing to its credential, exact scope/revision and allowed profile
   ID/revision. Use protocol `ssh_password` for Linux and
   `winrm_ntlm_password` for Windows; never reuse one across protocols.
5. **Discovery Profiles**: create one `ssh_linux.v1` profile and one
   `winrm_windows.v1` profile, each revision `1`, schema `v1alpha1`, with its
   matching pool, scope and binding. Activate only the records for the first test.

Referenced authority is immutable: create new revisions and matching bindings
for scope/profile changes rather than editing an existing authorized revision.
Overlaps are deduplicated and exclusions removed before planning. At most
**1,024 selected addresses per scope/run** may remain; 1,025 fails planning.
Network, broadcast, gateways and reserved addresses are not removed automatically.
For example `192.0.2.0/29`, excluding `.0/32`, `.4/31`, `.7/32`, selects
`.1`, `.2`, `.3`, `.6`. Use full CIDRs in the actual fields. This is a credentialed
sweep, not OS detection: only place the intended protocol's targets in its scope.
See the [selection contract](mixed-inventory-pilot.md#subnet-selection-and-bounds).

## 4. Check and start the matching candidate worker

Keep the token and all startup files under absolute, restricted paths, readable
by the worker account. On Linux use root-owned policy files with worker-group
read access (directory `0750`, files `0640`); on a foreground personal worker
use an owner-only directory and token mode `0600`. Allowlist files contain
canonical IPv4 CIDRs, one per line, and should grant only approved targets.
Prepare separate SSH/Windows allowlists, verified `known_hosts`, and trusted
WinRM CA PEM. A ServiceNow scope cannot widen these local grants.

The following uses a reviewer-provided candidate binary at a disposable path;
replace the instance, pool/site and paths. It leaves the installed release alone.

```sh
/absolute/candidate/topo worker check \
  -servicenow-instance https://your-instance.service-now.com \
  -token-ref file:/absolute/private/worker-token \
  -worker-pool pilot-mixed -site pilot-site \
  -allow-ssh-linux \
  -ssh-target-allowlist /absolute/private/linux-targets.allow \
  -ssh-known-hosts /absolute/private/known_hosts \
  -allow-winrm-windows \
  -winrm-target-allowlist /absolute/private/windows-targets.allow \
  -winrm-ca-certs /absolute/private/windows-ca-certs.pem \
  -remote-start-interval 1s -max-concurrency 2
```

`status: "ready"` proves registration and one heartbeat, not target access,
credential release or IRE. Check claims no tasks. Repeat the exact command with
`worker run` in place of `worker check` and keep it running; Ctrl-C stops it.
For a single protocol omit the other protocol's enable flag and both files.

The shipped Linux service enables SSH only. Adding Windows files or variables
to its environment file does not enable Windows. A future candidate service
configuration must use the same approved flags in **both** its preflight and
run commands; service packaging/cutover is not established by this foreground
example. Do not replace the current installed service for this review.

Startup files are read once. Stop, revise policy/trust, rerun check with identical
intended flags and restart to load changes. Policy registration binds allowlists,
SSH host-key digest, WinRM trust digest and start interval. OAuth token rotation
also requires restarting the process to resolve the replacement token.

Starts are spaced at least one second by default across both protocols in one
worker; the supported interval is 100 ms–1 minute without burst credit.
Concurrency `2` limits leased tasks, not a guarantee of two simultaneous target
connections. Pool lease capacity is another ceiling. With N workers and interval
T, budget up to N/T target starts per second and at most the lesser of pool
capacity and summed worker concurrency in outstanding leases. Authentication
and SOAP message volume are not bounded by the start rate itself.

## 5. Verify manual runs before scheduling

Choose **Discovery Profiles → Run now** separately for Linux and Windows.
Inspect **Runs**, tasks, collection errors and IRE delivery. Check actual CMDB
source identities and ownership relationships, then repeat each profile and
confirm the same CIs/relations reconcile. A terminal task or ready worker alone
is insufficient: an unreachable target can finish with errors and no inventory.
Validated empty deliveries do not call IRE.

Only after successful manual repeats create/activate a Topo **Schedule** with
the matching profile, bounded interval and next-run time. These are Topo
schedules, not native ServiceNow Discovery schedules. Keep intervals appropriate
to target count, pacing and task deadlines. Use **Cancel run** to stop a run;
inspect terminal tasks and lease expiry rather than assuming immediate remote
shutdown. Waiting tasks retain renewable leases and stop on cancellation or
deadline. Restart uses a new worker boot; real Windows cancelled-expiry slot
recovery is tested, while broader mixed restart acceptance remains open.

## Troubleshooting

| Symptom | Action |
| --- | --- |
| Worker check fails | Verify matching binary, absolute readable files, PEM/allowlist syntax, instance HTTPS origin, pool/site, worker role and exact OAuth grants. Do not print the token. |
| Ready worker claims nothing | Check profile/scope/binding activation and revisions, pool identity/capacity, run tasks and existing leases. Check never starts discovery. |
| Local-policy denial | Narrow or correct the ServiceNow scope, or obtain local authorization and restart with reviewed files. Do not broaden policy solely to silence the error. |
| SSH host-key mismatch | Verify target identity out of band, then update the approved key and restart. Do not disable host verification. |
| WinRM certificate failure | Check IP SAN, validity, chain, correct independently trusted PEM and reachability on 5986; restart after trust updates. |
| Authentication or WMI denial | Custodian checks the protocol-specific username/password and binding; target administrator checks non-admin WMI read/remote permissions. No administrator credential or shell grant is required by this inventory scope. |
| Credential broker denial | Check exact protocol/profile/scope revisions and live attempt/lease ownership. Fix authority mismatch; do not grant generic credential-table reads. |
| Timeout/empty result | Check routing/firewall/listener and task budget. Review structured errors; other authorized hosts may still succeed. |
| Cancelled task appears occupied | Wait through lease expiry and inspect recovery. Ensure the app includes the pinned lease-clearing fix; older candidate exports lack it. |
| IRE failure or unexpected duplicates | Confirm source registration, permitted mappings and identification/reconciliation rules; inspect sanitized delivery receipts and source keys before another apply. |

## Compatibility and remaining gates

| Area | Evidence at this source boundary | Limit |
| --- | --- | --- |
| Public release | Signed 0.4.6 worker channels and native 0.4.6 XML | Managed Linux explicit `/32` only; no candidate support shipped |
| Ubuntu 24.04 SSH | Real direct repeats and managed subnet/broker/IRE repeats | Three-address bounded lab scope; no broad estate guarantee |
| Windows Server 2022/2025 | Real non-admin HTTPS/NTLM managed repeats, negatives and cancelled-expiry recovery | Restricted computer/interfaces; no domain/Kerberos or address-change proof |
| Subnet bounds | Real exclusions, local denial, 1,024 planning ceiling, pacing, pool capacity, cancellation, unreachable isolation | No 1,024-host live scan; collection did not demonstrate two simultaneous SSH connections |
| Source/simulation | Reviewed operations, mapper, broker, planner and worker fixtures; 17 CI checks green on each pinned PR | Public-channel jobs test unchanged shipped release; fixtures are not live acceptance |
| Native installation | Existing-instance 0.4.7 upgrade and 32 ordered physical index combinations | Matching final export, empty-instance clean install, populated-data upgrade and every unique constraint remain open |
| Release/compatibility expansion | Candidate source exists | Signed candidate/channel publication, macOS managed runtime, combined mixed/address-change/restart acceptance remain open |

Exact dated sources, executable checksums, run IDs and limitations are in
[mixed evidence](evidence/mixed-inventory.md) and the Windows-owned
[PR #80 evidence](https://github.com/Nischoy-ai/topo/pull/80). Earlier pending
checkpoints in the evidence history must be read with their later scoped results.
This guide adds no new runtime or installation evidence. Release cutover must
update downloads, source pins, verification, service configuration and supported
scope together after the remaining gates and publication; until then use the
published Linux guide for customer installations.
