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
- **Implemented milestone slices:** fixed WS-Management CIM identity, BIOS, OS, compute, network, volume, service, and patch operations; fixed machine-wide uninstall-registry software inventory over bounded WinRS; loopback Topo Lab WinRM frontend; HTTPS-only NTLMv2 authentication; fault isolation; a two-scan 500-Windows test; and concurrent two-scan acceptance for 500 Linux plus 500 Windows targets with 2,000 stable assets and no duplicates.
- **Explicitly deferred evidence:** sanitized real-host compatibility fixtures from Windows Server 2022 and one other supported release remain required before a real-host compatibility claim; per-user software inventory, Kerberos, and certificate authentication remain tracked follow-ups.
- **Implemented:** shared bounded `env:`/`file:` credential references for controller, SSH, and WinRM inputs; a Vault KV version 2 provider adapter with token lease lookup/renewal support; and a Kubernetes Secret provider adapter using the pod's own service account, with least-privilege RBAC scoping and redacted API errors. Secret values remain prohibited as CLI arguments. This completes the credential-references milestone.
- **Implemented:** outbound-only `topo agent run` — periodic local discovery delivered to the controller's existing ingestion API over the same bearer-key contract, with an AES-256-GCM-encrypted, bounded, tamper-detecting offline spool keyed by the same credential-reference contract used everywhere else; a hardened Linux systemd unit template (`packaging/systemd`, verified with `systemd-analyze verify`); and `topo agent install`/`uninstall` Windows Service Control Manager integration with automatic start, restart-on-failure, and Event Log output. This completes the Topo Agent MVP milestone. Windows service registration is verified by cross-compilation and code review only, not yet against a real Windows Service Control Manager. See [docs/topo-agent.md](docs/topo-agent.md).
- **Current milestone:** ServiceNow IRE duplicate-CI validation.
- Collector enrollment, outbound mTLS, certificate rotation, heartbeats, and jobs.
- Isolated gRPC plugin supervisor with signed manifests and resource limits.
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
