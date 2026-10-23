# Product roadmap

Topo discovers infrastructure through locally approved operations and publishes
normalized observations to destination systems. ServiceNow controls the current
managed Linux/Windows discovery workflow and reconciles supported CIs through IRE.

## Current release

**Topo 0.4.7** provides the published combined ServiceNow application/index
XML and the signed 0.4.7 worker through Linux APT/RPM and macOS
Homebrew. See
[XML installation](docs/servicenow-update-set.md) and
[worker availability](docs/distribution.md#release-availability) for the actual
download and channel status. Dated build and installation results are in the
[distribution evidence](docs/evidence/distribution.md#047-beta-worker).

Windows package publication and stable channels are planned. The macOS CLI
formula does not use Apple Developer ID signing or notarization.

[Deployment security](docs/deployment-security.md) describes the managed
credential lifecycle and the separate standalone-controller storage controls.

## Release engineering baseline

Release/security builds use exact **Go 1.26.10** with Go 1.26 compatibility.
The 2026-10-08 vulnerability scan required this patch and `golang.org/x/net`
0.60.0; the [security record](docs/security-review.md#2026-10-08-build-baseline-update)
retains the affected scan and verification scope. This patch update preserves
M3 capability priorities.

<a id="supported-customer-beta-path"></a>

## Supported customer path

| Area | Supported scope | Evidence and guide |
| --- | --- | --- |
| Managed discovery | Linux SSH/22 and Windows HTTPS/5986 NTLM computer/interfaces; separate protocol profiles, bounded IPv4 CIDRs/exclusions, local trust/allowlists, Password2 broker, leases and schedules. | [Linux/Windows setup](docs/mixed-inventory-setup.md) · [Managed worker evidence](docs/evidence/servicenow-worker.md) |
| CMDB publication | Only computers (`cmdb_ci_computer`), network adapters (`cmdb_ci_network_adapter`), and one relationship: `host_has_interface` → `Owns::Owned by`. | [Mapping boundary](docs/servicenow.md#reviewed-mapping-boundary) · [Real IRE evidence](docs/evidence/servicenow-ire.md) |
| Installation | Combined ServiceNow XML plus signed Linux APT/RPM and macOS Homebrew workers; dated restoration and channel checks. Upgrade evidence is historical; populated 0.4.7 upgrade remains deferred/not passed. | [XML validation](docs/servicenow-validation.md) · [Distribution evidence](docs/evidence/distribution.md) |

This is a focused computer/interface release, not broad estate replacement coverage.
Real mixed managed evidence uses Linux workers. Windows installers are not published,
and macOS managed mixed runtime is not independently exercised.

## Other source components and validation limits

The following components exist in the source tree for standalone operation or
evaluation. Their implementation does not make them supported managed 0.4.7
capabilities. Simulation demonstrates behavior under the fixture's conditions;
it does not establish real-system compatibility or production readiness.

| Component | Implemented scope | Validation limit | Guide |
| --- | --- | --- | --- |
| Standalone host discovery | Local discovery and reviewed SSH operations | Separate from the managed target policy; real IRE evidence covers only the documented classes and runs. | [SSH](docs/ssh-discovery.md) |
| Windows discovery and agent service | Fixed WinRM inventory operations and Windows service wrapper | Broader WinRM inventory remains fixture-tested. The computer/interface release passed direct collection on Windows Server 2022/2025; scoped managed repeats and negative gates passed in [candidate PR #80](https://github.com/Nischoy-ai/topo/pull/80); broader acceptance remains open. Service registration checked by cross-compilation and code review only; real Windows Service Control Manager unverified. | [WinRM](docs/winrm-discovery.md) · [Agent](docs/topo-agent.md) |
| SNMP | SNMPv3 MIB-II device/interface inventory | Lab wire-protocol fixture uses `noAuthNoPriv`; production `authPriv` has no real-equipment validation. | [SNMP](docs/snmp.md) |
| VMware | Read-only host/VM inventory | HTTPS/authenticated `vcsim` tests; real vCenter/ESXi unverified. | [VMware](docs/vmware.md) |
| Kubernetes | Node/Pod inventory | Topo Lab API fixture; live cluster unverified. | [Kubernetes](docs/kubernetes.md) |
| AWS | Organizations account/OU structure | Simulation evidence; no live-account compatibility claim. No per-account resource inventory. | [AWS](docs/aws.md) |
| Azure | Tenant management-group/subscription structure | Fixture evidence; live-tenant acceptance absent and Reader authorization unresolved. No per-subscription resource inventory. | [Azure](docs/azure.md) |
| Inventory resolution | Source precedence, conflicts and freshness | Standalone inventory behavior; broader relationship precedence and cross-ID correlation remain planned. | [Source resolution](docs/source-resolution.md) |
| Standalone controller and collectors | SQLite, audit, schedules, backup/restore, enrollment, mTLS, rotation/revocation, heartbeats and jobs | Separate deployment with documented persistence and recovery limits; not required by the managed release. | [Storage](docs/storage.md) · [Enrollment](docs/enrollment.md) |
| Credential references | Bounded env/file, Vault KV2 and Kubernetes Secret adapters | Provider-specific tests and deployment guidance; separate from the managed Password2 broker. | [Credential references](docs/credential-references.md) |

The [evidence index](docs/evidence/README.md) records dated real-system results.
Simulator scale gates do not prove sustained customer-estate capacity.

## Current development focus

M3 — hybrid discovery and managed ServiceNow deployment — remains the current
milestone. Historical 0.4.6 combined-package installation and focused upgrade
evidence remain recorded; populated 0.4.7 upgrade is deferred/not passed.
Customer documentation now covers the bounded Linux/Windows workflow.
The published **ServiceNow-managed Linux and Windows computer/interface release**
includes allowlisted IPv4 CIDRs, explicit exclusions and bounded execution.
Linux subnet, Windows managed and reliability acceptance retain their dated
scope; actual network-outage, address-change and midcollection/IRE interruption
behavior remain unexercised. Native restoration on a used PDI and all 32 ordered
physical index definitions were verified. The sealed combined export was compared
offline, not newly committed as the final combined digest on a pristine instance.
Populated upgrade is deferred/not passed; additional unique-enforcement probes
were owner-excluded/not independently exercised. Neither was required for this release.
Follow the [setup guide](docs/mixed-inventory-setup.md) and [acceptance record](docs/mixed-inventory-pilot.md).
Windows worker publication remains separate.

## Planned capabilities

- Additional Kubernetes workload kinds and AWS/Azure resource inventory.
- Relationship precedence and correlation across source identities.
- Additional IRE class and relationship mappings with verified dependency contracts.
- Broader real-system compatibility, upgrade and sustained-capacity testing.
- SSO/RBAC commercial modules behind documented open interfaces.
- Stable release channels, previous-version channel upgrade evidence, and
  Windows signing/distribution.
- Rate-limited allowlisted sweeps and LLDP/CDP topology discovery.
- High availability and PostgreSQL when a multi-controller deployment requires them.

Plans are subject to testing and review. Topo-owned discovery published through
supported IRE APIs is the ServiceNow integration direction. Experimental Relay
and ECC transports are documented for developers and are separate from the
customer installation path.
