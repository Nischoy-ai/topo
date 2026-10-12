# Product roadmap

Topo discovers infrastructure through locally approved operations and publishes
normalized observations to destination systems. ServiceNow controls the current
managed Linux discovery workflow and reconciles supported CIs through IRE.

## Current release

**Topo 0.4.6 Beta** provides the published combined ServiceNow application/index
XML and the signed worker `v0.4.6-beta.1` through Linux APT/RPM and macOS
Homebrew. See
[XML installation](docs/servicenow-update-set.md) and
[worker availability](docs/distribution.md#release-availability) for the actual
download and channel status. Dated build and installation results are in the
[distribution evidence](docs/evidence/distribution.md#046-beta-worker).

Windows package publication and stable channels are planned. The macOS CLI
formula does not use Apple Developer ID signing or notarization.

[Deployment security](docs/deployment-security.md) describes the managed
credential lifecycle and the separate standalone-controller storage controls.

## Release engineering baseline

Release/security builds use exact **Go 1.26.9** with Go 1.26 compatibility.
The 2026-10-08 vulnerability scan required this patch and `golang.org/x/net`
0.60.0; the [security record](docs/security-review.md#2026-10-08-build-baseline-update)
retains the affected scan and verification scope. This patch update preserves
M3 capability priorities.

## Supported customer beta path

| Area | Beta scope | Evidence and guide |
| --- | --- | --- |
| Managed discovery | Explicitly listed IPv4 Linux hosts over SSH port 22; Password2 broker, outbound stateless workers, leases, manual runs and schedules. No subnet scanning or Windows targets. | [Linux setup](docs/pilot-quickstart.md) · [Managed worker evidence](docs/evidence/servicenow-worker.md) |
| CMDB publication | Only computers (`cmdb_ci_computer`), network adapters (`cmdb_ci_network_adapter`), and one relationship: `host_has_interface` → `Owns::Owned by`. | [Mapping boundary](docs/servicenow.md#reviewed-mapping-boundary) · [Real IRE evidence](docs/evidence/servicenow-ire.md) |
| Installation | Combined ServiceNow XML plus signed Linux APT/RPM and macOS Homebrew workers; dated install and upgrade checks. | [XML validation](docs/servicenow-validation.md) · [Distribution evidence](docs/evidence/distribution.md) |

This is a focused Linux pilot, not a replacement for Device42's broad estate
coverage. Linux and macOS worker packages do not imply discovery coverage for
those operating systems beyond the target scope above. Windows discovery is
not available in the managed beta, and Windows packages are not published.

## Other source components and validation limits

The following components exist in the source tree for standalone operation or
evaluation. Their implementation does not make them supported managed-beta
capabilities. Simulation demonstrates behavior under the fixture's conditions;
it does not establish real-system compatibility or production readiness.

| Component | Implemented scope | Validation limit | Guide |
| --- | --- | --- | --- |
| Standalone host discovery | Local discovery and reviewed SSH operations | Separate from the managed target policy; real IRE evidence covers only the documented classes and runs. | [SSH](docs/ssh-discovery.md) |
| Windows discovery and agent service | Fixed WinRM inventory operations and Windows service wrapper | Broader WinRM inventory remains fixture-tested. The unpublished computer/interface candidate passed direct collection on Windows Server 2022/2025; managed acceptance is pending. Service registration checked by cross-compilation and code review only; real Windows Service Control Manager unverified. | [WinRM](docs/winrm-discovery.md) · [Agent](docs/topo-agent.md) |
| SNMP | SNMPv3 MIB-II device/interface inventory | Lab wire-protocol fixture uses `noAuthNoPriv`; production `authPriv` has no real-equipment validation. | [SNMP](docs/snmp.md) |
| VMware | Read-only host/VM inventory | HTTPS/authenticated `vcsim` tests; real vCenter/ESXi unverified. | [VMware](docs/vmware.md) |
| Kubernetes | Node/Pod inventory | Topo Lab API fixture; live cluster unverified. | [Kubernetes](docs/kubernetes.md) |
| AWS | Organizations account/OU structure | Simulation evidence; no live-account compatibility claim. No per-account resource inventory. | [AWS](docs/aws.md) |
| Azure | Tenant management-group/subscription structure | Fixture evidence; live-tenant acceptance absent and Reader authorization unresolved. No per-subscription resource inventory. | [Azure](docs/azure.md) |
| Inventory resolution | Source precedence, conflicts and freshness | Standalone inventory behavior; broader relationship precedence and cross-ID correlation remain planned. | [Source resolution](docs/source-resolution.md) |
| Standalone controller and collectors | SQLite, audit, schedules, backup/restore, enrollment, mTLS, rotation/revocation, heartbeats and jobs | Separate deployment with documented persistence and recovery limits; not required by the managed beta. | [Storage](docs/storage.md) · [Enrollment](docs/enrollment.md) |
| Credential references | Bounded env/file, Vault KV2 and Kubernetes Secret adapters | Provider-specific tests and deployment guidance; separate from the managed Password2 broker. | [Credential references](docs/credential-references.md) |

The [evidence index](docs/evidence/README.md) records dated real-system results.
Simulator scale gates do not prove sustained customer-estate capacity.

## Current development focus

M3 — hybrid discovery and managed ServiceNow deployment — remains the current
milestone. Combined-package installation and focused upgrade acceptance are
complete, with customer installation documentation for the Linux workflow.
The next approved scope is a **validated ServiceNow-managed Linux and Windows
inventory pilot with bounded IPv4 subnet discovery**. Development will add
managed Windows computer/interface inventory, allowlisted CIDR selection with
exclusions and bounded execution, and real mixed-estate/IRE acceptance. See the [candidate scope and gates](docs/mixed-inventory-pilot.md). These
are pending gates; the published beta retains the Linux-only scope above.
Windows target support will use Linux/macOS workers; Windows worker publication
remains a separate planned capability.

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
