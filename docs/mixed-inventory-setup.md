# Managed Linux and Windows discovery setup

New setup? Start with [your first discovery run](first-success.md) for the
short Linux path, then return here for detailed configuration.

The signed 0.4.7 worker and the Topo 0.4.7 native package are both on the
[v0.4.7-beta.2 release](https://github.com/Nischoy-ai/topo/releases/tag/v0.4.7-beta.2).
Install the worker from there or through the [signed channels](distribution.md),
which provide `v0.4.7-beta.1` with the same worker code. Confirm
`topo version` starts with `v0.4.7` before configuring it.

The 0.4.7 workflow discovers computers and IP-enabled interfaces on Linux over
verified SSH/22 and Windows over verified HTTPS/5986 with NTLMv2. Linux and macOS
worker packages do not imply Windows installer publication or broader target
inventory. Real managed mixed-path evidence uses Linux workers; macOS managed
mixed runtime remains unexercised. Record the exact worker version, build source
and binary checksum. Earlier evidence retains its original [integrated source](evidence/mixed-inventory.md)
and Windows attribution; a version label alone is not that source identity.

## 1. Establish the installation and target prerequisites

Use an authorized development ServiceNow environment and follow your change
process. The prepared native 0.4.7 customer package contains one unchanged
combined XML, 474 metadata updates and three distinct update sets: app, 32 index
definitions and the Global discovery-source Fix Script. Its XML SHA-256 is
`45a13e48c675b77d11e2ff1610ff04b0e63cd396daad22f2c529d2be9b05b721`
(1,418,336 bytes). The native export repeats an identical parent record; this
is still three distinct sets. Download it from the [v0.4.7-beta.2 release](https://github.com/Nischoy-ai/topo/releases/tag/v0.4.7-beta.2).

Verify its signed checksum manifest using the exact protected
workflow commit in the release notes before importing. Then import through
**Retrieved Update Sets → Import Update Set from XML**, preview and review all
problems, commit the complete batch, and run **Nischoy Topo — Register Discovery
Source** with **Unloadable** cleared. Use the package's INSTALLATION.md and
INDEXES.md as the version-matched installation and ordered-index checklists.
A historical Committed receipt alone does not prove physical index presence
after table recreation. Keep workers stopped and schedules inactive until these
checks complete; do not bypass active platform operations or preview problems.

The exact combined export was compared offline with reviewed inputs. Earlier
native evidence restored its source-matched app on a used instance with 437
reviewed deletion-history collisions, followed by supported companion repair
and fresh verification of all 32 ordered physical definitions. That is separate
component evidence, not a new combined-digest or pristine-instance install.
Independent enforcement of the 17 unique definitions was not exercised;
additional duplicate-insert tests are owner-excluded, relying on reviewed platform
configuration. Populated upgrade testing is deferred/not passed. These omissions remain
explicit validation limits; neither was required by the approved release scope. See the [dated evidence](evidence/mixed-inventory.md).

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
Use `SERVERNAME\username` for a local Windows account; domain account
compatibility is not established. Environments that disable NTLM are unsupported
by this release; preserve domain policy and wait for a supported authentication
mode rather than weakening it. Kerberos remains deferred.
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
its label does not imply SSH-only support. Use a separate credential
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

## 4. Check and start the matching worker

Keep the token and all startup files under absolute, restricted paths, readable
by the worker account. On Linux use root-owned policy files with worker-group
read access (directory `0750`, files `0640`); on a foreground personal worker
use an owner-only directory and token mode `0600`. Allowlist files contain
canonical IPv4 CIDRs, one per line, and should grant only approved targets.
Prepare separate SSH/Windows allowlists, verified `known_hosts`, and trusted
WinRM CA PEM. A ServiceNow scope cannot widen these local grants.

Install and verify a 0.4.7 worker through its signed release
or the release package-manager channels. Confirm `topo version`, then
replace the instance, pool/site and paths in this installed-binary example.

```sh
topo worker check \
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
to its environment file does not enable Windows. A matching mixed service
configuration must use the same approved flags in **both** its preflight and
run commands; service packaging/cutover is not established by this foreground
example. Use the explicit foreground command for mixed targets until a matching reviewed
service configuration is provided.

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
recovery is tested. Later bounded Linux/Windows scenarios exercised native
scheduled dispatch, cancellation and healthy follow-up scans, plus actual
process kill/restart before credentials. A 45-second process-local HTTP-client
fault recovered; this was not an actual network outage. Restart during collection
or IRE, address changes and service/reboot recovery were not exercised.

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
| Public release | Signed v0.4.7-beta.1 worker channels and native 0.4.7 XML | Focused Linux/Windows computer/interfaces; Windows installers and Apple identity/notarization excluded |
| Ubuntu 24.04 SSH | Real direct repeats and managed subnet/broker/IRE repeats | Three-address bounded lab scope; no broad estate guarantee |
| Windows Server 2022/2025 | Real non-admin HTTPS/NTLM managed repeats, negatives and cancelled-expiry recovery | Restricted computer/interfaces; no domain/Kerberos or address-change proof |
| Subnet bounds | Real exclusions, local denial, 1,024 planning ceiling, pacing, pool capacity, cancellation, unreachable isolation | No 1,024-host live scan; collection did not demonstrate two simultaneous SSH connections |
| Response bounds | Real Windows responses rejected under a 64-byte local budget | Production 1 MiB oversize remains fixture-tested |
| Source/simulation | Reviewed operations, mapper, broker, planner and worker fixtures; 17 CI checks green on each pinned PR | Earlier pinned PR channel jobs exercised historical 0.4.6; new 0.4.7 channel results are recorded separately in distribution evidence. Source fixtures are not live acceptance |
| Native installation | Source-matched used-instance app restoration, critical script/twelve table IDs and all 32 required ordered physical index definitions verified after companion repair | Base retained 437 reviewed history collisions; not pristine-instance proof. Populated upgrade deferred/not passed; 17 unique enforcement not independently exercised, owner-excluded |
| Bounded reliability | Separate Linux/Windows manual/repeat, native schedules, pre-credential process restart, process-local 45-second client interruption, cancellation and healthy follow-up; 18 Applied receipts retained seven CI/four relationship identities | Actual network outage, address change and midcollection/midIRE restart unexercised; no general outage/reboot guarantee |
| Compatibility expansion | Signed 0.4.7 is published | macOS managed mixed runtime and broader inventory/compatibility remain unexercised |

Exact dated sources, executable checksums, run IDs and limitations are in
[mixed evidence](evidence/mixed-inventory.md) and the Windows-owned
[PR #80 evidence](https://github.com/Nischoy-ai/topo/pull/80). Earlier pending
checkpoints in the evidence history must be read with their later scoped results.
The later reliability observer used runtime `06e6eca`, test-only source
`a2115e6b16f4e60ee54701592028bc5d237f43ee` and Linux binary SHA-256
`a8fd4d3514f75a826c6aad31d0af1a2dc90ca182c3dbbb5aac996066839bfe0d`.
Fourteen native runs yielded eighteen complete/three cancelled tasks, with
eighteen Applied IRE receipts and identical baseline/final source identities.
The local-only observer is not a published worker. This guide summarizes
existing evidence; it generates no new runtime or installation proof. Published worker signatures/provenance and native package checksums authenticate
released bytes; they do not expand the dated live acceptance scope. Original
0.4.6 release assets remain preserved. Use the [Linux setup](pilot-quickstart.md)
for an SSH-only deployment, or this guide for separate Linux/Windows profiles.
