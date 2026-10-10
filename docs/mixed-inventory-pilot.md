# Managed Linux and Windows inventory pilot

This is the acceptance plan for the **unpublished 0.4.7 source candidate**.
The published 0.4.6 app and `v0.4.6-beta.1` worker still support only the
managed Linux path described in the [setup guide](pilot-quickstart.md).
Do not use these candidate flags with that published worker or install the
candidate over an existing development installation to bypass acceptance.

The target is ServiceNow-managed computer and network-adapter inventory on
Linux and Windows, using bounded, explicitly authorized IPv4 subnet selection.
It does not claim broad Device42 replacement coverage. Windows targets can be
collected by Linux/macOS workers; Windows worker packaging is a separate scope.

## Candidate contract

- `ssh_linux.v1`: reviewed SSH Linux operations on port 22 with verified
  `known_hosts`, and an immutable `ssh_password` binding.
- `winrm_windows.v1`: only computer identity, BIOS/OS identity and IP-enabled
  network adapters, with an immutable `winrm_ntlm_password` binding. HTTPS port
  5986, verified certificate chain/IP SAN and NTLMv2 are required. No WinRS
  shell, software, disk, service or patch operation runs in this mode.
- Both protocols use the existing attempt-, lease-, profile- and scope-bound
  Password2 broker. Worker identities cannot read credential tables. The
  legacy `x_664635_topo_ssh_credential` table keeps its name and ACLs; its UI
  label becomes **Remote Credentials** and username capacity becomes 256
  characters. Binding protocol determines the permitted username form and
  prevents an SSH binding from authorizing a Windows task, or vice versa.
- Publication keeps two classes and one relationship: `cmdb_ci_computer`,
  `cmdb_ci_network_adapter`, and `host_has_interface` → `Owns::Owned by`.
  Additional inventory categories are separate mapping slices.

## Subnet selection and bounds

Create separate protocol profiles and immutable target-scope revisions. A scope
may contain IPv4 CIDRs and explicit CIDR exclusions. Set **IPv4 partition
prefix** to `32`; overlapping selections are deduplicated and exclusions are
removed before expansion. At most **1,024 selected addresses** may remain per
scope/run. Larger selections fail during planning, before any scan is queued.
Each address becomes one deterministic task, retaining existing lease recovery,
cancellation and result bounds.

For example, selecting `192.0.2.0/29` and excluding `192.0.2.0/32`,
`192.0.2.4/31` and `192.0.2.7/32` creates tasks for `.1`, `.2`, `.3` and `.6`.
Network, broadcast, gateway and other reserved addresses are not automatically
removed; the operator must specify exclusions appropriate to the network.

The worker independently checks every task against its local protocol allowlist
before credential resolution or a target connection. A ServiceNow profile
cannot expand that authority. An out-of-policy task fails rather than silently
changing the planned target set. SSH/WinRM attempts collect identity through
those protocols; this is a credentialed sweep, with no generic port scanner,
OS fingerprinting, ICMP dependency, automatic trust enrollment or IPv6 support.
Unreachable or untrusted targets report collection errors and create no CIs.

Target attempts start at most once per second by default, shared across both
remote protocols in one worker. `-remote-start-interval` can select 100 ms–1 min;
there is no accumulated burst credit. This is a target-attempt rate, not a
limit on individual authentication/SOAP messages. Waiting attempts retain
renewable leases and stop on cancellation or task deadline. Local discovery
bypasses this remote limiter. `-max-concurrency` and ServiceNow pool capacity
remain independent limits. Multiple workers each have their own rate limit;
size a pool's worker count and concurrency to bound aggregate traffic.

## Candidate worker configuration

The existing SSH flags remain. Add these flags when Windows collection is
explicitly authorized:

```sh
-allow-winrm-windows \
-winrm-target-allowlist /etc/topo-worker/windows-targets.allow \
-winrm-ca-certs /etc/topo-worker/windows-ca-certs.pem \
-remote-start-interval 1s
```

Both file paths must be absolute. Trust certificates through an independent
administrative path before placing them in the PEM bundle; target IP SAN
verification remains enabled. The roots, target allowlists and start interval
are bound into the registered policy digest. Startup files are read once;
restart after changing local policy or trust. There is no TLS bypass or Basic
fallback in the managed Windows operation. Use `topo worker check` before `run`.

The candidate standalone `topo discover winrm -host-interfaces-only` uses the
same restricted inventory selection for focused compatibility tests. It does
not exercise the ServiceNow credential broker or prove app installation.

## Acceptance status

| Gate | Status |
| --- | --- |
| Fixed operations, local authorization, protocol-bound broker, no-data mapping, bounded CIDR expansion and cancellation | Source tests; separate from real ServiceNow enforcement |
| Windows Server 2022 and 2025 computer/interface collection as non-admin | Two real scans per host passed with verified TLS, zero errors and stable identity; production executor and offline IRE preview exercised |
| Ubuntu 24.04 computer/interface collection | Two candidate production-executor scans passed with verified host keys: 3 assets/2 relationships, zero errors, stable identities and offline IRE preview |
| Windows WQL and HTTP/2 compatibility regressions | Source fixes plus real-host collection evidence; published worker unchanged |
| Mixed managed runs, real Password2/ACL/OAuth enforcement and repeated IRE apply | Pending candidate deployment to an authorized disposable ServiceNow instance |
| Real bounded subnet/exclusions, closed ports, wrong credentials/trust and cancellation | Pending full managed acceptance |
| Address-change identity and scheduling/lease recovery across the real mixed estate | Pending |
| Clean candidate app installation and data-preserving upgrade | Pending |
| Signed candidate worker, native XML and public-channel acceptance | Pending; preceding release assets remain intact |

Completion requires the real managed gates above and immutable version/source
attribution in the [evidence record](evidence/mixed-inventory.md). Fixture tests
and direct collector runs do not close those gates. The public beta scope will
change only after acceptance and publication.
