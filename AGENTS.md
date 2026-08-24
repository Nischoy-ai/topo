# Nischoy Topo agent instructions

These instructions are the durable starting point for coding agents. Do not
assume access to an earlier chat.

## Start every task

1. Read `docs/project-plan.md`, `ROADMAP.md`, `README.md`, and `SECURITY.md`.
2. Inspect `git status`, the current branch, and recent history. Preserve user
   changes and synchronize with `origin/main` before creating a new branch when
   network access permits.
3. Confirm the current milestone in `docs/project-plan.md`; do not silently
   reorder the roadmap.
4. For implementation work, make the smallest complete vertical slice, run the
   relevant tests, and update the handoff section in `docs/project-plan.md`.

## Product boundaries

- Topo is a public, standalone product under Nischoy. It must not depend on the
  private Nischoy website or private repositories.
- Keep discovery destination-neutral. ServiceNow is an important publisher,
  not the internal data model or discovery engine.
- Use ServiceNow IRE APIs and stable source identity; do not write CMDB tables
  directly or make IP addresses long-lived device identities.
- Remote discovery executes only compiled-in, reviewed operations. A job or
  controller must never supply arbitrary SSH commands, PowerShell, or scripts.
- Require bounded reads, deadlines, cancellation, controlled concurrency,
  host/server identity verification, structured errors, and least-privilege
  credentials.
- Never accept secrets as ordinary CLI values, job options, labels, logs, or
  observation attributes.
- Simulation is the scale-test strategy. Retain a small sanitized real-system
  compatibility matrix for protocol and OS behavior.
- PostgreSQL is intentionally deferred until mixed Linux/Windows discovery,
  credential references, and the first end-to-end ServiceNow validation are
  complete, unless the user explicitly changes that priority.

## Current priority

M2.5 — release readiness and security hardening — is complete: all seven
slices below are done. Two items remain open as tracked follow-up rather
than blocking M3 work: independent retest of the merged findings
(`TSR-2026-001`/`002`/`003`/`004`/`009`), and real beta/N-1 package-channel
promotion evidence (deferred until the user authorizes external repository
and production signing-key provisioning). Neither is a production-readiness
claim; see `docs/security-review.md`.

M3 — hybrid release candidate — is current: AWS Organizations, Azure
tenants/subscriptions, and Kubernetes discovery; source precedence and
conflict/freshness visibility; scale/upgrade testing at 1K/10K/100K assets;
and SSO/RBAC commercial modules behind documented open interfaces, per
`ROADMAP.md`. Slice 1, Kubernetes node/pod discovery, is implemented — see
`docs/kubernetes.md` and "Current milestone: M3" in `docs/project-plan.md`.
AWS Organizations and Azure tenants/subscriptions discovery, and Kubernetes
workload object kinds beyond Node/Pod, remain unstaged — stage each the
same way (Objective, Deliverables, Acceptance gates, Deliberate non-goals)
before starting it, and confirm scope with the user first, rather than
assuming an order.

The completed M2.5 slices, kept for reference:

1. **Done.** Separate the operator control plane from the collector
   data plane. With an API key configured, inventory/audit/status reads and
   token/job/schedule mutations require the bearer key; verified collector
   certificates may deliver observations, send heartbeats, poll/report jobs,
   and rotate themselves. Bind mTLS observation identity to the peer
   certificate. Preserve bearer-key compatibility on collector endpoints and
   the open no-key evaluation mode, and document that the bearer key still
   carries operator authority.
2. **Done.** Certificate revocation plus explicit compromise recovery
   and safer rotation operations. Persist immutable serial-specific records in
   SQLite, fail closed during authorization, block rotation by a revoked
   certificate, surface old/new serials, and recover through fresh-token
   re-enrollment of the same collector identity. Keep the memory backend
   evaluation-only and define rotation-race ordering for the supported
   single-controller process.
3. **Done.** Tested backup/restore and schema upgrade/rollback
   procedures. Backups and restores must be verified and non-overwriting;
   every supported SQLite schema must retain real data through restore and
   forward migration; all pending migrations must roll back together on a
   failure. Rollback means restoring the pre-upgrade backup to a new path,
   never reverse-migrating or overwriting the failed database in place.
4. **Done.** Reproducible signed artifacts, SBOMs, checksums, and
   build provenance. Build Linux/macOS/Windows amd64/arm64 archives twice from
   different paths with exact Go 1.25.13, reject any byte drift, sign the
   checksum manifest keylessly, generate signed GitHub provenance and SBOM
   attestations, pin release actions by commit, verify evidence before a tag
   can create a GitHub Release, and document independent consumer verification.
5. **Done.** DEB, RPM, MSI, Helm, raw archives, and offline-bundle
   packaging from the same verified release artifacts.
6. **Done — automation; operational evidence deferred.** Publish those
   artifacts through a signed Nischoy APT repository,
   a signed Nischoy RPM repository, `Nischoy-ai/homebrew-tap`, Microsoft's
   WinGet catalog, and an OCI Helm registry. Keep stable/beta promotion,
   repository-native signing/key rotation, and clean-machine install/upgrade/
   uninstall tests in scope; additional package managers follow demand. The
   first real beta and N-1 stable promotions remain required, explicitly
   deferred until the user authorizes external repository and production-key
   provisioning.
7. **Done.** Prepare and commission an external security review and
   remediate every finding raised so far. Preparation must include a
   reviewer scope/threat model, a
   supported vulnerability-free build baseline, and explicit findings/closure
   rules; preparation is not itself an independent review. Focused maintainer-
   audit remediations bind enrollment tokens to the operator-selected collector
   ID (`TSR-2026-001`), protect the live SQLite database plus backup creation
   window (`TSR-2026-002`/`TSR-2026-009`), and route a `workflow_dispatch`
   version input through `env:` instead of raw `${{ }}` shell/PowerShell
   interpolation in `promote.yml`, with a new CI check against recurrence
   (`TSR-2026-003`, low severity — a same-run validation step already
   constrained the value at discovery, so it was not independently
   exploitable, but the constraint lives in a different step than the use).
   An independent reviewer (Grok/xAI) has now completed a first review pass
   against an immutable commit and reported one medium finding,
   `TSR-2026-004` — publisher HTTPS clients (webhook, ServiceNow) accepted
   URL userinfo and followed redirects, potentially replaying a bearer token
   against an unconfigured destination; the related agent-sender/
   enrollment-client residual is fixed in the same change. See "Independent
   review" in `docs/security-review.md`. All five findings are merged and
   ready for independent retest; no finding is marked `Verified` by a
   maintainer or coding-agent assertion — only the reviewer's retest of the
   exact remediation commit can do that, so track that retest (and any
   further findings the reviewer raises) as ongoing follow-up alongside M3,
   not as a blocker to starting it.

The credential-references milestone is complete:

1. **Done.** A shared, bounded credential-reference contract with `env:` and
   `file:` providers for early evaluation.
2. **Done.** Adoption by the controller API key, SSH password/private key,
   and WinRM password CLI paths without accepting secret values as CLI
   arguments.
3. **Done.** A `vault:<path>#<field>` provider adapter (KV version 2) and a
   `k8s:[<namespace>/]<secret-name>#<field>` provider adapter using the
   pod's own service account, both with provider-specific tests and
   least-privilege deployment guidance.
4. **Done.** Security tests that prove secret values do not enter errors or
   logs, across all four providers.

The outbound-only Topo Agent MVP milestone is complete:

1. **Done.** Agent core loop (`topo agent run`): periodic local discovery
   delivered to the controller's existing ingestion API over the existing
   bearer-key contract, with an AES-256-GCM-encrypted, bounded,
   tamper-detecting offline spool keyed by the same credential-reference
   contract as everything else. See `docs/topo-agent.md`.
2. **Done.** Linux systemd unit (`packaging/systemd`, verified with
   `systemd-analyze verify`) and Windows service wrapping
   (`topo agent install`/`uninstall`, `cmd/topo/service_windows.go`) so
   `topo agent run` survives reboots and restarts on failure, plus
   install/uninstall documentation in `docs/topo-agent.md`. Windows service
   registration is verified by cross-compilation and code review, not yet
   against a real Windows Service Control Manager; treat it as unverified
   on real Windows, matching the WinRM real-host fixture posture below.

The ServiceNow IRE duplicate-CI validation milestone is complete for both
halves now: `mapPayload` deduplicates by `source_native_key` (and
relationships by `(type, from, to)`) within a batch, and is validated to
emit an identical `(source_native_key, className)` set across
independently repeated Topo Lab discovery scans — the precondition for
ServiceNow's own IRE to reconcile rather than duplicate a CI. That
precondition was verified against a real ServiceNow developer instance on
2026-08-19 for the `cmdb_ci_computer` class, and for an `IRERelation`
between it and `cmdb_ci_network_adapter`: submitting the same
`sys_object_source_info` twice returns `operation: UPDATE` against the
original `sysId`, not a new one, and resubmitting the same relation
returns `operation: NO_CHANGE`, not a duplicate `cmdb_rel_ci` row. See
`docs/servicenow.md` for the full evidence and scope. Still unverified:
ServiceNow's own behavior for Topo's other CI classes, larger multi-item
batches, and the IRE response schema — do not represent those as
validated. A ServiceNow developer instance is available for further
validation when needed; do not fabricate
findings for the classes/paths not yet exercised — get real evidence the
same way this one was obtained.

The collector enrollment, outbound mTLS, certificate rotation, heartbeats,
and job delivery milestone is complete — five distinct capabilities,
staged as separate slices:

1. **Done.** Collector enrollment: the controller is its own certificate
   authority (`internal/enrollment`, `-ca-dir`), issuing short-lived
   client certificates through a single-use, time-bounded token
   (`POST /v1/enrollment-tokens`, `POST /v1/enroll`, `topo agent enroll`).
   The collector's private key is generated locally and never transmitted.
   Opt-in and additive: `topo serve` without `-ca-dir` is unchanged. See
   `docs/enrollment.md`.
2. **Done.** Outbound mTLS: `topo serve -mtls` runs a native TLS listener
   issuing itself a server certificate from the enrollment CA and verifying
   client certificates against it (`tls.VerifyClientCertIfGiven`, not
   `RequireAndVerifyClientCert` — the TLS layer must still accept a
   connection with no client certificate at all, since a collector's first
   request, `POST /v1/enroll`, has none to present yet; per-endpoint
   enforcement happens in application middleware instead). `topo agent run
   -mtls-cert-dir` presents the enrolled certificate on outbound requests
   instead of, or alongside, the bearer API key. `topo agent enroll
   -controller-ca-cert` pins the controller's self-signed CA certificate so
   the bootstrap enrollment request itself can complete against an `-mtls`
   controller. See `docs/enrollment.md`.
3. **Done.** Certificate rotation: `POST /v1/rotate` renews a collector's
   certificate before it expires, authenticated by the collector's current
   certificate over mTLS rather than a new enrollment token — deliberately
   no bearer-API-key fallback (accepting one would let any holder mint a
   certificate for any collector ID), and the reissued certificate's
   identity always comes from the already-verified peer certificate, never
   from the CSR. `topo agent rotate` presents the current certificate,
   generates a fresh key pair and CSR, and overwrites `-cert-dir`. Manual,
   not automatic: a running `agent run` loads its certificate once at
   startup and must be restarted after rotation to pick it up. See
   `docs/enrollment.md`.
4. **Done.** Heartbeats: `POST /v1/heartbeats` is a lightweight liveness
   signal, distinct from observation delivery, so the controller need not
   wait on the (often 15+ minute) discovery `-interval` to notice a
   collector has gone quiet. `topo agent run -heartbeat-interval` (default
   one minute, `0` disables it) runs on its own independent ticker inside
   `agent.Run`, entirely decoupled from `-interval`. Unlike
   `POST /v1/rotate`, it accepts the bearer API key as well as a verified
   mTLS certificate — a heartbeat only asserts liveness, not an identity
   that gets certificate material issued to it — but a verified peer
   certificate's subject still overrides whatever `collector_id` the
   request body claims, matching rotation's identity rule. Always
   available, no CA or opt-in flag required. `GET /v1/collectors` lists
   each collector's most recent heartbeat and liveness. See
   `docs/heartbeats.md`.
5. **Done.** Job delivery: necessarily collector-initiated polling since
   the agent remains outbound-only, not a server push. `POST /v1/jobs`
   queues one job (today, exactly one type — `discover`) for a specific
   collector; `GET /v1/jobs` returns and marks-dispatched every job
   queued for the polling collector, at most once (no redelivery);
   `POST /v1/jobs/{id}/result` reports the outcome; `GET /v1/jobs/{id}`
   is a read-only status lookup. Identity-bound the same way as rotation
   and heartbeats, via a `collectorIdentity` helper now shared with the
   heartbeat handler too. `topo agent run` polls on the same
   `-heartbeat-interval` cadence it already uses for heartbeats — no new
   flag. A `discover` job reuses the existing `discoverAndSend` helper
   directly. Always available, no CA or opt-in flag required. See
   `docs/jobs.md`.

The complete scope and acceptance gates for every slice are in
`docs/project-plan.md`.

The SNMP and VMware discovery milestone (M2) is complete, both slices:

1. **Done.** SNMP device identity and interfaces: `pkg/discovery/snmp`
   implements the existing `discovery.Plugin` interface, querying MIB-II
   (`system`: `sysDescr`/`sysObjectID`/`sysUpTime`/`sysName`, and
   `interfaces`: `ifDescr`/`ifPhysAddress` via GETBULK) over SNMPv3 using
   `github.com/gosnmp/gosnmp`, pinned to `v1.42.1` — selected under the
   project's earlier Go 1.23 baseline and retained under the current Go 1.25
   security baseline. Production requires `authPriv` with SHA
   authentication and AES privacy, with no weaker fallback, mirroring how
   WinRM's production path requires NTLM+HTTPS. Asset identity is the
   SNMPv3 engine ID discovered during the USM handshake, not an IP
   address. Topo Lab's hand-rolled SNMP agent (`pkg/lab/snmp_server.go`,
   one loopback UDP socket per simulated device, `noAuthNoPriv` only,
   built on gosnmp's own exported packet decode/encode so the wire format
   exercised is real) backs a two-scan idempotency acceptance test
   exercising the plugin's actual SNMPv3 message framing. `authPriv` is
   implemented via gosnmp's own client-side USM crypto but remains
   unverified against real network equipment, matching the WinRM
   real-host fixture posture below. See `docs/snmp.md`.
2. **Done.** VMware vCenter virtual machine and host inventory:
   `pkg/discovery/vmware` implements `discovery.Plugin`, enumerating
   `HostSystem`/`VirtualMachine` objects read-only over the vSphere API
   (a bounded property-collector container view; no configuration, power,
   or lifecycle operation is ever issued) using `github.com/vmware/govmomi`,
   pinned to `v0.52.0` — selected under the earlier Go 1.23 baseline and
   retained under the current Go 1.25 security baseline. Asset identity is a
   host's hardware UUID or a VM's
   VC-managed instance UUID (falling back to its BIOS UUID for standalone
   ESXi hosts with no vCenter to assign one), never an IP address or
   inventory path; `vm_runs_on_host`, `host_has_interface`, and
   `vm_has_interface` relationships connect hosts, VMs, and their
   interfaces. Production requires HTTPS with normal certificate
   verification, with no fallback outside Topo Lab's `-lab` mode. Unlike
   SNMP, govmomi ships its own `vcsim` simulator built for exactly this
   kind of testing, so the two-scan idempotency and fault-isolation
   acceptance tests (`pkg/discovery/vmware/integration_test.go`) run
   directly against `govmomi/simulator` — with TLS and real credential
   enforcement deliberately turned on, since vcsim defaults to plaintext
   HTTP and accepts any non-empty credentials unless explicitly
   configured otherwise — rather than a hand-rolled Topo Lab fixture.
   Real vCenter/ESXi verification beyond `vcsim` has not been performed,
   matching the SNMP `authPriv` and WinRM real-host fixture posture. See
   `docs/vmware.md`.

The complete scope and acceptance gates are in `docs/project-plan.md`.

The persistent observation/audit storage and scheduling milestone is now
complete (all three slices). Slice 1 (persistent storage): `internal/store/sqlite`
implements the existing `store.Repository` interface using
`modernc.org/sqlite` (pure-Go, no cgo — required for this project's
`GOOS=windows` cross-compile CI check to keep working) pinned to `v1.39.0`,
selected under the earlier Go 1.23 baseline and retained under the current Go
1.25 security baseline. PostgreSQL is
deliberately not used: this project has no HA/clustered-controller story
yet, so a client-server database is not yet justified over a single
embedded file — see "Storage technology decision" under "Current
milestone: persistent observation/audit storage and scheduling" in
`docs/project-plan.md` for the full reasoning, which also covers slice 1's
other real gap-fix: relationships are now queryable through
`store.Repository` (`ListRelationships`, and `GET /v1/relationships`),
since previously `Memory` received them in every observation but never
exposed them. `topo serve -db-driver sqlite -db-dsn <path>` opts in;
`-db-driver memory` (the default) is unchanged — nothing survives a
restart, same as before this slice.

Slice 2 (immutable audit log): a new `internal/audit` package
implements a hash chain (each entry's hash covers its own content and the
previous entry's hash — tamper-evident, not physically write-once, and not
cryptographic non-repudiation), `store.Repository` gained
`AppendAuditEvent`/`ListAuditEntries` (implemented by both `Memory` and a
new SQLite `audit_entries` table, schema version 2 — `migrate` now applies
every pending versioned migration in order, so an existing version-1
database upgrades in place). The controller records an entry for
enrollment token issuance, collector enrollment, certificate rotation, and
job creation, best-effort with respect to the action itself (an
audit-write failure is logged, not treated as grounds to fail or undo an
action that already completed). Detail fields are always short strings,
never secret material — an enrollment token is referenced only by a
truncated SHA-256 fingerprint. New `GET /v1/audit` endpoint. See
`docs/storage.md`.

Slice 3 (server-side recurring discovery scheduling): `store.Repository`
gained a `Schedule` type and `UpsertSchedule`/`ListSchedules`/
`GetSchedule`/`DeleteSchedule` (implemented by both `Memory` and a new
SQLite `schedules` table, schema version 3). New `POST /v1/schedules`
(upsert, at most one schedule per collector), `GET /v1/schedules`, and
`DELETE /v1/schedules/{collector_id}` endpoints. Deliberately no
background ticker: a schedule only turns into a `model.Job` lazily, the
moment its collector next polls `GET /v1/jobs` and the schedule is due —
reusing `POST /v1/jobs`'s existing collector-initiated-polling machinery
rather than a second dispatch path, since Topo Agent is deliberately
outbound-only and a ticker would have nothing to push to anyway.
`JobStore.HasOutstanding` prevents a schedule from piling up a second job
while an earlier one is still outstanding. Unlike enrollment tokens,
heartbeats, and one-off job state — all still in-memory only — a schedule
*is* persisted under `-db-driver sqlite`: it is a standing operator
policy, not short-lived/self-healing like a heartbeat or a single job, so
silently losing it on restart would be a real operational surprise.
Schedule changes are audited
(`schedule_created`/`schedule_updated`/`schedule_deleted`). See
`docs/scheduling.md`.

Enrollment tokens, heartbeats, and one-off job state (not the audit record
that an action involving them happened, which does persist under
`-db-driver sqlite`) remain in-memory only; whether they need to become
durable is a question for a later milestone, not assumed now.

The Windows implementation and simulated scale gates are complete. Sanitized
fixtures from Windows Server 2022 and one other supported release are
explicitly deferred, not represented as completed, and remain required before
claiming real-host compatibility or production readiness.

The complete scope, acceptance gates, and follow-on order are in
`docs/project-plan.md`.

## Engineering workflow

- Use Go 1.25 compatibility and exact Go 1.25.13 for release/security evidence
  until the roadmap explicitly changes it. The M2.5 external-review preparation
  slice raised this baseline from Go 1.23 after `govulncheck` found reachable
  vulnerabilities with no supported 1.23 fix.
- Prefer standard-library components and narrowly scoped dependencies.
- Run `gofmt -w` on changed Go files, `go vet ./...`, `go test -race ./...`,
  and `go build -trimpath ./cmd/topo` before publishing. Files behind a
  `windows` build tag (Windows service integration) also need
  `GOOS=windows GOARCH=amd64 go vet ./...` and `go build`, matching the CI
  cross-compile check; there is no way to execute them on Linux CI.
- Run the pinned `govulncheck` gate through
  `scripts/security-review-checks.sh` for security-sensitive or release work.
- New protocol plugins need parser tests, configuration validation, connection
  and timeout tests, arbitrary-operation rejection tests, fault isolation, and
  repeat-scan identity tests.
- Work on `agent/<description>` branches and use pull requests. Never rewrite
  shared history or discard unrelated work.
- At milestone completion, update `README.md`, `ROADMAP.md`, relevant protocol
  docs, and the current handoff in `docs/project-plan.md` in the same PR.
