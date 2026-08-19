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
- **Implemented and verified against a real instance:** the ServiceNow IRE payload builder deduplicates by `source_native_key` within a batch and is validated to emit an identical `(source_native_key, className)` set across independently repeated Topo Lab discovery scans, the precondition for ServiceNow's own IRE to reconcile rather than duplicate a CI. That reconciliation behavior is now confirmed against a real ServiceNow developer instance for the `cmdb_ci_computer` class, and for an `IRERelation` payload connecting it to a `cmdb_ci_network_adapter`: resubmitting the same source key updates the existing CI rather than creating a duplicate, and resubmitting the same relation reports no change rather than a duplicate relationship row. Coverage of Topo's other CI classes against a real instance, and the IRE response schema, remain unverified. See [docs/servicenow.md](docs/servicenow.md).
- **Implemented (enrollment):** the controller can act as its own certificate authority (`-ca-dir`) and issue collectors short-lived client certificates through a single-use, time-bounded enrollment token (`POST /v1/enrollment-tokens`, `POST /v1/enroll`, `topo agent enroll`); the collector's private key is generated locally and never transmitted. Opt-in and additive — `topo serve` without `-ca-dir` is unchanged. See [docs/enrollment.md](docs/enrollment.md).
- **Implemented (outbound mTLS):** `topo serve -mtls` runs a native TLS listener, issuing itself a server certificate from the enrollment CA and verifying client certificates presented against it; a verified certificate authenticates a request without the bearer API key. `topo agent run -mtls-cert-dir` presents the enrolled certificate on outbound requests instead of, or alongside, the bearer key, and `topo agent enroll -controller-ca-cert` pins the controller's self-signed CA certificate so bootstrap enrollment itself can complete against an `-mtls` controller. Opt-in and additive — `-mtls` requires `-ca-dir` and is off by default. See [docs/enrollment.md](docs/enrollment.md#running-as-native-mtls).
- **Implemented (certificate rotation):** `POST /v1/rotate` renews a collector's certificate before its 90-day expiry, authenticated by the collector's current certificate over mTLS rather than a new enrollment token, with no bearer-API-key fallback and no way to request a certificate for any identity but the one already proven by the presenting certificate. `topo agent rotate` presents the current certificate, generates a fresh key pair and CSR, and overwrites `-cert-dir` in place; a running `topo agent run` must be restarted afterward to pick up the renewed certificate. See [docs/enrollment.md](docs/enrollment.md#renewing-a-certificate).
- **Implemented (heartbeats):** `POST /v1/heartbeats` is a lightweight liveness signal, distinct from observation delivery, accepted over the bearer API key or a verified mTLS certificate; `topo agent run` sends it on its own independent `-heartbeat-interval` (default one minute), separate from the discovery/delivery `-interval` so the controller need not wait 15+ minutes to notice a collector has gone quiet. `GET /v1/collectors` lists every collector's most recent heartbeat and whether it is still within the staleness window. Always available — no opt-in flag or CA required. See [docs/heartbeats.md](docs/heartbeats.md).
- **Implemented (job delivery):** `POST /v1/jobs` queues one job (today, exactly one type — `discover`, an out-of-schedule discovery pass) for a specific collector; `GET /v1/jobs` returns and marks-dispatched every job queued for the polling collector, at most once; `POST /v1/jobs/{id}/result` reports the outcome; `GET /v1/jobs/{id}` is a read-only status lookup. Since Topo Agent is deliberately outbound-only, this is collector-initiated polling, not a server push — `topo agent run` polls on the same `-heartbeat-interval` cadence it already uses for liveness heartbeats, no new flag. Identity-bound the same way as certificate rotation and heartbeats: a verified mTLS certificate's subject always wins over anything the caller claims otherwise. Always available — no opt-in flag or CA required. This completes the collector enrollment, outbound mTLS, rotation, heartbeats, and jobs milestone. See [docs/jobs.md](docs/jobs.md).
- Isolated gRPC plugin supervisor with signed manifests and resource limits.
- Persistent observation/audit storage and scheduling; evaluate PostgreSQL after the discovery and CMDB validation gates rather than treating it as the immediate next dependency.

The detailed scope, decisions, acceptance gates, and current handoff are maintained in [docs/project-plan.md](docs/project-plan.md).

## M2 — network and virtualization beta

- **In progress:** SNMPv3 and VMware vCenter discovery plugins. Slice 1
  (SNMP device identity and interfaces over SNMPv3, MIB-II `system` and
  `interfaces` groups, via `github.com/gosnmp/gosnmp`) is starting; slice 2
  (VMware vCenter inventory via `github.com/vmware/govmomi` and its
  `vcsim` simulator) has not begun. See "Current milestone: SNMP and
  VMware discovery" in [docs/project-plan.md](docs/project-plan.md).
- Rate-limited allowlisted sweep and topology plugins.
- Mapping overrides, incremental exports, retries, dead-letter queue, staleness policies, and high-availability collectors.
- DEB, RPM, MSI, Helm, offline bundle, signed artifacts, and SBOMs.

## M3 — hybrid release candidate

- AWS Organizations, Azure tenants/subscriptions, and Kubernetes discovery.
- Source precedence and conflict/freshness visibility.
- Scale and upgrade testing at 1K, 10K, and 100K assets.
- SSO/RBAC commercial modules behind documented open interfaces.

## Release gates

No production claim is made until mTLS enrollment, persistent storage, audit logs, signed releases, upgrade/restore tests, external penetration testing, and ServiceNow idempotency/duplicate-CI acceptance suites pass.
