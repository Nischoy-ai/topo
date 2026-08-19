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

The ServiceNow IRE duplicate-CI validation milestone is complete for the
half of it Topo controls: `mapPayload` deduplicates by `source_native_key`
(and relationships by `(type, from, to)`) within a batch, and is validated
to emit an identical `(source_native_key, className)` set across
independently repeated Topo Lab discovery scans — the precondition for
ServiceNow's own IRE to reconcile rather than duplicate a CI. See
`docs/servicenow.md`. ServiceNow's own identification/reconciliation
behavior and IRE response schema remain unverified: there is no ServiceNow
instance available to this project, so do not represent that half as
validated.

The current milestone is collector enrollment, outbound mTLS, certificate
rotation, heartbeats, and job delivery — five distinct capabilities,
deliberately staged as separate slices:

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
   enforcement happens in `auth()` instead). `topo agent run
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
5. Job delivery, necessarily collector-initiated polling since the agent
   remains outbound-only. This is the next, final slice of the milestone.

The complete scope and acceptance gates for slice 1, and the plan for
slices 2-5, are in `docs/project-plan.md`.

The Windows implementation and simulated scale gates are complete. Sanitized
fixtures from Windows Server 2022 and one other supported release are
explicitly deferred, not represented as completed, and remain required before
claiming real-host compatibility or production readiness.

The complete scope, acceptance gates, and follow-on order are in
`docs/project-plan.md`.

## Engineering workflow

- Use Go 1.23 compatibility until the roadmap explicitly changes it.
- Prefer standard-library components and narrowly scoped dependencies.
- Run `gofmt -w` on changed Go files, `go vet ./...`, `go test -race ./...`,
  and `go build -trimpath ./cmd/topo` before publishing. Files behind a
  `windows` build tag (Windows service integration) also need
  `GOOS=windows GOARCH=amd64 go vet ./...` and `go build`, matching the CI
  cross-compile check; there is no way to execute them on Linux CI.
- New protocol plugins need parser tests, configuration validation, connection
  and timeout tests, arbitrary-operation rejection tests, fault isolation, and
  repeat-scan identity tests.
- Work on `agent/<description>` branches and use pull requests. Never rewrite
  shared history or discard unrelated work.
- At milestone completion, update `README.md`, `ROADMAP.md`, relevant protocol
  docs, and the current handoff in `docs/project-plan.md` in the same PR.
