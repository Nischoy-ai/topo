# Roadmap

## M0 — working vertical slice (implemented here)

- Canonical observation, asset, evidence, error, and relationship contracts.
- Local non-privileged host/interface plugin and stable identity resolution.
- Authenticated ingestion/read API with bounded payloads and security headers.
- JSON Lines, HTTPS webhook, and ServiceNow IRE preview/publish adapters.
- Hardened container baseline, CI, tests, and extension documentation.
- Topo Lab deterministic estate generator, mixed Linux/Windows personas, fault injection, expected graphs, and a 500-host idempotency test.

## M1 — host discovery alpha

- **Implemented alpha:** Linux SSH discovery with password/private-key authentication, mandatory host-key policy, fixed command allowlist, bounded output, concurrency and deadlines.
- **Implemented alpha:** Topo Lab SSH frontend and a two-scan, 500-host acceptance test using real SSH handshakes and sessions entirely in memory.
- **Current milestone:** WinRM/Windows discovery and a Topo Lab WinRM frontend, culminating in a repeatable 500-Linux/500-Windows mixed-estate acceptance test.
- Credential references, Vault/Kubernetes Secret adapters, and ServiceNow IRE duplicate-CI validation.
- Collector enrollment, outbound mTLS, certificate rotation, heartbeats, and jobs.
- Isolated gRPC plugin supervisor with signed manifests and resource limits.
- Linux systemd and Windows service agents with encrypted offline buffering.
- Persistent observation/audit storage and scheduling; evaluate PostgreSQL after the discovery and CMDB validation gates rather than treating it as the immediate next dependency.

The detailed scope, decisions, acceptance gates, and current handoff are maintained in [docs/project-plan.md](docs/project-plan.md).

## M2 — network and virtualization beta

- Rate-limited allowlisted sweep, SNMPv3, topology, and VMware vCenter plugins.
- Mapping overrides, incremental exports, retries, dead-letter queue, staleness policies, and high-availability collectors.
- DEB, RPM, MSI, Helm, offline bundle, signed artifacts, and SBOMs.

## M3 — hybrid release candidate

- AWS Organizations, Azure tenants/subscriptions, and Kubernetes discovery.
- Source precedence and conflict/freshness visibility.
- Scale and upgrade testing at 1K, 10K, and 100K assets.
- SSO/RBAC commercial modules behind documented open interfaces.

## Release gates

No production claim is made until mTLS enrollment, persistent storage, audit logs, signed releases, upgrade/restore tests, external penetration testing, and ServiceNow idempotency/duplicate-CI acceptance suites pass.
