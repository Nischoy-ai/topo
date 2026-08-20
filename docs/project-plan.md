# Nischoy Topo project plan and handoff

This document is the durable source of truth for project direction and
cross-chat continuity. `ROADMAP.md` is the shorter public release roadmap;
`AGENTS.md` contains standing execution rules.

## Current handoff

- **Updated:** 2026-08-19
- **Public repository:** <https://github.com/Nischoy-ai/topo>
- **Latest completed milestone:** SNMP and VMware discovery (`ROADMAP.md`
  M2), both slices done. Slice 1 (SNMP, merged in
  <https://github.com/Nischoy-ai/topo/pull/21>): `pkg/discovery/snmp`
  queries MIB-II `system`/`interfaces` over SNMPv3 via
  `github.com/gosnmp/gosnmp` (pinned `v1.42.1`); asset identity is the
  SNMPv3 engine ID; production requires `authPriv`; Topo Lab's hand-rolled
  `noAuthNoPriv`-only agent (`pkg/lab/snmp_server.go`, built on gosnmp's
  own exported packet decode/encode) backs a two-scan idempotency
  acceptance test. `topo discover snmp` / `topo lab snmp-serve`; see
  `docs/snmp.md`. Slice 2 (VMware, this slice): `pkg/discovery/vmware`
  enumerates `HostSystem`/`VirtualMachine` inventory read-only over the
  vSphere API via `github.com/vmware/govmomi` (pinned `v0.52.0`); asset
  identity is a host's hardware UUID or a VM's VC-managed instance UUID
  (falling back to its BIOS UUID); `vm_runs_on_host`,
  `host_has_interface`, and `vm_has_interface` relationships. Unlike SNMP,
  govmomi ships its own `vcsim` simulator, so the two-scan idempotency and
  fault-isolation acceptance tests run directly against
  `govmomi/simulator` rather than a hand-rolled Topo Lab fixture — with
  TLS and real credential enforcement deliberately turned on, since vcsim
  defaults to plaintext HTTP and open auth. `topo discover vmware`; see
  `docs/vmware.md`. Both slices leave `authPriv`/real-vCenter verification
  against genuinely live systems unverified — implemented and tested
  against faithful simulators only, the same posture as WinRM real-host
  fixtures.
- **Open pull request:** none at last update; slice 1 of the persistent
  storage milestone (below) is starting.
- **Merged pull requests (SNMP/VMware milestone, now complete):** SNMP
  discovery in <https://github.com/Nischoy-ai/topo/pull/21>; VMware
  discovery in <https://github.com/Nischoy-ai/topo/pull/22>.
- **Also verified in an earlier milestone, outside any slice/PR:** given
  access to a real ServiceNow developer instance, ServiceNow's own IRE
  reconciliation behavior was confirmed for real, for both items and
  relationships — submitting a `cmdb_ci_computer` item once creates a CI,
  resubmitting the identical `sys_object_source_info` updates that same CI
  (`operation: UPDATE` against the original `sysId`) rather than
  duplicating it; the same holds for an `IRERelation` between a
  `cmdb_ci_computer` and a `cmdb_ci_network_adapter`, which came back
  `operation: NO_CHANGE` on resubmission. See
  [`docs/servicenow.md`](servicenow.md#verified-against-a-real-instance)
  for full detail and what remains unverified (the other CI classes,
  larger batches, multiple relations, the response schema).
- **Current milestone:** persistent observation/audit storage and
  scheduling (see "Current milestone: persistent observation/audit storage
  and scheduling" above for the full spec). Slice 1 (SQLite-backed
  `store.Repository`, `internal/store/sqlite`, relationships as
  newly-first-class stored data) is starting. The collector
  enrollment/outbound mTLS/rotation/heartbeats/jobs milestone, the
  ServiceNow real-instance validation follow-on, and the SNMP/VMware
  discovery milestone are all complete.
- **Verified in the previous slice (VMware):** Under Go 1.23, `gofmt -l` (clean),
  `go vet ./...` (Linux and `GOOS=windows GOARCH=amd64`), `go test -race
  ./...`, `go build -trimpath ./cmd/topo`, and the Windows cross-compile
  build all pass. New tests cover: pure-function config validation and
  target parsing (embedded-credential rejection, HTTPS-required-outside-
  `-lab`, loopback-only `-lab` targets); pure-function inventory mapping
  (missing-identity objects skipped rather than failing the whole target,
  `InstanceUuid`-preferred-over-`Uuid` fallback, host-moref-to-VM
  relationship resolution, MAC extraction from virtual/physical NICs); a
  two-scan idempotency acceptance test against a real `vcsim` instance
  over HTTPS with enforced credentials, proving stable host/VM identity
  and zero duplicates across repeated scans through `store.Memory`; a
  wrong-password case proving real (not simulator-default-open) auth
  rejection surfaces as a `vmware_connect` error; and an unreachable-
  target case proving a retryable `vmware_connect` error rather than a
  hang or crash. Also manually verified the full CLI flow end to end
  (`vcsim`-backed model → `topo discover vmware -lab` → inspected JSON
  Lines output) via a throwaway harness, not committed to the repository.
- **Explicitly deferred evidence:** Sanitized captures and regression
  fixtures from Windows Server 2022 and one other supported release;
  real-Windows verification of the Topo Agent's Windows service wrapper;
  ServiceNow's own IRE behavior for the CI classes not yet exercised
  against a real instance (`cmdb_ci_disk`, `cmdb_ci_spkg`,
  `cmdb_ci_vm_instance`) and the IRE response schema itself; SNMP
  `authPriv` against a real network device; and VMware discovery against a
  real vCenter or ESXi host beyond `vcsim`. Do not fabricate any of these
  from Topo Lab, `vcsim`, or guessed schemas; obtain them from the real
  controlled system when one becomes available, the same way the
  ServiceNow real-instance evidence above was obtained.
- **Explicit deferral:** Do not make PostgreSQL the next milestone on its
  own — evaluate it as part of the persistent-storage-and-scheduling
  milestone now current, not before. Automatic background Vault token
  renewal for long-running processes, and support for leased
  dynamic-secrets engines beyond token renewal, remain deferred follow-ups.
  One agent instance per host (fixed systemd unit / Windows service name)
  is an intentional Agent MVP limitation, not tracked as a gap. Do not
  attempt to parse or assume ServiceNow's IRE response schema without a
  real instance to verify it against. Certificate revocation is explicitly
  out of scope for the enrollment/outbound-mTLS/rotation slices. Heartbeat
  and job state are in-memory only and do not survive a controller
  restart. SNMPv1/v2c, vendor MIBs, LLDP/CDP topology, and VMware
  datastore/network/resource-pool/folder/vApp inventory are real, scoped
  follow-ups deliberately left out of the SNMP/VMware milestone, not
  silently bundled in — see "Deliberate non-goals for this milestone"
  under the completed-milestone section below.

Before beginning new work, synchronize local `main`, create a focused feature
branch, and replace this handoff when the milestone changes.

## Product strategy

Topo is an open-source discovery data plane that helps Nischoy enter enterprise
accounts through useful infrastructure components before offering a full CMDB.
It supports two acquisition modes:

- **Topo Relay:** agentless, segment-local discovery over protocols such as SSH,
  WinRM, SNMP, VMware, and cloud APIs.
- **Topo Agent:** outbound-only endpoint inventory for systems where remote
  credentials or inbound management access are undesirable.

Both paths emit the same destination-neutral observation contract. Publishers
send normalized configuration items and relationships to ServiceNow or other
CMDBs. The longer-term product family is:

- **Topo Relay** — agentless collector.
- **Topo Agent** — endpoint collector.
- **Topo Hub** — self-hosted controller and local asset view.
- **Topo Connect** — ServiceNow and other CMDB publishers.
- **Topo Graph** — future full CMDB.

## Architectural decisions

1. **Observations before mutable records.** Preserve source evidence in an
   `ObservationEnvelope`; resolve stable assets separately.
2. **Strong identity.** Prefer machine IDs, serials, cloud-native IDs, and
   source-native identifiers. IP address is mutable evidence, not identity.
3. **Destination neutrality.** Core discovery types must not depend on a
   ServiceNow class hierarchy or custom CMDB fields.
4. **Safe remote execution.** Protocol plugins own an exact audited operation
   set. Jobs choose targets and approved options, never command text.
5. **Simulation for scale.** Topo Lab provides deterministic personas, faults,
   ground truth, and repeated-scan evaluation without hundreds of VMs.
6. **Small real compatibility matrix.** Sanitized fixtures and a few real hosts
   validate protocol, authentication, locale, permissions, and OS behavior that
   simulation cannot prove.
7. **Secrets by reference.** Credentials belong in environment/file inputs for
   early evaluation and in secret-provider references for production.
8. **ServiceNow through IRE.** Publish through Identify and Reconcile APIs with
   stable source information; never write CMDB tables directly.
9. **Persistence comes after discovery proof.** In-memory storage is adequate
   for current acceptance tests. Persistent storage follows mixed-host coverage
   and end-to-end CMDB validation.

## Completed foundation

### M0 vertical slice

- Canonical asset, relationship, evidence, error, and observation contracts.
- Local host/interface discovery and in-memory identity resolution.
- Authenticated ingestion/read API with bounded payloads.
- JSON Lines, HTTPS webhook, and ServiceNow IRE publishers/preview.
- Container baseline, CI, tests, and extension documentation.
- Deterministic Topo Lab persona engine, faults, expected graph, and 500-host
  repeated-scan test.

### Linux SSH discovery alpha

- Password and private-key authentication.
- Mandatory host-key policy, connection/command deadlines, concurrency bounds,
  and bounded output.
- Exact audited Linux command contract and parsers.
- Topo Lab SSH frontend using genuine SSH handshakes and session channels.
- Authentication, permission, malformed-output, timeout, and
  arbitrary-command rejection coverage.
- Two scans of 500 Linux personas through 1,000 SSH connections, 100% expected
  coverage of 1,000 assets, and no duplicate resolved assets.

## Windows WinRM discovery alpha implementation

### Objective

Demonstrate safe, repeatable agentless discovery of Windows Server estates and
prove a single Topo Relay can normalize a mixed Linux/Windows environment.

### Deliverables

1. **Audited operation contract**
   - Define fixed WS-Management/CIM and PowerShell operations in code.
   - Separate required identity/hardware operations from optional software,
     patch, and service enumeration.
   - Reject arbitrary command text and unrecognized operations.
   - Never use `Win32_Product`; enumerate installed software from supported
     registry locations to avoid MSI consistency checks.

2. **Parsers and normalization**
   - Computer name, machine identity, domain/workgroup, manufacturer, model,
     BIOS serial, OS edition/version/build, architecture, CPU, and memory.
   - Network adapters, MAC addresses, IP addresses, and relationships.
   - Volumes, services, installed software, and installed patches.
   - Stable identity behavior compatible with existing Topo Lab ground truth.

3. **Production WinRM client**
   - HTTPS server identity verification by default.
   - Connection and operation timeouts, context cancellation, output bounds,
     controlled concurrency, and structured per-target errors.
   - Lab-only Basic authentication over an isolated local endpoint.
   - NTLM/Negotiate for the initial enterprise pilot; track Kerberos and
     certificate authentication as follow-up work if the chosen transport does
     not support them safely in the first slice.
   - Passwords only through secret inputs; no password CLI flag.

4. **Topo Lab WinRM frontend**
   - Reuse Windows 2019, 2022, and 2025 personas.
   - Exercise real HTTP/WS-Management envelopes and operation routing.
   - Simulate authentication failure, timeout, permission denial, malformed
     output, latency/jitter, and disappear-after-first-scan behavior.
   - Expose direct in-memory connection hooks for fast deterministic tests.

5. **CLI and documentation**
   - Add lab serve/target commands and `topo discover winrm`.
   - Document audited operations, authentication, TLS, permissions, fault
     semantics, alpha limitations, and safe pilot deployment.

### Acceptance gates

- Parser and operation-contract unit tests pass under the race detector.
- The lab rejects arbitrary PowerShell/WS-Man operations.
- Required-operation failures isolate the affected target; optional permission
  failures retain a partial host inventory.
- Two scans of 500 simulated Windows hosts reach 100% expected identity
  coverage and create no duplicates.
- A mixed acceptance test scans 500 Linux hosts over SSH and 500 Windows hosts
  over WinRM, then repeats the scan without duplicate stable assets.
- Sanitized fixtures from at least Windows Server 2022 and one other supported
  Windows Server release pass regression tests.
- `gofmt`, `go vet ./...`, `go test -race ./...`, and the production build pass.

### Deliberate non-goals

- No arbitrary remote scripts.
- No general orchestration or software deployment.
- No full Active Directory discovery in this milestone.
- No requirement to provision hundreds of real machines.
- No PostgreSQL dependency.

The implementation, fault coverage, and simulated scale/identity gates above
are complete. The real-host fixture gate is explicitly deferred and remains
open; therefore Topo does not yet claim real-host Windows compatibility.

## Completed milestone: credential references and external secret providers

### Objective

Keep credential values out of command lines, jobs, observations, and logs while
giving every credential consumer one provider-neutral, bounded input contract.

### Slices

1. **Done.** Shared `env:` and absolute-path `file:` references for
   evaluation and mounted secret files, adopted by controller, SSH, and WinRM
   CLI paths.
2. **Done.** Vault provider adapter (`vault:<path>#<field>`, KV version 2)
   with bounded reads, environment-variable authentication guidance, token
   lease lookup/renewal support, cancellation, and redacted provider errors.
   Automatic background renewal for long-running processes and leased
   dynamic-secrets engines beyond token renewal remain deferred.
3. **Done.** Kubernetes Secret provider adapter
   (`k8s:[<namespace>/]<secret-name>#<field>`) authenticating with the pod's
   own service account, with namespace scoping (defaulting to the pod's own
   namespace, overridable per reference), bounded reads, cancellation, and
   redacted API errors. Least-privilege scoping is enforced by Kubernetes
   RBAC on that service account, not by Topo itself; the documentation
   includes a least-privilege `Role`/`RoleBinding` example.

All three slices are implemented; none of them claims a full-featured native
Vault or Kubernetes client (for example, KV version 1, dynamic Vault secrets
engines beyond token renewal, and Kubernetes Secret watch/list are all out of
scope), only the bounded read-one-field contract this milestone needs.

## Completed milestone: outbound-only Topo Agent MVP

### Objective

Give systems that cannot accept inbound remote-management connections or
distribute remote credentials a way to self-report inventory: an agent that
runs on the endpoint, discovers itself, and pushes observations outward to a
Topo Hub controller over HTTPS, buffering to encrypted local storage instead
of dropping data when the controller is unreachable.

### Slices

1. **Done.** Agent core loop (`topo agent run`): reuses the existing
   non-privileged local-host discovery plugin on a configurable interval,
   delivers each observation to the controller's existing
   `POST /v1/observations` endpoint using the existing bearer API key
   credential-reference contract, and on delivery failure spills to a
   bounded, AES-256-GCM-encrypted on-disk spool keyed by a credential
   reference (so the spool key can live in `env:`, `file:`, `vault:`, or
   `k8s:`, like every other Topo secret). Each run first retries anything
   already spooled, oldest first, before discovering again. Graceful
   shutdown on SIGINT/SIGTERM matches the existing `serve` and `lab serve`
   commands. No new transport, authentication, or discovery protocol: this
   slice is existing building blocks wired into a loop.
2. **Done.** Linux systemd unit (`packaging/systemd`) and Windows service
   wrapping (`cmd/topo/service_windows.go`, `topo agent install`/
   `uninstall`) so `topo agent run` survives reboots and restarts on
   failure, plus install/uninstall documentation in `docs/topo-agent.md`.
   The systemd unit was verified with `systemd-analyze verify` and a real
   install/run/teardown cycle in a scratch environment; the Windows service
   code is verified only by cross-compilation and code review, not against
   a real Windows Service Control Manager — that remains an explicit
   deferred verification gate alongside the WinRM real-host fixtures.

Both slices are implemented; this milestone is complete.

### Deliberate non-goals for this milestone

- No collector enrollment, outbound mTLS, certificate rotation, or
  heartbeats; the agent authenticates with the same static bearer API key
  `topo serve` already accepts. Enrollment and mTLS are a later, separately
  scoped roadmap item.
- No job delivery or remote-controlled behavior; the agent only self-reports
  on its own schedule.
- No dynamic secrets-engine leasing for the spool encryption key beyond what
  the existing credential-reference providers already give it.
- No macOS agent in this milestone.

### Acceptance gates

- Spool encryption round-trips exactly and detects tampering (AES-GCM
  authentication failure) rather than silently returning corrupted data.
- The spool enforces a configurable byte bound and reports a clear error
  rather than growing without limit when the controller is unreachable for
  an extended period.
- An integration test runs the agent loop against a real in-process
  controller (`internal/controller` behind `httptest`): observations arrive
  while the controller is reachable, buffer while it is not, and drain once
  it recovers, with no observation lost or duplicated in the store.
- `gofmt`, `go vet ./...`, `go test -race ./...`, and the production build
  pass, matching every other milestone in this project.

## Completed milestone: ServiceNow IRE duplicate-CI validation

### Objective

Prove that Topo's ServiceNow publisher sends idempotent, duplicate-free
Identify & Reconcile payloads — the same physical asset always maps to the
same `(className, source_native_key)` pair, both within a single batch and
across independently repeated scans — which is the precondition for
ServiceNow's own IRE engine to reconcile to one CI rather than create
duplicates.

This milestone is deliberately scoped to what Topo itself controls. There is
no ServiceNow instance available to this project to develop or test
against, and ServiceNow's IRE response schema is proprietary and
undocumented outside an instance's own scripted REST API definitions.
Claiming to validate ServiceNow's own identification/reconciliation
behavior without a real instance would mean fabricating unverified
real-system behavior, which this project's own conventions (WinRM real-host
fixtures, Windows service verification) explicitly avoid. "Duplicate-CI
validation" here therefore means proving Topo's outbound behavior is
correct, not ServiceNow's.

### Slices

1. **Done.** Fix within-batch duplicate emission: `mapPayload` previously
   appended a new IRE item every time an asset's native ID appeared in the
   input, so the same asset present in more than one input envelope (for
   example, a batch of several buffered observations covering the same
   host) produced two IRE items with the identical `source_native_key`.
   It now deduplicates by `source_native_key` (most recent observation
   wins, matching `store.Memory`'s resolved-asset semantics) and
   deduplicates relationships by `(type, from, to)`.
2. **Done.** Cross-scan idempotency validation using Topo Lab's existing
   two-scan pattern (the same one the SSH/WinRM acceptance gates use):
   `TestMapPayloadIsIdempotentAcrossRepeatedLabScans` asserts the mapped
   `(source_native_key, className)` set from two independent discovery runs
   of the same estate is identical, and
   `TestPublishBatchSendsIdempotentRequestsAcrossRepeatedLabScans` asserts
   the same at the wire level (method, path, auth header, source keys)
   against a fake IRE endpoint that exists to validate Topo's request
   generation, not to simulate ServiceNow's response behavior.
3. **Done.** `PublishBatch` captures the (bounded) response body in
   `Diagnostics` for operator review, without parsing or depending on any
   particular field of it, since that schema is unverified.
4. **Done.** `docs/servicenow.md` documents exactly what is and is not
   validated, and what an operator must still do (configure identification
   rules per CI class, validate against a real/sandboxed instance) before
   claiming production readiness.

### Deliberate non-goals for this milestone

- No parsing of ServiceNow's IRE response body; its schema is proprietary
  and unverified without a real instance.
- No claim about ServiceNow's own identification/reconciliation behavior;
  only Topo's outbound payload is validated.
- No real or sandboxed ServiceNow instance integration test; that requires
  infrastructure this project does not have access to and remains an
  explicit deferred gate before production readiness, alongside WinRM
  real-host fixtures and real-Windows Topo Agent service verification.

### Acceptance gates

- `mapPayload` never emits two items with the same `source_native_key`, or
  two identical relationships, from one `PublishBatch`/`Preview` call.
- The `(source_native_key, className)` set `mapPayload` produces from two
  independently repeated Topo Lab discovery scans of the same estate is
  identical.
- The actual HTTP requests `PublishBatch` sends for those same two scans
  are identical in method, path, auth header, and source keys.
- `gofmt`, `go vet ./...`, `go test -race ./...`, and the production build
  pass, matching every other milestone in this project.

### Follow-on (2026-08-19): real-instance validation

The gate this milestone deliberately left open — "no claim about
ServiceNow's own identification/reconciliation behavior" — is now partly
closed. Given access to a real ServiceNow developer instance, the actual
`POST /api/now/identifyreconcile/enhanced` behavior was exercised directly
with the exact payload shape `mapPayload` produces: submitting a
`cmdb_ci_computer` item once created a CI (`operation: INSERT`);
resubmitting the identical `sys_object_source_info` reconciled to the same
CI (`operation: UPDATE` against the original `sysId`, matched via
`sys_object_source`) rather than creating a duplicate. This is the first
real evidence — not an assumption — that Topo's payload construction
actually satisfies what ServiceNow's IRE needs to reconcile correctly. A
real, previously-unknown requirement was also found this way: `cmdb_ci`'s
`discovery_source` field is a registered choice list, not free text, so an
unregistered discovery source is rejected outright
(`INVALID_INPUT_DATA`) — a production deployment must register it via
`sys_choice` before any write succeeds. A follow-up test extended this to
`IRERelation` payloads: two items (`cmdb_ci_computer` and
`cmdb_ci_network_adapter`) plus a relation between them reconciled the
same way on resubmission — the relation itself came back `operation:
NO_CHANGE`, not a duplicate `cmdb_rel_ci` row, confirmed by a direct table
query. Full detail, scope, and what remains unverified (the other CI
classes, larger multi-item batches, multiple relations in one request, the
IRE response schema) is in
[`docs/servicenow.md`](servicenow.md#verified-against-a-real-instance).
This did not reopen the milestone or its slices above, which remain
accurate as a record of what shipped in PR #13; it is additional evidence
obtained afterward, once instance access became available.

## Completed milestone: collector enrollment, outbound mTLS, rotation, heartbeats, and jobs

### Objective

Move the controller from a single shared bearer API key toward per-collector
identity: each collector proves itself once via a short-lived enrollment
token and receives its own client certificate, which becomes the basis for
mutually authenticated transport, liveness tracking, and eventually
controller-assigned work. This roadmap line names five distinct
capabilities; it is deliberately staged as multiple slices rather than one
PR, matching every other multi-part milestone in this project.

### Slices

1. **Done.** Collector enrollment: the controller becomes its own
   certificate authority (`internal/enrollment`, ECDSA P-256, self-signed,
   persisted to `-ca-dir` with the private key protected by filesystem
   permissions like every other private key in this project, not a second
   application-level encryption layer). An admin mints a single-use,
   time-bounded enrollment token via `POST /v1/enrollment-tokens` (existing
   bearer-key auth). A collector generates its own key pair locally — the
   private key is never transmitted, only the certificate signing request
   (CSR) — and submits it with the token to `POST /v1/enroll`, which
   validates the CSR's self-signature before redeeming the token (so a
   malformed request never burns a valid token), then issues a
   short-lived (90-day) client-auth certificate plus the CA certificate.
   `topo agent enroll` is the collector-side CLI command. Enrollment is
   opt-in: `topo serve` without `-ca-dir` behaves exactly as before, and
   `/v1/enrollment-tokens`/`/v1/enroll` return 501 when not configured.
2. **Done.** Outbound mTLS: wire the enrolled certificate into live traffic.
   `topo serve -mtls` gains a native TLS listener — the controller issues
   itself a server certificate from the same CA that signs collector
   certificates (`enrollment.IssueServerCertificate`, 1-year TTL, generated
   fresh on every start rather than persisted) — and verifies client
   certificates presented against that CA
   (`tls.VerifyClientCertIfGiven`, not `RequireAndVerifyClientCert`: the TLS
   layer must still accept a handshake with no client certificate at all,
   because a collector's first-ever request, `POST /v1/enroll`, has none to
   present; the `auth()` middleware enforces the requirement — a verified
   peer certificate or the bearer API key — per endpoint instead). A
   verified peer certificate satisfies `auth()` without the bearer key.
   `topo agent run -mtls-cert-dir` and `internal/agent.Sender` gain a way to
   present the enrolled certificate on outbound requests instead of, or
   alongside, the bearer API key
   (`enrollment.LoadClientTLSConfig`/`agent.NewSender`'s new `tlsConfig`
   parameter). `topo agent enroll` gains `-controller-ca-cert` to pin the
   controller's self-signed CA certificate for the enrollment request
   itself (distributed out-of-band alongside the token, the same way the
   token already is), solving the bootstrap trust problem a self-signed
   `-mtls` controller otherwise creates for an ordinary HTTPS client.
3. **Done.** Certificate rotation: renew a collector's certificate before it
   expires, authenticated by the current still-valid certificate rather
   than a new enrollment token. `POST /v1/rotate` requires an already
   TLS-verified peer certificate — deliberately no bearer-API-key fallback,
   since accepting one would let anyone holding the shared key mint a
   certificate for any collector ID — and derives the collector ID to
   reissue from that peer certificate's subject, not from anything in the
   request body, so a collector can only ever rotate its own identity.
   `topo agent rotate` is the collector-side CLI command: it presents the
   certificate in `-cert-dir` over mTLS, generates a fresh key pair and CSR
   (rotation renews the key, not just the certificate), and overwrites
   `-cert-dir` with what the controller returns. Rotation is manual, not a
   background loop inside `agent run`: a running `agent run` process loads
   its certificate once at startup and does not reload it live, so an
   operator (or a scheduler) invoking `agent rotate` must also restart
   `agent run` afterward for the renewed certificate to take effect.
4. **Done.** Heartbeats: `POST /v1/heartbeats` is a lightweight liveness
   signal, distinct from observation delivery, so the controller can tell
   a collector is alive between scans without waiting on the
   discovery/delivery `-interval` (often 15+ minutes). `topo agent run`
   sends it on its own independent cadence, `-heartbeat-interval` (default
   one minute, `0` disables it) — a second ticker inside `agent.Run`,
   decoupled from `-interval` entirely. Unlike `POST /v1/rotate`,
   heartbeats accept the bearer API key as well as a verified mTLS
   certificate (`s.auth()`, the same middleware every other data-plane
   endpoint uses) — there's no analogous "any holder can impersonate any
   collector" risk, since a heartbeat only ever asserts liveness, not an
   identity that gets material (a certificate) issued to it; when a
   verified peer certificate is present, its subject overrides whatever
   `collector_id` the request body claims, matching rotation's identity
   rule, but bearer-key-authenticated heartbeats have no such stronger
   signal to fall back on. `GET /v1/collectors` lists every collector's
   most recent heartbeat and whether it is still within
   `enrollment`-independent `controller.DefaultHeartbeatStaleAfter` (three
   minutes). Both endpoints are always registered, not gated behind a
   flag: heartbeats need no CA or additional infrastructure, only
   whichever auth a collector already has. A failed heartbeat is logged
   and dropped, never spooled or retried — unlike a failed observation
   delivery, a stale heartbeat has no lasting value once the next one
   supersedes it. See `docs/heartbeats.md`.
5. **Done.** Job delivery: since Topo Agent is deliberately outbound-only
   (it never accepts inbound connections), this is collector-initiated
   polling rather than a server push. `POST /v1/jobs` queues one job (an
   operator names the target `collector_id`); `GET /v1/jobs` returns and
   marks-dispatched every job queued for the polling collector (at most
   once — a crash between poll and result loses the job, no redelivery);
   `POST /v1/jobs/{id}/result` reports the outcome; `GET /v1/jobs/{id}` is
   a read-only status lookup with no dispatch side effect, for an operator
   checking on a job independent of the collector's own poll. Polling and
   reporting are identity-bound the same way as `POST /v1/rotate` and
   `POST /v1/heartbeats`: a verified mTLS peer certificate's subject
   overrides whatever `collector_id` the caller claims otherwise, via
   the shared `collectorIdentity` helper (also now used by the heartbeat
   handler, replacing its previous inlined copy of the same logic).
   `topo agent run` polls for jobs on the same `-heartbeat-interval`
   cadence it already uses for liveness heartbeats — no new flag — since
   both are cheap, frequent check-ins distinct from the heavier discovery
   `-interval`. There is exactly one job type, `discover`, since it is
   the only capability the agent actually has; it reuses the existing
   `discoverAndSend` helper directly (now returning an error so a job's
   reported outcome reflects whether discovery itself succeeded, not
   whether the resulting observation was delivered synchronously —
   delivery keeps its own independent spool-retry path regardless of how
   discovery was triggered). Always registered, like heartbeats: no CA or
   opt-in flag required. See `docs/jobs.md`.

### Deliberate non-goals for slice 1

- No certificate revocation. A compromised collector key is contained by
  the bounded 90-day certificate TTL, not by a revocation list; rotation
  (slice 3, done — see below) renews a certificate but does not add
  revocation, which remains a future, separately scoped addition if the
  bounded TTL proves insufficient on its own.
- No persistent CA/token storage beyond the CA key/cert files themselves:
  the token store is in-memory, matching every other piece of controller
  state today, so an in-flight enrollment must be retried with a freshly
  minted token after a controller restart.
- No change to existing bearer-key authenticated behavior; enrollment is
  purely additive and opt-in via `-ca-dir`.
- No live mTLS transport yet (slice 2) — this slice proves the enrollment
  primitive (token → CSR → signed, CA-verifiable certificate) independent
  of how it will later be used for live authentication.

### Deliberate non-goals for slice 2

- No certificate rotation. The controller's own server certificate is
  reissued fresh on every `topo serve -mtls` start rather than persisted or
  renewed while the process runs; its 1-year TTL is chosen to outlive
  reasonably long controller uptimes, not to bound compromise the way a
  collector certificate's 90-day TTL does. Collector certificate rotation
  is slice 3 (done — see below); the controller's own server certificate is
  not rotated by that slice either, since it is never persisted in the
  first place.
- No change to existing bearer-key or plain-HTTP behavior; `-mtls` is
  opt-in and requires `-ca-dir`, and `-mtls-cert-dir` on `topo agent run` is
  independent of `-api-key-ref` — setting one does not disable the other.
- No automatic reverse-proxy replacement guidance change for deployments
  that do not opt into `-mtls`; they still need an operator-provided
  TLS-terminating reverse proxy, exactly as before this slice.

### Deliberate non-goals for slice 3

- No automatic/background rotation. `topo agent rotate` is a manual (or
  externally scheduled) CLI command, not a loop inside `agent run`; a
  running `agent run` process does not reload its certificate live and
  must be restarted after rotation. In-process automatic renewal is a
  possible future refinement, not required to satisfy this slice's
  "renew before expiry" goal.
- No revocation, still, as in slices 1 and 2 — rotation renews a
  certificate but has no mechanism to invalidate one before its natural
  expiry.
- No rotation of the controller's own `-mtls` server certificate; it is
  reissued fresh on every `topo serve -mtls` start regardless, so there is
  nothing to rotate mid-run.
- No bearer-API-key path for rotation, by design, not oversight — see the
  slice 3 description above for why.

### Deliberate non-goals for slice 4

- No historical heartbeat log; `GET /v1/collectors` reports only each
  collector's single most recent heartbeat.
- No alerting when a collector goes stale; `GET /v1/collectors` must be
  polled by whatever consumes it.
- No spooling or retry for a failed heartbeat, unlike a failed
  observation delivery — deliberate, not an oversight, since a stale
  heartbeat has no value once the next one supersedes it a
  `-heartbeat-interval` later.
- No persistent heartbeat storage; like the enrollment token store, it is
  in-memory only and does not survive a controller restart.
- No per-collector configurable staleness threshold on the controller;
  `controller.DefaultHeartbeatStaleAfter` is one fixed constant for every
  collector, since the controller has no reliable way to know an
  individual collector's actual configured `-heartbeat-interval`.

### Deliberate non-goals for slice 5

- No job listing or browsing endpoint; `GET /v1/jobs/{id}` looks up one
  job by ID only. An operator must keep track of the `job_id`
  `POST /v1/jobs` returned.
- No job cancellation once queued.
- No job redelivery. `GET /v1/jobs` marks a job dispatched the instant it
  is returned; a collector that crashes before reporting a result loses
  that job, with no automatic retry. Deliberate, matching this project's
  preference for simple, explicit behavior over a queue with redelivery
  semantics that would need their own edge cases worked out — an operator
  who still wants the work done resubmits it.
- No job types beyond `discover`. Nothing else is a real, honest
  capability of `topo agent run` today, so nothing else is offered.
- No persistent job storage; like the enrollment token store and
  heartbeat store, `JobStore` is in-memory only and does not survive a
  controller restart.
- No separate job-polling cadence or flag; it rides `-heartbeat-interval`
  on purpose, to avoid a second ticker and a second flag for what is,
  operationally, the same kind of frequent, cheap check-in as a
  heartbeat.

### Acceptance gates (slice 1)

- A minted enrollment token can be redeemed exactly once; a second
  redemption attempt fails with the same error as an unknown or expired
  token.
- A structurally invalid CSR is rejected without consuming the token, so a
  malformed request can be retried with the same token.
- An issued certificate verifies against the CA certificate returned in the
  same response, has the requested collector ID as its subject common name,
  and carries the TLS client-authentication extended key usage.
- An end-to-end test exercises the real HTTP client
  (`enrollment.Enroll`) against a real controller handler, not just
  hand-built requests, and was additionally verified manually with
  `openssl x509`/`openssl verify` against a running `topo serve` and
  `topo agent enroll`, independent of Go's own `crypto/x509` implementation.
- `gofmt`, `go vet ./...`, `go test -race ./...`, `go build -trimpath
  ./cmd/topo`, and the `GOOS=windows` cross-compile check all pass.

### Acceptance gates (slice 2)

- A request presenting a client certificate verified against the
  controller's CA reaches protected endpoints without a bearer
  `Authorization` header.
- A request presenting neither a verified client certificate nor a correct
  bearer key is rejected (401) by every endpoint that requires either.
- `POST /v1/enroll` still succeeds over the `-mtls` listener from a client
  presenting no certificate at all — proven by a test that exercises the
  real `httptest`-driven TLS handshake, not just the application-level
  handler, so a regression to `RequireAndVerifyClientCert` (which would
  break every collector's first-ever enrollment) is caught at the TLS
  layer, not just the HTTP layer.
- `internal/agent.Sender`, configured with an enrolled certificate and no
  API key, delivers successfully to a controller running `-mtls` with a
  bearer key configured — proving certificate-only authentication actually
  works end to end, not just that the controller *accepts* certificates in
  isolation.
- `topo agent enroll -controller-ca-cert` completes successfully against a
  live `topo serve -mtls` controller with a self-signed certificate, and
  fails without `-controller-ca-cert` against the same controller — proven
  both by unit test and by a manual run of the real CLI binaries end to
  end (mint token → enroll → run → observations land at the controller),
  matching the manual-verification bar every other slice in this project
  has met.
- `gofmt`, `go vet ./...`, `go test -race ./...`, `go build -trimpath
  ./cmd/topo`, and the `GOOS=windows` cross-compile check all pass.

### Acceptance gates (slice 3)

- A collector presenting its currently-valid certificate over mTLS can
  obtain a fresh certificate from `POST /v1/rotate` with no token and no
  bearer key, and the new certificate has a different serial number and a
  freshly generated key than the one it rotated from.
- A request to `POST /v1/rotate` presenting no client certificate at all
  is rejected — proven against a real TLS handshake, not just the
  application-level handler.
- A request to `POST /v1/rotate` presenting the correct bearer API key but
  no client certificate is still rejected, proving there is no bearer-key
  fallback for this endpoint specifically (unlike every other protected
  endpoint).
- A CSR submitted to `POST /v1/rotate` requesting a different collector ID
  than the one on the presenting peer certificate is ignored: the issued
  certificate's subject always matches the peer certificate's identity,
  never the CSR's requested one.
- `topo agent rotate` against a live `topo serve -mtls` controller
  overwrites `-cert-dir` with a certificate that a subsequent
  `topo agent run -mtls-cert-dir` can deliver observations with, and that
  delivery still requires no bearer key even when the controller strictly
  enforces one — proven by a manual run of the real CLI binaries end to
  end (enroll → rotate → run → observations land at the controller),
  matching the manual-verification bar every other slice in this project
  has met.
- `gofmt`, `go vet ./...`, `go test -race ./...`, `go build -trimpath
  ./cmd/topo`, and the `GOOS=windows` cross-compile check all pass.

### Acceptance gates (slice 4)

- `POST /v1/heartbeats` accepts either the bearer API key or a verified
  mTLS client certificate, and `GET /v1/collectors` then reports that
  collector as alive.
- A heartbeat over mTLS is recorded under the verified peer certificate's
  identity even when the request body claims a different `collector_id` —
  proven the same way as rotation's identical rule: a CSR-equivalent
  spoofing attempt is ignored, not honored.
- `agent.Run`, given an `Interval` far longer than the test's own
  deadline and a short `HeartbeatInterval`, still causes the controller to
  record the collector as alive — proven end to end against a real
  `controller.Server` over `httptest`, not a mocked heartbeat call, so a
  regression that accidentally coupled the two tickers together would be
  caught.
- `agent.Run` with `HeartbeatInterval` left at its zero value sends no
  heartbeats at all, confirming the feature is opt-in at the library level
  even though the CLI defaults `-heartbeat-interval` to one minute.
- A collector's status flips from alive to not-alive once its last
  heartbeat is older than the configured staleness threshold — proven
  with an injected short threshold, not a real multi-minute wait.
- Manually verified against the real CLI binaries: a collector running
  with a discovery `-interval` deliberately far longer than the test
  window (so no observation delivery can be responsible) still appears
  alive in `GET /v1/collectors`, driven entirely by
  `-heartbeat-interval`'s independent ticker.
- `gofmt`, `go vet ./...`, `go test -race ./...`, `go build -trimpath
  ./cmd/topo`, and the `GOOS=windows` cross-compile check all pass.

### Acceptance gates (slice 5)

- A job queued for collector A is not returned by collector B's poll,
  proven with two distinct collector IDs against the same `JobStore`.
- A job is returned by exactly one poll; a second poll for the same
  collector after the first does not redeliver it.
- A poll or result report over mTLS claiming a different `collector_id`
  than the verified peer certificate's real identity is bound to that
  real identity anyway — the same identity rule already proven for
  `POST /v1/rotate` and `POST /v1/heartbeats`, verified here specifically
  for `GET /v1/jobs` and `POST /v1/jobs/{id}/result`.
- A result reported for a job dispatched to a different collector, or for
  a job never dispatched, or reported twice for the same job, is
  rejected.
- `POST /v1/jobs` with an unsupported `type` is rejected at creation
  (400), not accepted and left to fail silently later.
- `agent.Run`, given an `Interval` far longer than the test's own
  deadline and a short `HeartbeatInterval`, still causes a queued
  `discover` job to be polled, executed, and reported as `succeeded` —
  proven end to end against a real `controller.Server` over `httptest`,
  confirming discovery/delivery happened as a direct result of the job,
  not the (effectively disabled) `Interval` ticker.
- A job whose discovery pass fails is reported as `failed`, with a
  non-empty error, not silently dropped or reported as `succeeded`.
- Manually verified against the real CLI binaries: a collector running
  with `-interval 100h` picks up and executes a `discover` job queued via
  `curl`, purely through `-heartbeat-interval`'s poll, and the job's
  status transitions to `succeeded` — the same "isolate the mechanism
  from the discovery ticker entirely" pattern used to verify heartbeats.
- `gofmt`, `go vet ./...`, `go test -race ./...`, `go build -trimpath
  ./cmd/topo`, and the `GOOS=windows` cross-compile check all pass.

This completes all five slices of the "collector enrollment, outbound
mTLS, rotation, heartbeats, and jobs" milestone.

## Completed milestone: SNMP and VMware discovery

### Objective

Extend Topo's discovery surface beyond SSH/Linux and WinRM/Windows host
discovery to network equipment (via SNMPv3) and virtualization inventory
(via VMware vCenter), matching `ROADMAP.md`'s M2 line: "Rate-limited
allowlisted sweep, SNMPv3, topology, and VMware vCenter plugins." Like
every other multi-part milestone in this project, this is staged as
separate slices rather than one PR — SNMP and VMware are different
protocols, different dependencies, and different testing strategies, so
they do not share a slice.

### Slices

1. **Done.** SNMP device identity and interfaces: a new `pkg/discovery/snmp` plugin
   implementing the existing `discovery.Plugin` interface, querying MIB-II
   (`system` and `interfaces` groups — `sysDescr`, `sysObjectID`,
   `sysUpTime`, `sysName`, and an `ifTable` walk) over SNMPv3 using
   `github.com/gosnmp/gosnmp` — the ecosystem-standard pure-Go SNMP client;
   hand-rolling SNMP's BER/ASN.1 wire format and SNMPv3's USM
   authentication/privacy crypto (RFC 3414/3826) from scratch is exactly
   the kind of well-trodden, security-sensitive protocol work this
   project's "prefer standard-library components and narrowly scoped
   dependencies" principle exists to weigh against, not to forbid outright
   — this is the project's third external dependency, after
   `golang.org/x/crypto` (SSH) and `github.com/Azure/go-ntlmssp` (WinRM
   NTLM), each added for the same reason. Pinned to `v1.42.1`, the last
   version declaring `go 1.22` compatibility with this project's `go
   1.23.0` — newer versions require `go 1.24` and were deliberately not
   used to avoid silently bumping the language version CI is pinned to.
   Production requires `authPriv` (SHA/AES), mirroring how WinRM's
   production path requires NTLM+HTTPS and only permits a weaker mode
   (Basic auth) inside explicit `LabMode`; Topo Lab's SNMP agent —
   necessarily hand-rolled, since gosnmp is client-only and there is no
   equivalent "vcsim"-style SNMP agent simulator to reuse — supports
   `noAuthNoPriv` only, so the two-scan idempotency acceptance test
   exercises real SNMPv3 message framing and the plugin's parsing/mapping
   logic without requiring a from-scratch reimplementation of USM's HMAC
   and AES-CFB crypto on the server side. `authPriv` is implemented via
   gosnmp's own (independently maintained and widely used) client-side USM
   implementation, but — like WinRM real-host fixtures and Windows Service
   Control Manager verification before it — is implemented-but-unverified
   against a real device until one is available; do not represent it as
   validated against real network equipment. CLI: `topo discover snmp`
   (production and `-lab`) and `topo lab snmp-serve` (binds one loopback
   UDP socket per simulated device and prints `host:port` targets, since
   — unlike the SSH/WinRM Lab servers, which multiplex by username behind
   one fixed `-addr` — SNMP has no connection-level identity to multiplex
   on, so serving and listing targets are one command rather than two).
   See `docs/snmp.md`.
2. **Done.** VMware vCenter virtual machine and host inventory: a new
   `pkg/discovery/vmware` plugin implementing `discovery.Plugin`, using
   `github.com/vmware/govmomi` (the official vSphere Go SDK, pinned to
   `v0.52.0` — the last release declaring `go 1.23.0` compatibility) to
   enumerate `HostSystem` and `VirtualMachine` objects read-only via a
   property-collector container view, with a fixed property set
   (`name`/`summary`/`config.network` for hosts,
   `name`/`summary`/`config.hardware.device` for VMs) — no configuration,
   power, or lifecycle operation is ever issued. This is the project's
   fourth external dependency, added for the same reason as
   `golang.org/x/crypto`, `github.com/Azure/go-ntlmssp`, and
   `github.com/gosnmp/gosnmp`. Asset identity is never an IP address or
   inventory path: a host's identity is its hardware UUID, a VM's is its
   VC-managed instance UUID (falling back to its BIOS UUID for standalone
   ESXi hosts with no vCenter to assign one); a `vm_runs_on_host`
   relationship links each VM to its running host, and
   `host_has_interface`/`vm_has_interface` link each host/VM to its
   interface assets, mirroring the naming convention SSH/WinRM/SNMP already
   established. Listing hosts is required (a failure fails the whole
   target with `vmware_operation`); listing VMs is optional (a failure
   emits a retryable `vmware_partial` and returns host-only inventory),
   the same required/optional split those other plugins use. Production
   requires HTTPS with normal certificate verification; `-lab` permits HTTP
   and skipped certificate verification, restricted to loopback targets,
   mirroring WinRM's `-lab-basic` and SNMP's `-lab`. Unlike SNMP, govmomi
   ships its own vCenter simulator (`vcsim`) built for exactly this kind of
   testing, so Topo Lab has no hand-rolled VMware fixture — the two-scan
   idempotency acceptance test and fault-isolation tests (wrong password,
   unreachable target) run directly against `govmomi/simulator`, over real
   HTTPS SOAP with a self-signed certificate and real credential
   enforcement (vcsim's default open-auth mode was deliberately overridden
   for the wrong-password test to be meaningful — see
   `pkg/discovery/vmware/integration_test.go`). CLI: `topo discover vmware`
   (production and `-lab`); no `topo lab vmware-serve` was added, since
   `govmomi/simulator` already serves this role directly in tests and via
   its own upstream tooling for manual exploration. Real vCenter/ESXi
   verification beyond vcsim has not been performed — implemented and
   tested against a faithful simulator, not yet proven against a live
   system, the same posture as WinRM real-host fixtures and SNMP `authPriv`.
   See `docs/vmware.md`.

This completes both slices of the SNMP/VMware discovery milestone.

### Deliberate non-goals for this milestone

- No SNMPv1/v2c support. Production targets community-string SNMP as a
  legacy, lower-security protocol this project does not want to
  standardize collector credentials around; if a real deployment needs it,
  that is a separate, explicitly scoped follow-up, not silently bundled
  into "SNMP discovery."
- No vendor-specific MIBs (Cisco, Juniper, etc.) or topology protocols
  (LLDP/CDP) in this slice — MIB-II only. Vendor MIB support is real,
  useful, unbounded scope better added incrementally once the core plugin
  and its testing pattern exist.
- No real-device verification for SNMP. Like WinRM real-host fixtures, this
  is an explicit deferred gate, not a claim of completeness — Topo Lab's
  `noAuthNoPriv`-only agent proves the plugin's own logic, not
  interoperability with real network equipment or `authPriv` against a
  real USM implementation other than gosnmp's own.
- No datastore, network, resource pool, folder, or vApp inventory for
  VMware — `HostSystem` and `VirtualMachine` only. No VMware Tools-reported
  guest IP addresses either: guest network state requires Tools running,
  which is not guaranteed, so virtual NIC identity comes from the VM's own
  hardware configuration (always available) instead.
- No real vCenter/ESXi verification beyond `vcsim`. The same deferred-gate
  posture as SNMP's real-device verification: implemented and tested
  against a faithful simulator, not yet proven against a live system.

## Current milestone: persistent observation/audit storage and scheduling

### Objective

Close the gap `ROADMAP.md`'s release gates already name explicitly: "No
production claim is made until mTLS enrollment, persistent storage, audit
logs, ... pass." Today `internal/store.Memory` is the only
`store.Repository` implementation, and every other piece of controller
state (enrollment tokens, heartbeats, jobs) is also in-memory-only by
explicit prior design — none of it survives a restart. Discovery is
scheduled only client-side, via `topo agent run -interval`; the controller
has no notion of a recurring schedule, only one-off jobs. Like every other
multi-part milestone in this project, this is staged as separate slices.

### Storage technology decision

`modernc.org/sqlite`, pinned to `v1.39.0` — the last release declaring `go
1.23.0` compatibility with this project's pinned toolchain (newer releases
require `go 1.24`/`go 1.25` and were deliberately not used, the same
reasoning applied to every prior dependency pin in this project). It is a
pure-Go transpilation of SQLite's C source (no cgo), which matters
concretely here: this project's CI cross-compiles for Windows
(`GOOS=windows GOARCH=amd64 go build`), and a cgo-based SQLite driver would
require a Windows C cross-compiler this CI does not have. This is the
project's fifth external dependency, after `golang.org/x/crypto`,
`github.com/Azure/go-ntlmssp`, `github.com/gosnmp/gosnmp`, and
`github.com/vmware/govmomi` — added for the same reason each of those was:
implementing a durable, concurrent-safe, ACID storage engine from scratch
is exactly the kind of well-trodden work this project's dependency
philosophy exists to weigh against, not to forbid outright.

PostgreSQL is deliberately not used yet. `AGENTS.md` already says not to
make it "the next milestone on its own — evaluate it as part of the
persistent-storage-and-scheduling milestone now current, not before," and
having actually reached this milestone, the evaluation's conclusion is:
Topo has no HA/clustered-controller story yet (a single controller process
is still the only supported deployment shape — see `SECURITY.md`), so a
client-server database operators must additionally provision and manage
is not yet justified by anything Topo actually needs. SQLite is a single
file, requires no separate service, and is sufficient for a single
controller process. The `Repository` interface change in slice 1 is
designed so a `postgres` driver can be added later as a third option
without another interface change — this is a capacity decision, not a
architecture lock-in, and should be revisited once HA/clustering is
actually on the roadmap rather than assumed now.

### Slices

1. Persistent storage: a new `internal/store/sqlite` package implementing
   the existing `store.Repository` interface (observations, assets) plus a
   new `ListRelationships` method both `Memory` and the new SQLite backend
   must implement — relationships are not currently queryable at all
   through `store.Repository`, even though `Memory.SaveObservation`
   receives them in every envelope; this is a real gap being fixed here,
   not scope creep, since retrofitting a persisted schema after the fact
   is a real migration cost. `model.StableRelationshipID` mirrors the
   existing `model.StableAssetID` scheme (hash of type/from/to). CLI:
   `topo serve -db-driver sqlite -db-dsn <path>` (default `-db-driver
   memory`, today's unchanged behavior). A shared black-box test suite runs
   identical assertions against both `Memory` and the SQLite backend
   through the `Repository` interface alone, so the two implementations
   cannot silently diverge in observable behavior. New `GET
   /v1/relationships` endpoint alongside the existing `GET /v1/assets` and
   `GET /v1/observations`. See `docs/storage.md`.
2. Immutable audit log: an append-only record of admin/security-relevant
   controller actions (enrollment token issuance, enrollment, certificate
   rotation, job creation), persisted the same way observations are.
   "Immutable" here means hash-chained entries (each entry's stored hash
   covers its own content and the previous entry's hash, so removing or
   editing an entry after the fact is detectable, not that the underlying
   storage is physically write-once) — the same class of guarantee, not
   cryptographic non-repudiation. Scope and exact API surface to be
   finalized when this slice starts.
3. Server-side recurring discovery scheduling: today `POST /v1/jobs`
   queues exactly one job; there is no controller-side notion of "run
   `discover` for this collector every N minutes" independent of whatever
   `-interval` a given `topo agent run` happens to be started with.
   Scope and exact API surface to be finalized when this slice starts.

### Deliberate non-goals for slice 1

- No PostgreSQL backend yet — see "Storage technology decision" above.
- No migration of existing in-memory enrollment-token/heartbeat/job state
  to persistent storage in this slice. Those remain explicitly
  in-memory-only per their own prior design notes; whether they need to
  become durable is a question for a later slice once discovery data
  persistence itself is proven, not assumed now.
- No schema versioning/migration framework beyond what SQLite's own
  `PRAGMA user_version` and a small in-code migration table need for this
  project's single-controller deployment shape; a dedicated migration tool
  is unwarranted complexity until there is more than one schema revision
  to manage in practice.

## Follow-on order

With the credential-provider, Topo Agent MVP, ServiceNow IRE duplicate-CI
validation, collector enrollment/outbound mTLS/rotation/heartbeats/jobs, and
SNMP/VMware discovery milestones complete, pursue these slices in order
unless evidence from an enterprise pilot changes the priority:

1. Persistent observation/audit storage and scheduling (current milestone);
   evaluate PostgreSQL at this point rather than assuming it is mandatory.
2. Packaging, signed artifacts, SBOMs, upgrades, backup/restore, and external
   security testing.
3. AWS, Azure, Kubernetes, conflict/freshness visibility, and larger scale
   gates leading toward Topo Graph.

`ROADMAP.md`'s M2 line also lists a "rate-limited allowlisted sweep" and
network topology protocols (LLDP/CDP) — real, scoped follow-ups deliberately
left out of both SNMP/VMware slices above rather than silently bundled in;
pick them up alongside whichever of the above priorities needs them first,
not as an assumed default next milestone.

## Definition of milestone completion

A milestone is complete only when implementation, security boundaries, failure
behavior, scale/identity acceptance tests, user documentation, roadmap status,
and the current handoff are updated together and merged through a green PR.

## New-chat startup

Open Codex in the repository root and use:

> Read AGENTS.md, docs/project-plan.md, ROADMAP.md, README.md, and SECURITY.md.
> Inspect git status and recent history. Continue the current Nischoy Topo
> milestone from the handoff without relying on prior chat history. Before
> implementing, verify that the stated current milestone still matches merged
> code and present a concise execution plan. Preserve all architectural and
> security decisions, and update the handoff when the milestone changes.
