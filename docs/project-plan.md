# Nischoy Topo project plan and handoff

This document is the durable source of truth for project direction and
cross-chat continuity. `ROADMAP.md` is the shorter public release roadmap;
`AGENTS.md` contains standing execution rules.

## Current handoff

- **Updated:** 2026-08-18
- **Public repository:** <https://github.com/Nischoy-ai/topo>
- **Latest completed slice:** `k8s:[<namespace>/]<secret-name>#<field>` credential-reference provider backed by a Kubernetes API client (`pkg/credentialref/kubernetes`), wired into the existing resolver alongside `env:`/`file:`/`vault:`. Authenticates in-cluster using the pod's own projected service account token (re-read on every request, so kubelet-rotated tokens need no renewal logic) and the cluster CA; `TOPO_KUBERNETES_API_SERVER`/`TOPO_KUBERNETES_TOKEN_FILE`/`TOPO_KUBERNETES_CA_FILE`/`TOPO_KUBERNETES_NAMESPACE` override the defaults for out-of-cluster or non-default deployments. Least-privilege scoping is delegated entirely to Kubernetes RBAC on the calling service account (documented `Role`/`RoleBinding` example scoped to named Secrets, `get` only). **This completes the credential-references milestone**: `env:`, `file:`, `vault:`, and `k8s:` are all implemented and adopted by the controller API key, SSH password/private key, and WinRM password paths, with no CLI changes required per provider.
- **Merged pull request:** <https://github.com/Nischoy-ai/topo/pull/9> (Vault adapter; this Kubernetes slice pushed to `claude/next-repo-work-g2ihni`, PR pending)
- **Merged commit:** `5b23c210` (merge of the Vault adapter into `main`; update after this slice merges)
- **Current milestone (next):** Outbound-only Topo Agent MVP for Linux and Windows with encrypted offline buffering, per the follow-on order below.
- **Verified in this slice:** Under Go 1.23, `gofmt -l` (clean), `go vet ./...`, the exact CI race/coverage command `go test -race -coverprofile=coverage.out ./...`, and `go build -trimpath ./cmd/topo` all pass. Kubernetes client tests (`pkg/credentialref/kubernetes`) use `httptest` to cover successful reads (default and explicit namespace), missing secret/field as `ErrUnavailable`, permission-denied errors that do not leak secret bytes, invalid base64 rejection, response-size bounds, context cancellation, token re-read/rotation, and environment-driven configuration (in-cluster defaults vs. explicit overrides, including the CA-file default only applying in true in-cluster mode). `credentialref` tests cover the new `k8s:` reference end-to-end via a mock server, both default and explicit namespace forms, and confirm missing `KUBERNETES_SERVICE_HOST` fails clearly.
- **Explicitly deferred evidence:** Sanitized captures and regression fixtures from Windows Server 2022 and one other supported release. Do not fabricate this evidence from Topo Lab; obtain it from controlled real hosts and review it for hostnames, addresses, domain data, serials, UUIDs, account names, and other sensitive values before commit. This gate remains open before claiming real-host Windows compatibility or production readiness. Per-user uninstall hives, Kerberos/SPNEGO, and certificate authentication also remain follow-ups.
- **Explicit deferral:** Do not make PostgreSQL the next milestone. Automatic background Vault token renewal for long-running processes, and support for leased dynamic-secrets engines beyond token renewal, remain deferred follow-ups rather than part of any current milestone.

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

## Follow-on order

With the credential-provider milestone complete, pursue these slices in
order unless evidence from an enterprise pilot changes the priority:

1. Outbound-only Topo Agent MVP for Linux and Windows with encrypted buffering.
2. End-to-end ServiceNow IRE duplicate-CI and reconciliation validation.
3. Collector enrollment, outbound mTLS, rotation/revocation, heartbeat, and
   job delivery.
4. Persistent observation/audit storage and scheduling; evaluate PostgreSQL at
   this point rather than assuming it is mandatory.
5. SNMPv3/network topology and VMware vCenter discovery.
6. Packaging, signed artifacts, SBOMs, upgrades, backup/restore, and external
   security testing.
7. AWS, Azure, Kubernetes, conflict/freshness visibility, and larger scale
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
