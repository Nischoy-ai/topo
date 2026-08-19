# Nischoy Topo project plan and handoff

This document is the durable source of truth for project direction and
cross-chat continuity. `ROADMAP.md` is the shorter public release roadmap;
`AGENTS.md` contains standing execution rules.

## Current handoff

- **Updated:** 2026-08-19
- **Public repository:** <https://github.com/Nischoy-ai/topo>
- **Latest completed slice:** Job delivery (slice 5, the final slice, of the "collector enrollment, outbound mTLS, rotation, heartbeats, and jobs" milestone). `POST /v1/jobs` lets an operator queue one job (today, exactly one type: `discover`, an out-of-schedule discovery pass — the only real capability `topo agent run` has) for a specific collector. `GET /v1/jobs` returns and marks-dispatched every job queued for the polling collector, at most once (no redelivery if the collector crashes before reporting a result — deliberate, matching this project's preference for simple, explicit behavior over queue-redelivery semantics). `POST /v1/jobs/{id}/result` reports the outcome; `GET /v1/jobs/{id}` is a read-only status lookup with no dispatch side effect. Polling and reporting are identity-bound exactly like `POST /v1/rotate` and `POST /v1/heartbeats`: a verified mTLS peer certificate's subject overrides whatever `collector_id` the caller claims otherwise, now via a shared `collectorIdentity` helper that also replaced the heartbeat handler's previously-inlined copy of the same logic. `topo agent run` polls for jobs on the same `-heartbeat-interval` cadence it already uses for liveness heartbeats — no new flag — since both are cheap, frequent check-ins distinct from the heavier discovery `-interval`. A `discover` job reuses the existing `discoverAndSend` helper directly (now returning an error, so a job's reported outcome reflects whether discovery itself succeeded, not whether the resulting observation was delivered synchronously — delivery keeps its own independent spool-retry path regardless of how discovery was triggered). Always registered, like heartbeats: no CA or opt-in flag required. `internal/agent.Sender` gained `PollJobs`/`ReportJobResult` alongside a shared `get`/`doRaw` helper pairing with the existing `post`, so GET and POST requests share the same auth-header and status-classification logic. Verified with unit tests (including one proving a job poll/result over mTLS is bound to the verified peer certificate's identity even when the caller claims a different `collector_id`, and one proving `agent.Run` with a long `Interval` and a short `HeartbeatInterval` still causes a queued `discover` job to be polled, executed, and reported `succeeded`, isolating job execution from the discovery ticker entirely) and a manual run of the real CLI binaries end to end (agent with `-interval 100h` picks up and executes a `curl`-submitted job purely through `-heartbeat-interval`'s poll).
- **Open pull request:** <https://github.com/Nischoy-ai/topo/pull/19> (documents the ServiceNow real-instance validation noted above; no code changes)
- **Merged pull request:** job delivery in <https://github.com/Nischoy-ai/topo/pull/18> (completes the enrollment/mTLS/rotation/heartbeats/jobs milestone); heartbeats in <https://github.com/Nischoy-ai/topo/pull/17>; certificate rotation in <https://github.com/Nischoy-ai/topo/pull/16>; collector enrollment in <https://github.com/Nischoy-ai/topo/pull/14>; outbound mTLS in <https://github.com/Nischoy-ai/topo/pull/15>; ServiceNow IRE duplicate-CI validation in <https://github.com/Nischoy-ai/topo/pull/13>.
- **Merged commit:** `c9a31bf` (merge of job delivery into `main`); PR #19 not yet merged.
- **Also verified this session, outside any slice/PR:** given access to a real ServiceNow developer instance, ServiceNow's own IRE reconciliation behavior was confirmed for real for the first time — submitting a `cmdb_ci_computer` item once creates a CI, resubmitting the identical `sys_object_source_info` updates that same CI (`operation: UPDATE` against the original `sysId`) rather than duplicating it. A real, previously-unknown requirement was found in the process: `cmdb_ci`'s `discovery_source` field is a registered choice list, and an unregistered value is rejected outright. See [`docs/servicenow.md`](servicenow.md#verified-against-a-real-instance) for full detail and exactly what remains unverified (other CI classes, relationships, the response schema).
- **Current milestone:** Collector enrollment, outbound mTLS, certificate rotation, heartbeats, and job delivery — **complete** as of this slice (all five slices implemented; see "Current milestone: collector enrollment, outbound mTLS, rotation, heartbeats, and jobs" below for the full spec and every slice's acceptance gates). The next milestone has not yet been chosen; see "Follow-on order" below for candidates.
- **Verified in this slice:** Under Go 1.23, `gofmt -l` (clean), `go vet ./...`, the exact CI race/coverage command `go test -race -coverprofile=coverage.out ./...`, and `go build -trimpath ./cmd/topo` all pass on Linux; `GOOS=windows GOARCH=amd64 go vet ./...` and `go build` also pass. New tests cover: a job queued for one collector not being returned by a different collector's poll; a job being returned by exactly one poll, never redelivered; a poll or result report over mTLS claiming a different `collector_id` still being bound to the verified peer certificate's real identity; result-reporting rejections (wrong collector, undispatched job, double report, unknown job ID); `POST /v1/jobs` rejecting an unsupported job type at creation; `agent.Run` end to end against a real `controller.Server` over `httptest`, proving a queued job is polled/executed/reported purely via the independent `HeartbeatInterval` ticker while `Interval` never fires within the test; a failed discovery pass being reported as a `failed` job with a non-empty error, not silently dropped; and `Sender.PollJobs`/`ReportJobResult` unit tests. Also manually verified the full CLI flow end to end (controller → agent with `-interval 100h -heartbeat-interval 2s` → `curl`-submitted `discover` job → `GET /v1/jobs/{id}` shows `succeeded` and the observation count increases), and confirmed `POST /v1/jobs` with an unsupported type is rejected with 400 rather than silently accepted.
- **Explicitly deferred evidence:** Sanitized captures and regression fixtures from Windows Server 2022 and one other supported release; real-Windows verification of the Topo Agent's Windows service wrapper; and validation of ServiceNow's own IRE identification/reconciliation behavior for the CI classes and relationship payloads not yet exercised against the real instance now available (only `cmdb_ci_computer`, single-item, no relationships has been tested — see `docs/servicenow.md`), plus the IRE response schema itself, which remains unparsed and proprietary. Do not fabricate any of these from Topo Lab or from guessed schemas; obtain them from the real controlled system — a ServiceNow developer instance is now available for exactly this. These gates remain open before claiming real-host Windows compatibility, full ServiceNow production readiness, or general production readiness.
- **Explicit deferral:** Do not make PostgreSQL the next milestone. Automatic background Vault token renewal for long-running processes, and support for leased dynamic-secrets engines beyond token renewal, remain deferred follow-ups. One agent instance per host (fixed systemd unit / Windows service name) is an intentional Agent MVP limitation, not tracked as a gap. Do not attempt to parse or assume ServiceNow's IRE response schema without a real instance to verify it against. Certificate revocation is explicitly out of scope for the enrollment, outbound-mTLS, and rotation slices; a compromised collector key is contained by the bounded 90-day certificate TTL (or less, if rotated more often) only. Heartbeat and job state are in-memory only, like the enrollment token store; neither survives a controller restart. Job delivery has deliberately no listing/browsing endpoint (status lookup is by ID only), no cancellation, and no redelivery of an already-dispatched job — an operator resubmits if a job's outcome still matters after a collector never reports back. A real ServiceNow developer instance is now available (see above); use it to close the remaining gaps rather than guessing at ServiceNow's behavior, but do not claim broader ServiceNow coverage than what has actually been exercised against it.

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
`sys_choice` before any write succeeds. Full detail, scope, and what
remains unverified (other CI classes, `IRERelation` payloads, the IRE
response schema, multi-item batches) is in
[`docs/servicenow.md`](servicenow.md#verified-against-a-real-instance).
This did not reopen the milestone or its slices above, which remain
accurate as a record of what shipped in PR #13; it is additional evidence
obtained afterward, once instance access became available.

## Current milestone: collector enrollment, outbound mTLS, rotation, heartbeats, and jobs

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

## Follow-on order

With the credential-provider, Topo Agent MVP, and ServiceNow IRE
duplicate-CI validation milestones complete, pursue these slices in order
unless evidence from an enterprise pilot changes the priority:

1. Collector enrollment, outbound mTLS, rotation/revocation, heartbeat, and
   job delivery.
2. Persistent observation/audit storage and scheduling; evaluate PostgreSQL at
   this point rather than assuming it is mandatory.
3. SNMPv3/network topology and VMware vCenter discovery.
4. Packaging, signed artifacts, SBOMs, upgrades, backup/restore, and external
   security testing.
5. AWS, Azure, Kubernetes, conflict/freshness visibility, and larger scale
   gates leading toward Topo Graph.

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
