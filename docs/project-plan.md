# Nischoy Topo project plan and handoff

This document is the durable source of truth for project direction and
cross-chat continuity. `ROADMAP.md` is the shorter public release roadmap;
`AGENTS.md` contains standing execution rules.

## Current handoff

- **Updated:** 2026-08-18
- **Public repository:** <https://github.com/Nischoy-ai/topo>
- **Latest completed slice:** ServiceNow IRE duplicate-CI validation, for the half of it Topo controls. `mapPayload` (`pkg/publisher/servicenow/servicenow.go`) now deduplicates by `source_native_key` (last observation wins) and by relationship `(type, from, to)` even across multiple input envelopes in one batch — previously, an asset appearing twice in the input produced two IRE items with the same `source_native_key`. `TestMapPayloadIsIdempotentAcrossRepeatedLabScans` runs a Topo Lab estate through discovery twice (the same two-scan pattern the SSH/WinRM acceptance gates use) and asserts the mapped `(source_native_key, className)` set is identical both times; `TestPublishBatchSendsIdempotentRequestsAcrossRepeatedLabScans` asserts the same at the wire level against a fake IRE endpoint. `PublishBatch` also now captures the (bounded) response body in `Diagnostics` for operator review. **This is deliberately scoped to what Topo itself controls**: there is no ServiceNow instance available to this project, so ServiceNow's own identification/reconciliation behavior and its IRE response schema are neither exercised nor assumed — see `docs/servicenow.md` for the explicit boundary. This closes the ServiceNow IRE duplicate-CI validation milestone within that scope.
- **Merged pull request:** <https://github.com/Nischoy-ai/topo/pull/12> (systemd/Windows-service wrapping, the prior slice); this ServiceNow slice is <https://github.com/Nischoy-ai/topo/pull/13>
- **Merged commit:** `a628a8a` (merge of the systemd/Windows-service wrapping into `main`; update after this slice merges)
- **Current milestone (next):** Collector enrollment, outbound mTLS, certificate rotation, heartbeats, and job delivery, per the follow-on order below.
- **Verified in this slice:** Under Go 1.23, `gofmt -l` (clean), `go vet ./...`, the exact CI race/coverage command `go test -race -coverprofile=coverage.out ./...`, and `go build -trimpath ./cmd/topo` all pass on Linux; `GOOS=windows GOARCH=amd64 go vet ./...` and `go build` also pass. `pkg/publisher/servicenow` coverage rose from 39.7% to 78.4%. New tests cover: within-batch dedup for both items (last-observation-wins) and relationships; response-body diagnostics capture; cross-scan idempotency of `mapPayload` output against a real two-scan Topo Lab estate; and cross-scan idempotency of the actual `PublishBatch` HTTP requests (method, path, auth header, and source keys) against a fake IRE endpoint (`httptest.NewTLSServer`, since `Validate` requires HTTPS).
- **Explicitly deferred evidence:** Sanitized captures and regression fixtures from Windows Server 2022 and one other supported release; real-Windows verification of the Topo Agent's Windows service wrapper; and now also validation of ServiceNow's own IRE identification/reconciliation behavior and response schema against a real or sandboxed instance. Do not fabricate any of these from Topo Lab or from guessed schemas; obtain them from the real controlled system. These gates remain open before claiming real-host Windows compatibility, ServiceNow production readiness, or general production readiness.
- **Explicit deferral:** Do not make PostgreSQL the next milestone. Automatic background Vault token renewal for long-running processes, and support for leased dynamic-secrets engines beyond token renewal, remain deferred follow-ups. One agent instance per host (fixed systemd unit / Windows service name) is an intentional Agent MVP limitation, not tracked as a gap. Do not attempt to parse or assume ServiceNow's IRE response schema without a real instance to verify it against.

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
