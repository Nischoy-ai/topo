# Product roadmap

Topo discovers infrastructure through locally approved operations and publishes
normalized observations to destination systems. ServiceNow controls the current
managed Linux discovery workflow and reconciles supported CIs through IRE.

## Current releases

- **ServiceNow application 0.4.6 preview 2:** one native XML batch containing
  the application and 32 required indexes. See [installation](docs/servicenow-update-set.md)
  and [package validation](docs/servicenow-validation.md).
- **Worker v0.1.0-beta.1:** signed Linux APT/RPM packages and a macOS Homebrew
  formula, available on amd64 and arm64. See [distribution](docs/distribution.md).
- Windows package publication and stable channels are planned. The current
  macOS CLI beta uses Homebrew and is not Apple Developer ID signed or notarized.

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

Protocol guides describe the tested environments and compatibility boundaries.
The ServiceNow package's fresh and repeat installation results are recorded
separately from older application-only upgrade evidence. Simulated scale gates
cover 1K, 10K and 100K assets; those timings do not measure live ServiceNow capacity.

## Current development focus

M3 — hybrid discovery and managed ServiceNow deployment — remains the current
milestone. The active work completes combined-package upgrade verification and
customer installation documentation for the Linux workflow.

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
