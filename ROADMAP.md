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

## Available capabilities

| Area | Implemented scope | Guide |
| --- | --- | --- |
| ServiceNow-managed discovery | Outbound stateless workers, approved IPv4 Linux SSH targets, Password2 credential broker, leases, manual runs and schedules | [Linux setup](docs/pilot-quickstart.md) |
| CMDB publication | IRE preflight and publication for computers, network adapters, and ownership relationships; stable source identity and repeat reconciliation | [ServiceNow IRE](docs/servicenow.md) |
| Host discovery | Local discovery, reviewed SSH operations, and fixed WinRM inventory operations | [SSH](docs/ssh-discovery.md), [WinRM](docs/winrm-discovery.md) |
| Network and virtualization | SNMPv3 MIB-II device/interface inventory and read-only VMware host/VM inventory | [SNMP](docs/snmp.md), [VMware](docs/vmware.md) |
| Cloud structure | Kubernetes Node/Pod inventory, AWS Organizations account/OU structure, Azure tenant subscription structure | [Kubernetes](docs/kubernetes.md), [AWS](docs/aws.md), [Azure](docs/azure.md) |
| Inventory resolution | Source precedence, conflict reporting, and freshness visibility | [Source resolution](docs/source-resolution.md) |
| Collector lifecycle | Enrollment, outbound mTLS, certificate rotation/revocation, heartbeats, and polled jobs | [Enrollment](docs/enrollment.md), [Jobs](docs/jobs.md) |
| Controller persistence | Single-process SQLite inventory, schedules, tamper-evident audit, verified backup/restore and forward migrations | [Storage](docs/storage.md) |
| Credentials | Bounded env/file, Vault KV2, and Kubernetes Secret references | [Credential references](docs/credential-references.md) |
| Distribution | Reproducible worker archives, signed checksums, SBOM/provenance, signed Linux repositories, and tested public-channel installs | [Releases](docs/releases.md) |

Protocol compatibility and package validation are recorded separately in the
[documentation evidence index](docs/evidence/README.md).

## Current development focus

M3 — hybrid discovery and managed ServiceNow deployment — remains the current
milestone. Combined-package installation and focused upgrade acceptance are
complete, with customer installation documentation for the Linux workflow.
Further capability work follows the planned scopes below.

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
