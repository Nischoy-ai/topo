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
- **Implemented (enrollment):** the controller can act as its own certificate authority (`-ca-dir`) and issue collectors short-lived client certificates through a single-use, time-bounded, collector-ID-scoped enrollment token (`POST /v1/enrollment-tokens`, `POST /v1/enroll`, `topo agent enroll`); the collector's private key is generated locally and never transmitted. Opt-in and additive — `topo serve` without `-ca-dir` is unchanged. See [docs/enrollment.md](docs/enrollment.md).
- **Implemented (outbound mTLS):** `topo serve -mtls` runs a native TLS listener, issuing itself a server certificate from the enrollment CA and verifying client certificates presented against it; a verified certificate authenticates a request without the bearer API key. `topo agent run -mtls-cert-dir` presents the enrolled certificate on outbound requests instead of, or alongside, the bearer key, and `topo agent enroll -controller-ca-cert` pins the controller's self-signed CA certificate so bootstrap enrollment itself can complete against an `-mtls` controller. Opt-in and additive — `-mtls` requires `-ca-dir` and is off by default. See [docs/enrollment.md](docs/enrollment.md#running-as-native-mtls).
- **Implemented (certificate rotation):** `POST /v1/rotate` renews a collector's certificate before its 90-day expiry, authenticated by the collector's current certificate over mTLS rather than a new enrollment token, with no bearer-API-key fallback and no way to request a certificate for any identity but the one already proven by the presenting certificate. `topo agent rotate` presents the current certificate, generates a fresh key pair and CSR, and overwrites `-cert-dir` in place; a running `topo agent run` must be restarted afterward to pick up the renewed certificate. See [docs/enrollment.md](docs/enrollment.md#renewing-a-certificate).
- **Implemented (heartbeats):** `POST /v1/heartbeats` is a lightweight liveness signal, distinct from observation delivery, accepted over the bearer API key or a verified mTLS certificate; `topo agent run` sends it on its own independent `-heartbeat-interval` (default one minute), separate from the discovery/delivery `-interval` so the controller need not wait 15+ minutes to notice a collector has gone quiet. `GET /v1/collectors` lists every collector's most recent heartbeat and whether it is still within the staleness window. Always available — no opt-in flag or CA required. See [docs/heartbeats.md](docs/heartbeats.md).
- **Implemented (job delivery):** `POST /v1/jobs` queues one job (today, exactly one type — `discover`, an out-of-schedule discovery pass) for a specific collector; `GET /v1/jobs` returns and marks-dispatched every job queued for the polling collector, at most once; `POST /v1/jobs/{id}/result` reports the outcome; `GET /v1/jobs/{id}` is a read-only status lookup. Since Topo Agent is deliberately outbound-only, this is collector-initiated polling, not a server push — `topo agent run` polls on the same `-heartbeat-interval` cadence it already uses for liveness heartbeats, no new flag. Identity-bound the same way as certificate rotation and heartbeats: a verified mTLS certificate's subject always wins over anything the caller claims otherwise. Always available — no opt-in flag or CA required. This completes the collector enrollment, outbound mTLS, rotation, heartbeats, and jobs milestone. See [docs/jobs.md](docs/jobs.md).
- Isolated gRPC plugin supervisor with signed manifests and resource limits.
- **Complete:** Persistent observation/audit storage and scheduling. Slice 1 (persistent storage): `topo serve -db-driver sqlite -db-dsn <path>` opts into a SQLite-backed `store.Repository` (`internal/store/sqlite`, `modernc.org/sqlite`, pure-Go/no cgo) that survives a controller restart; `-db-driver memory` (default) keeps prior in-memory-only behavior unchanged. Relationships are queryable through `store.Repository` (`ListRelationships`, `GET /v1/relationships`), and `SaveObservation` is idempotent by observation ID in both backends. PostgreSQL evaluated and deliberately deferred — no HA/clustered-controller story exists yet to justify a client-server database over a single embedded file. Slice 2 (immutable audit log): a hash-chained log (`internal/audit`) of admin/security-relevant controller actions — enrollment token issuance, collector enrollment, certificate rotation, job creation, and schedule changes — persisted the same way observation data is, via new `store.Repository` methods both backends implement; `GET /v1/audit` returns the chain, and `internal/audit.VerifyChain` detects any entry edited, reordered, or removed after the fact. Audit detail fields are always short strings, never secret material. Slice 3 (server-side recurring discovery scheduling): `POST /v1/schedules` (upsert, one per collector), `GET /v1/schedules`, `DELETE /v1/schedules/{collector_id}` let an operator set a recurring `discover` cadence for a collector; a due schedule turns into an actual job lazily, the next time that collector polls `GET /v1/jobs` — no background ticker, since Topo Agent is deliberately outbound-only — reusing the existing job-delivery machinery rather than a second dispatch path. Unlike enrollment tokens, heartbeats, and one-off job state (still in-memory only), a schedule is itself persisted under `-db-driver sqlite`, since silently losing a standing recurring-discovery policy on restart would be a real operational surprise. A shared conformance test suite proves both backends behave identically across discovery data, the audit log, and schedules. See [docs/storage.md](docs/storage.md) and [docs/scheduling.md](docs/scheduling.md).

The detailed scope, decisions, acceptance gates, and current handoff are maintained in [docs/project-plan.md](docs/project-plan.md).

## M2 — network and virtualization beta

- **Implemented alpha (SNMP):** SNMPv3 network device identity and
  interface discovery — MIB-II `system` (`sysDescr`, `sysObjectID`,
  `sysUpTime`, `sysName`) and `interfaces` (`ifDescr`, `ifPhysAddress` via
  GETBULK) groups only, via `github.com/gosnmp/gosnmp`. Production
  requires `authPriv` with SHA authentication and AES privacy; there is no
  weaker fallback outside Topo Lab's `-lab` mode. Asset identity is the
  SNMPv3 engine ID discovered during the USM handshake, not an IP address.
  Topo Lab's hand-rolled `noAuthNoPriv` SNMP agent (one loopback UDP
  socket per simulated device) proves the plugin's own message framing
  and parsing/mapping logic through a real two-scan idempotency
  acceptance test; `authPriv` is implemented but not yet verified against
  a real device. See [docs/snmp.md](docs/snmp.md).
- **Implemented alpha (VMware):** VMware vCenter (or standalone ESXi)
  virtual machine and host inventory via `github.com/vmware/govmomi`,
  using read-only vSphere API enumeration only. Asset identity is a
  host's hardware UUID or a VM's VC-managed instance UUID (falling back
  to its BIOS UUID for standalone ESXi hosts with no vCenter to assign
  one), never an IP address or inventory path; `vm_runs_on_host`,
  `host_has_interface`, and `vm_has_interface` relationships connect
  hosts, VMs, and their interfaces. Production requires HTTPS with
  normal certificate verification; there is no fallback outside Topo
  Lab's `-lab` mode. Unlike SNMP, govmomi ships its own `vcsim`
  simulator built for exactly this kind of testing, so the two-scan
  idempotency and fault-isolation acceptance tests run directly against
  `govmomi/simulator` rather than a hand-rolled Topo Lab fixture; real
  vCenter/ESXi verification beyond `vcsim` has not been performed. This
  completes both slices of the SNMP/VMware discovery milestone. See
  [docs/vmware.md](docs/vmware.md).
- Rate-limited allowlisted sweep and topology plugins.
- Mapping overrides, incremental exports, retries, dead-letter queue, staleness policies, and high-availability collectors.

## M2.5 — release readiness and security hardening

- **Implemented (authorization boundary):** operator reads and control-plane mutations require the configured bearer API key; verified collector certificates are limited to observation delivery, heartbeats, job polling/results, and certificate rotation. mTLS observations are bound to the peer certificate identity. The bearer key remains accepted on collector endpoints for backward compatibility, and leaving it unset preserves evaluation mode.
- **Implemented (certificate revocation and compromise recovery):** operator-only `POST /v1/certificate-revocations` immutably revokes one canonical certificate serial; `GET /v1/certificate-revocations` lists the records. SQLite schema version 4 persists revocations across restarts, application authorization fails closed on lookup errors, revoked certificates cannot use collector endpoints or rotate, and fresh-token re-enrollment recovers the same collector identity with a new key and serial. Rotation surfaces old/new serials and is linearized against revocation in the supported single-controller process. See [docs/enrollment.md](docs/enrollment.md#revoking-and-recovering-a-certificate).
- **Implemented:** backup/restore and upgrade tooling with verified, non-overwriting
  recovery; tested migration from every supported schema; and transaction-wide
  rollback on migration failure. See [docs/storage.md](docs/storage.md#backup-and-restore).
- **Implemented:** byte-reproducible Linux/macOS/Windows amd64/arm64 archives,
  SHA-256 manifests, SPDX SBOMs, keyless Sigstore checksum signatures, and
  signed GitHub build/SBOM attestations. See [docs/releases.md](docs/releases.md).
- **Implemented:** DEB, RPM, MSI, Helm, raw archive, and offline-bundle packaging from the
  same immutable release artifacts. See [docs/packages.md](docs/packages.md).
- **Implemented automation; operational evidence deferred:** package-manager distribution through signed Nischoy APT and RPM repositories, an
  organization Homebrew tap, WinGet catalog manifests, and an OCI Helm registry,
  with stable/beta promotion and clean-machine install/upgrade/uninstall tests.
  Deterministic generation and protected fail-closed promotion automation are
  implemented; real beta and N-1 stable promotion evidence remains required.
  See [docs/distribution.md](docs/distribution.md).
- Additional package ecosystems such as Chocolatey, Scoop, AUR, and Snap follow
  demonstrated user demand rather than blocking the initial release channels.
- **Complete:** external security review preparation and remediation. The
  reviewer scope/threat model and closure protocol are documented; the
  security baseline uses exact Go 1.25.13 with pinned `govulncheck`, and
  pre-review reachable toolchain/dependency findings plus plaintext external
  secret-provider transport were remediated. Every finding raised so far —
  maintainer-audit `TSR-2026-001` (enrollment tokens not bound to their
  intended collector ID), `TSR-2026-002`/`TSR-2026-009` (live SQLite file and
  backup-staging permissions), `TSR-2026-003` (raw `${{ }}` workflow-input
  interpolation in `promote.yml`), and the first independent-reviewer finding
  `TSR-2026-004` (Grok/xAI; publisher HTTPS clients followed redirects and
  accepted URL userinfo) — is fixed and merged. This closes M2.5's
  implementation and remediation scope; two items remain open as
  tracked follow-up rather than blocking further roadmap work: independent
  retest of the fixed findings, and real beta/N-1 package-channel promotion
  evidence (deferred until external repository and production signing-key
  provisioning is authorized). Neither is represented as complete, and
  neither is a production-readiness claim — see
  [docs/security-review.md](docs/security-review.md) and "Release gates"
  below.

## M3 — hybrid release candidate

- **Implemented alpha (Kubernetes):** cluster node and pod inventory —
  Kubernetes UID-based identity (never node/pod name or an IP address),
  `pod_runs_on_node` relationships, via `k8s.io/client-go`. Production
  requires HTTPS with normal certificate verification; there is no
  fallback outside Topo Lab's `-lab` mode. `client-go` has no local
  simulator equivalent to VMware's `vcsim`, so Topo Lab hand-rolls a
  Kubernetes API fixture (real `k8s.io/api` JSON over real HTTP,
  `pkg/lab/kubernetes_server.go`) the same way it did for SNMP; the
  two-scan idempotency acceptance test runs the full 500-host scenario
  (500 nodes, 500 pods, 500 relationships). Real-cluster verification
  beyond the hand-rolled fixture has not been performed. Node/Pod only —
  no Deployment, Service, or other workload object kinds yet. See
  [docs/kubernetes.md](docs/kubernetes.md).
- **Implemented alpha (AWS Organizations):** organization account structure —
  root/OU/account hierarchy via `member_of` relationships, AWS-assigned-ID
  identity (never account name), via `aws-sdk-go-v2`'s Organizations
  client. Production requires HTTPS; there is no fallback outside Topo
  Lab's `-lab` mode. The Organizations API has no official local
  simulator, so Topo Lab hand-rolls an AWS-JSON-1.1 fixture
  (`pkg/lab/aws_organizations_server.go`) with real SigV4 signature
  verification via the SDK's own signer, the same way it did for
  Kubernetes and SNMP; the two-scan idempotency acceptance test runs the
  full 500-host scenario as 500 simulated accounts (506 assets, 505
  relationships). Real-organization verification beyond the hand-rolled
  fixture has not been performed. Organization structure only — no
  per-account resource inventory (EC2, S3, IAM, etc.) yet. See
  [docs/aws.md](docs/aws.md).
- Azure tenants/subscriptions discovery, Kubernetes workload object kinds
  (Deployment, Service, ConfigMap, PersistentVolumeClaim, etc.) beyond
  Node/Pod, and AWS per-account resource inventory, remain unstaged.
- Source precedence and conflict/freshness visibility.
- Scale and upgrade testing at 1K, 10K, and 100K assets.
- SSO/RBAC commercial modules behind documented open interfaces.

## Release gates

No production claim is made until mTLS enrollment, persistent storage, audit logs, signed releases, upgrade/restore tests, external penetration testing, and ServiceNow idempotency/duplicate-CI acceptance suites pass.
