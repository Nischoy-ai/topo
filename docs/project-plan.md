# Nischoy Topo project plan and handoff

This document is the durable source of truth for project direction and
cross-chat continuity. `ROADMAP.md` is the shorter public release roadmap;
`AGENTS.md` contains standing execution rules.

## Current handoff

- **Updated:** 2026-08-28
- **Public repository:** <https://github.com/Nischoy-ai/topo>
- **Milestone status:** M2.5 (release readiness and security hardening) is
  complete — see "Completion status" under "Completed milestone: M2.5" below.
  M3 (hybrid release candidate) is current; slice 1 (Kubernetes node/pod
  discovery) is implemented and merged
  (<https://github.com/Nischoy-ai/topo/pull/41>); slice 2 (AWS
  Organizations account-structure discovery) is implemented and merged
  (<https://github.com/Nischoy-ai/topo/pull/42>); slice 3 (Azure tenant
  subscription-structure discovery) is implemented, completing all three
  protocol integrations `ROADMAP.md`'s M3 line names; slice 4 (source
  precedence and asset conflict/freshness visibility) is implemented and
  merged in <https://github.com/Nischoy-ai/topo/pull/46>; slice 5
  (ServiceNow-controlled Topo Relay MVP), prioritized by the enterprise-pilot
  requirement, is merged in <https://github.com/Nischoy-ai/topo/pull/47> and
  retained as an experimental scoped-app predecessor, not the required final
  architecture. Slice 6 (native ServiceNow ECC-compatible MID transport) is
  implemented and merged in <https://github.com/Nischoy-ai/topo/pull/48>:
  `topo mid run`, strict direct-SOAP ECC polling, durable claim/restart
  handling, local identity locking, Heartbeat-only dispatch, and correlated
  denial of every other topic. Real
  `ecc_agent` bootstrap/validation, exact stock Heartbeat XML, and Up/Down
  evidence remain unverified. See "Current milestone: M3" below. The most recent merged
  M2.5 slice fixed
  `TSR-2026-004`, the first finding from an actual independent reviewer
  (Grok/xAI) rather than maintainer self-audit: publisher HTTPS clients
  (webhook, ServiceNow) and the related agent-sender/enrollment-client
  residual now reject URL userinfo (plus, for the base-address forms,
  path/query/fragment) and refuse to follow redirects, so a bearer
  credential or enrollment token cannot be replayed against an unconfigured
  destination (renumbered from the reviewer's own `TSR-2026-001` label to
  avoid colliding with this project's already-assigned `TSR-2026-001`; see
  `docs/security-review.md#independent-review`), merged in
  <https://github.com/Nischoy-ai/topo/pull/39>. Before that: the `promote.yml`
  `workflow_dispatch` version-input interpolation fix (`TSR-2026-003`) in
  <https://github.com/Nischoy-ai/topo/pull/38>; owner-only live SQLite
  creation plus private backup staging (`TSR-2026-002`/`TSR-2026-009`) in
  <https://github.com/Nischoy-ai/topo/pull/37>;
  collector-scoped enrollment tokens (`TSR-2026-001`) in
  <https://github.com/Nischoy-ai/topo/pull/35>; external-security-review
  preparation and pre-review remediation in
  <https://github.com/Nischoy-ai/topo/pull/33>.
- **Verified in the current slice (M3 slice 6, native ServiceNow
  ECC-compatible MID transport):** `topo mid run` uses only the fixed native
  `/ecc_queue.do?SOAP` direct service, rejects non-HTTPS/ambiguous instance
  URLs and redirects, authenticates a dedicated MID user with the shared
  credential-reference contract, and polls a bounded set of
  `output`/`ready` records addressed exactly to
  `mid.server.<configured-name>`. A synchronized owner-only append journal
  precedes the `processing` transition; an OS lock rejects a second local
  process for the same identity; restart recovery queries by `response_to`
  before insert so a lost SOAP response cannot duplicate the correlated
  `input`/`ready` result. XML body/depth/token, record, field, payload,
  parameter, timeout, and result bounds are enforced on Unix and Windows.
  Only `Heartbeat` is recognized. `Command`, `SSHCommand`, PowerShell,
  JavaScript, Groovy, and every unknown topic receive a bounded visible
  `topo_unsupported_topic` result and no target-bearing operation is executed.
  The deterministic `internal/mid/eccsim` SOAP/ECC fixture proves the transport,
  state, crash, correlation, authentication, redirect, bound, fault, and
  denial behavior. Exact Go 1.25.13 focused and full race tests, repository
  vet, Linux build, Windows amd64 vet/build, and the pinned
  `scripts/security-review-checks.sh` gate pass; `govulncheck` reports zero
  reachable vulnerabilities. The simulator does not prove ServiceNow sensor
  behavior. Real
  `ecc_agent` bootstrap/list appearance, validation, exact stock Heartbeat XML,
  Up/Down transitions, and Discovery Status remain explicitly unverified
  pending signed-in developer-instance/reference-MID access. See
  `docs/servicenow-mid.md`.
- **Verified development Homebrew pilot (not production-channel evidence):**
  the separate public
  <https://github.com/Nischoy-ai/homebrew-topo-dev> tap and its
  `v0.0.0-mid.1` prerelease package merged Topo commit
  `32733488a704114e3a805c6313aae4257cade7d4`. Exact Go 1.25.13 built all six
  raw archives twice from separate source paths with byte-identical output;
  every public asset was downloaded again and verified against the published
  `SHA256SUMS`. `topo-mid.rb` passes Homebrew style, strict online new-formula
  audit, a real Apple Silicon install, and `brew test`; the installed
  `/opt/homebrew/bin/topo` reports `v0.0.0-mid.1` and exposes `topo mid run`.
  The macOS binary has only Go's ad-hoc linker signature—no Developer ID team
  identity or notarization—and the mutable prerelease has no Sigstore bundle,
  GitHub build provenance, SBOM, or protected promotion evidence. This pilot
  neither provisions the deferred official `Nischoy-ai/homebrew-tap` nor
  satisfies the real-beta/N-1 stable package-channel gate.
- **Verified in the previous/predecessor slice (M3 slice 5,
  ServiceNow-controlled Topo Relay MVP):** `topo relay run` polls two fixed
  custom Scripted REST resources, executes only locally configured `local` or
  `ssh-linux` profiles, and retains IRE/result delivery in an encrypted spool.
  Its simulator-backed end-to-end evidence remains valid, but the scoped app,
  custom tables, and scripts are not the required final architecture or an
  installation prerequisite. They are retained as experimental predecessor
  artifacts; see `docs/servicenow-relay.md`.
- **Verified in the previous slice (M3 slice 4, source precedence and asset
  conflict/freshness visibility):** `topo serve -source-precedence` accepts
  an ordered, bounded list of discovery plugin names. Both repository
  backends retain one latest asset claim per site/collector/plugin source;
  shared resolution picks an explicit-priority winner, then freshness and a
  stable source hash break ties without depending on arrival or map order.
  `GET /v1/assets` adds the winning and contributing sources, precedence
  ranks, first/latest observation identifiers and timestamps, and
  field-level conflicts while preserving its existing top-level resolved
  asset fields. An out-of-order observation cannot roll one source backward.
  SQLite schema version 5 persists claims and reconstructs them from retained
  observations inside the all-pending-migrations transaction; conformance,
  restart, backup/restore, and every-supported-schema tests cover the change.
  Relationship precedence, cross-ID correlation, omission-based retirement,
  stale-after policy, and the 1K/10K/100K scale gate remain deliberate
  follow-ups. See `docs/source-resolution.md`.
- **Verified in the previous slice (M3 slice 3, Azure tenant
  subscription-structure discovery):** `pkg/discovery/azure` authenticates
  via the Azure AD OAuth2 client-credentials grant, then calls a single
  recursive `Get` on the tenant's root management group
  (`$expand=children&$recurse=true`) via `azure-sdk-for-go`'s
  `armmanagementgroups`/`armsubscriptions` clients — unlike AWS's
  per-parent listing walk, Azure returns the whole hierarchy in one call.
  Asset identity is each object's full ARM resource path, never a bare
  short name or display name: Azure's automatically created "Tenant Root
  Group" reuses the tenant's own GUID as its short name, so a bare-GUID
  identity would collide the Tenant and root ManagementGroup assets (found
  empirically while testing this slice — the first implementation produced
  a self-referential relationship before the ARM-path fix). All three
  kinds (Tenant, ManagementGroup, Subscription) map to
  `model.AssetCloudResource` with `Attributes["kind"]` distinguishing
  them, and a single `member_of` relationship forms the hierarchy, reusing
  AWS's relationship type. Azure has no official local ARM simulator;
  Topo Lab hand-rolls an ARM fixture (`pkg/lab/azure_server.go`) serving
  the tenant's OpenID Connect discovery document, the OAuth2 token
  endpoint, and the ARM Get/List responses — its wire field names were
  confirmed by reading `azure-sdk-for-go`'s own `models_serde.go`, then
  verified empirically by capturing the real client's actual request
  sequence against a logging test double. Unlike AWS's SigV4, Azure's ARM
  API has no per-request signing, so verifying the client
  ID/secret at the token endpoint and the bearer token on every ARM call
  by equality is the real protocol, not a simplification. One real
  constraint discovered while building the fixture: `azidentity`
  unconditionally refuses a non-HTTPS authority host, so Topo Lab's Azure
  fixture — unlike Kubernetes's and AWS's plain-HTTP loopback fixtures —
  always serves HTTPS with a freshly generated self-signed certificate.
  The two-scan idempotency acceptance test runs the full
  `examples/lab/clean-500.json` scenario as 500 simulated subscriptions
  nested two levels under two management groups: 506 total assets, 505
  `member_of` relationships — matching the AWS slice's numbers by
  deliberately symmetric fixture design — zero errors, stable and
  duplicate-free across a repeated scan and a `store.Memory` save,
  verified end-to-end via the CLI (`topo lab azure-serve`,
  `topo discover azure -lab`), not only in the test suite. `go test -race
  ./...` passes; `gofmt`/`go vet` clean. Real-tenant verification beyond
  the hand-rolled fixture has not been performed — the same posture as
  SNMP `authPriv`, real VMware/vCenter, and real Kubernetes clusters (AWS
  Organizations has since gained partial real-account verification — see
  the AWS slice bullet below). See `docs/azure.md`.
- **Slice before that (M3 slice 2, AWS Organizations account-structure
  discovery, merged):** `pkg/discovery/aws` calls
  `DescribeOrganization`/`ListRoots` then recursively walks
  `ListOrganizationalUnitsForParent`/`ListAccountsForParent` via
  `aws-sdk-go-v2`'s Organizations client. Asset identity is each object's
  AWS-assigned ID, never its mutable name; all four kinds (Organization,
  Root, OrganizationalUnit, Account) map to `model.AssetCloudResource`
  with `Attributes["kind"]` distinguishing them, and a single `member_of`
  relationship forms the containment hierarchy. Topo Lab hand-rolls an
  AWS-JSON-1.1 fixture (`pkg/lab/aws_organizations_server.go`) with real
  SigV4 signature verification via the SDK's own `v4.Signer`. The two-scan
  idempotency acceptance test runs the full `examples/lab/clean-500.json`
  scenario as 500 simulated accounts nested two levels under two OUs: 506
  total assets, 505 `member_of` relationships, zero errors. Since merging,
  it has also been verified against a real, live AWS account (2026-08-25):
  real connectivity, real SigV4 auth, real multi-account enumeration, and
  the documented four-action least-privilege IAM policy confirmed
  empirically sufficient — OU nesting, permission-denied handling, and
  delegated-admin/STS credentials remain verified only against the Lab
  fixture. See `docs/aws.md`.
- **Milestone slice before that (M3 slice 1, Kubernetes node/pod
  discovery, merged):** `pkg/discovery/kubernetes` lists `v1.Node` and
  `v1.Pod` objects cluster-wide via `k8s.io/client-go` (pinned `v0.35.8`,
  the latest release still compatible with exact Go 1.25.13 — `v0.36.x`
  requires Go 1.26). Asset identity is each object's Kubernetes UID, never
  its name or an IP address; both map to `model.AssetKubernetesObject`
  with `Attributes["kind"]` distinguishing `Node`/`Pod`. A
  `pod_runs_on_node` relationship resolves from `pod.Spec.NodeName`,
  skipped for unscheduled pods. Topo Lab hand-rolls a Kubernetes API
  fixture (`pkg/lab/kubernetes_server.go`, real `k8s.io/api` JSON over
  real HTTP) the same way it did for SNMP. The two-scan idempotency
  acceptance test runs the full `examples/lab/clean-500.json` scenario:
  500 node assets, 500 pod assets, 500 `pod_runs_on_node` relationships,
  zero errors. See `docs/kubernetes.md`.
- **Milestone before that:** SNMP and VMware discovery (`ROADMAP.md`
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
- **Open pull request:** development Homebrew evidence documentation in
  <https://github.com/Nischoy-ai/topo/pull/49>.
- **Merged pull requests:** SNMP discovery in
  <https://github.com/Nischoy-ai/topo/pull/21>; VMware discovery in
  <https://github.com/Nischoy-ai/topo/pull/22>; persistent storage
  milestone slice 1 (SQLite-backed `store.Repository`) in
  <https://github.com/Nischoy-ai/topo/pull/23>; slice 2 (hash-chained
  audit log) in <https://github.com/Nischoy-ai/topo/pull/24>; slice 3
  (server-side recurring discovery scheduling) in
  <https://github.com/Nischoy-ai/topo/pull/25>; M2.5 slice 1 (authorization
  boundary) in <https://github.com/Nischoy-ai/topo/pull/26>; M2.5 slice 2
  (certificate revocation and compromise recovery) in
  <https://github.com/Nischoy-ai/topo/pull/27>; M2.5 slice 3 (backup/restore
  and schema upgrade/rollback safety) in
  <https://github.com/Nischoy-ai/topo/pull/28>; M2.5 slice 4 (reproducible
  release artifacts and supply-chain evidence) in
  <https://github.com/Nischoy-ai/topo/pull/29>; M2.5 slice 5 (native package
  artifacts) in <https://github.com/Nischoy-ai/topo/pull/30>; M2.5 slice 6
  (package-manager distribution) in
  <https://github.com/Nischoy-ai/topo/pull/31>; external-review preparation in
  <https://github.com/Nischoy-ai/topo/pull/33>; collector-scoped enrollment-
  token remediation in <https://github.com/Nischoy-ai/topo/pull/35>; SQLite
  live-file and backup-staging remediation in
  <https://github.com/Nischoy-ai/topo/pull/37>; `promote.yml`
  workflow-interpolation remediation in
  <https://github.com/Nischoy-ai/topo/pull/38>; publisher/agent/enrollment
  redirect and userinfo remediation (`TSR-2026-004`) in
  <https://github.com/Nischoy-ai/topo/pull/39>; M3 Kubernetes discovery in
  <https://github.com/Nischoy-ai/topo/pull/41>; AWS Organizations discovery
  in <https://github.com/Nischoy-ai/topo/pull/42>; Azure tenant discovery in
  <https://github.com/Nischoy-ai/topo/pull/43>; and AWS/Azure live-validation
  handoff updates in <https://github.com/Nischoy-ai/topo/pull/44> and
  <https://github.com/Nischoy-ai/topo/pull/45>; source precedence/conflict
  visibility in <https://github.com/Nischoy-ai/topo/pull/46>; and the scoped-app
  ServiceNow Relay predecessor in <https://github.com/Nischoy-ai/topo/pull/47>;
  and native ServiceNow ECC-compatible MID transport in
  <https://github.com/Nischoy-ai/topo/pull/48>.
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
- **Completed milestone:** M2.5 — release readiness and security hardening.
  All seven slices are merged, including remediation of every finding raised
  so far by maintainer self-audit and by the first independent reviewer
  (Grok/xAI): `TSR-2026-001`/`002`/`003`/`004`/`009`. No critical or high
  findings were reported, and several documented security invariants were
  independently confirmed rather than accepted from documentation alone. The
  reviewer's independent retest of the exact remediation commits, and real
  beta/N-1 stable package-channel promotion evidence (deferred until the
  user authorizes external repository and production signing-key
  provisioning), remain open as tracked follow-up — neither blocks M3, and
  neither is a production-readiness claim. See "Completion status" under
  "Completed milestone: M2.5" below and `docs/security-review.md#independent-
  review`.
- **Current milestone:** M3 — hybrid release candidate. Slice 6, native
  ServiceNow ECC-compatible MID transport and real-instance evidence, is the
  current enterprise-pilot priority. See "Current milestone: M3" below.
- **Verified in the current remediation slice (`TSR-2026-004`, independent review):** an independent reviewer
  (Grok/xAI) found `TSR-2026-004` (reported as `TSR-2026-001` in the
  reviewer's own numbering; renumbered here — see "Independent review" in
  `docs/security-review.md` — to avoid colliding with this project's
  pre-existing `TSR-2026-001`): `pkg/publisher/webhook/webhook.go` and
  `pkg/publisher/servicenow/servicenow.go` validated only that a configured
  destination was an absolute HTTPS URL, not rejecting embedded userinfo,
  and their default `http.Client` had no `CheckRedirect` override, so a
  redirect response would be followed with the configured bearer token
  attached — a weaker boundary than `pkg/credentialref/vault`/`kubernetes`,
  which already reject userinfo and refuse redirects. `Validate` in both
  publishers now rejects userinfo (ServiceNow's `InstanceURL`, a base
  address the code appends a fixed API path to, also rejects a non-root
  path/query/fragment, mirroring `vault`'s `validateHTTPSAddress`), and both
  publishers' default HTTP client now refuses redirects
  (`CheckRedirect` → `http.ErrUseLastResponse`). The reviewer's related,
  lower-risk residual is fixed in the same change:
  `internal/agent/sender.go`'s `NewSender` and
  `internal/enrollment/client.go`'s `validControllerURL` (shared by `Enroll`
  and `Rotate`) apply the same userinfo/path/query/fragment rejection, and
  all three of those HTTP clients now refuse redirects too. Regression tests
  cover userinfo/path/query/fragment rejection and prove — with a real
  `httptest` redirect, not a mock — that the redirect target is never
  contacted and never receives the credential. The complete pinned
  `scripts/security-review-checks.sh` gate passes under exact Go 1.25.13,
  including the full race suite, Linux build, and Windows amd64 vet/build
  (`govulncheck` could not reach the vulnerability database from this
  sandbox's network policy — an environment restriction, not a code effect;
  it runs normally in CI). This is fixed and ready for the reviewer's
  independent retest; because this finding originated from an independent
  review rather than maintainer self-audit, only that retest — not a
  maintainer or coding-agent assertion — can mark it `Verified`, and the
  M2.5 gate remains open until it is.
- **Verified in the previous slice (`TSR-2026-003`, workflow interpolation):** a 2026-08-24 maintainer
  self-audit of the release/distribution automation found `TSR-2026-003`:
  four steps in `.github/workflows/promote.yml` (one Homebrew-formula test
  step and three WinGet-manifest validation/exercise steps) interpolated the
  `workflow_dispatch` `inputs.version` value directly into a `run:`
  shell/PowerShell script body via raw `${{ }}` substitution rather than
  `env:`, a GitHub Actions script-injection primitive into a job that later
  imports release-signing secrets. At discovery an earlier same-run step
  already constrained `inputs.version` to a safe semver pattern before those
  four steps ran, so this was not independently exploitable; it is filed and
  fixed as defense-in-depth since that constraint lives in a different job
  step than its use. All four steps now route the value through `env:` and
  reference `$VERSION`/`$env:VERSION`. A new
  `scripts/check-workflow-interpolation.sh` check scans every workflow's
  `run:` steps for raw `inputs.`/`github.event.` interpolation and runs in
  ordinary CI (`.github/workflows/ci.yml`) on every pull request, not only in
  `scripts/security-review-checks.sh`. The complete pinned
  `scripts/security-review-checks.sh` gate passes under exact Go 1.25.13,
  including zero reachable `govulncheck` findings, the full race/coverage
  suite, Linux vet/build, and Windows amd64 vet/build. This remediates
  `TSR-2026-003` for independent retest; it is not independent closure, and
  the remaining findings keep the M2.5 gate open.
- **Verified in the previous slice (`TSR-2026-002`/`TSR-2026-009`, SQLite permissions):** a missing SQLite database is
  exclusively pre-created and changed to POSIX mode `0600` before SQLite can
  open it, while an existing regular file is tightened before use and a final
  database or sidecar symlink is rejected. SQLite receives a filesystem path
  encoded as a URI with `mode=rw`, preventing it from recreating a removed path
  with default permissions. Existing WAL/shared-memory/rollback-journal files
  are protected before open and newly created WAL/SHM files inherit and are
  rechecked against the main-file mode. `VACUUM INTO` now writes beneath a
  fresh mode-`0700` staging directory, so a snapshot remains inaccessible to
  other local users during the full copy before the completed file is changed
  to `0600`, verified, synced, and atomically published without overwrite.
  Regression tests deliberately set umask `0000`, verify main/WAL/SHM/final-
  backup modes, exercise the unpublished staging window and cleanup, tighten a
  pre-existing `0644` database, and reject main/WAL symlinks. This remediates
  maintainer-audit findings `TSR-2026-002` and `TSR-2026-009` for independent
  retest. The complete pinned `scripts/security-review-checks.sh` gate passes
  under exact Go 1.25.13, including zero reachable `govulncheck` findings, the
  full race/coverage suite, Linux vet/build, and Windows amd64 vet/build. This
  does not independently close either finding or the M2.5 gate.
- **Verified in the previous slice (`TSR-2026-001`, enrollment token scope):** enrollment-token issuance now
  requires a bounded `collector_id`; `TokenStore` stores that identity and
  returns the same generic invalid-token error for a mismatch without consuming
  the token. The response and audit detail identify the intended collector but
  never expose the token beyond the issuance response. Tests cover malformed,
  unknown-field, trailing-object, and oversized issuance requests; store/API
  identity mismatch and retry; concurrent correct-versus-mismatched redemption;
  response identity; and audit identity/redaction. The complete pinned
  `scripts/security-review-checks.sh` gate passes under exact Go 1.25.13,
  including zero reachable `govulncheck` findings, the full race/coverage
  suite, Linux vet/build, and Windows amd64 vet/build. This remediates
  `TSR-2026-001` for independent retest; it is not independent closure, and the
  remaining findings keep the M2.5 gate open.
- **Verified in the latest slice (external-review preparation):**
  `docs/security-review.md` maps principals, attack surfaces, code, and evidence
  into a reviewer scope and defines stable finding records, severity decisions,
  remediation ownership, and independent closure. A first official
  `govulncheck` run against the old exact Go 1.23.12 release baseline found 41
  reachable vulnerabilities. Exact Go 1.25.13 plus
  `golang.org/x/crypto v0.52.0` and `github.com/Azure/go-ntlmssp v0.1.1`
  clears every reachable finding under pinned `govulncheck v1.7.0`; CI and
  `scripts/security-review-checks.sh` now fail on a regression. The verbose scan
  retains the module-only `x/crypto/openpgp` advisory, but Topo imports only
  `x/crypto/ssh`, so no affected package or symbol is reachable. A self-audit
  also found that the Vault/Kubernetes secret adapters accepted plaintext HTTP
  despite their verified-TLS contract; both now require strict HTTPS, reject
  ambiguous base URLs, and refuse redirects, with real-TLS and downgrade/token-
  forwarding regression tests. `scripts/security-review-checks.sh` passes end
  to end under exact Go 1.25.13: module verification, formatting/diff checks,
  vet, the vulnerability scan, the full race/coverage suite, Linux build, and
  Windows amd64 vet/build. Workflow YAML parses and `actionlint` passes. This
  committed tree also reproduces all six raw release archives and their
  metadata byte-for-byte from two absolute paths with `go1.25.13`, then
  reproduces the DEB/RPM/Helm package set from those verified inputs. This is
  maintainer preparation and remediation, not an independent review; the gate
  remains open.
- **Reviewer engagement brief issued (2026-08-23):**
  `docs/security-review-engagement.md` records the outbound brief used to
  commission the independent review against immutable commit
  `c0cfb7848e6732590002265fccd7cf0fcbd8c7e9` (the M2.5 external-security-review
  preparation merge, `docs/security-review.md`'s target). It restates that
  packet's scope, trust boundaries, invariants, rules of engagement, finding
  format, and closure protocol as a self-contained brief for the independent
  reviewer, plus the required initial review-plan response. Issuing the brief
  is commissioning, not the review itself: the independent review, findings,
  remediation, and independent retest remain open, and this handoff will
  record the reviewer's identity, findings register, and retest evidence once
  they are received.
- **Verified in the previous slice (package-manager distribution):** a
  standard-library promotion builder validates release checksums and emits
  deterministic APT, RPM, Homebrew, WinGet, and OCI Helm inputs without
  rebuilding or repackaging Topo. Focused tests cover byte identity, two-path
  reproduction, tamper cleanup, channel policy, and MSI product-code parity;
  a real build against slice-5 CI artifacts also succeeds. Release automation
  now isolates RPM, Authenticode, Developer ID, and Apple notarization keys and
  refreshes evidence over the final native-signed bytes. The protected manual
  promotion workflow verifies release identity/provenance, signs repository
  metadata, gates stable on an authenticated real N-1 tag, exercises actual
  N-1-to-N upgrade and N-to-N-1-to-N rollback on Ubuntu/Fedora plus Homebrew/
  WinGet/OCI paths, and mutates external channels only after every test passes.
  The required organization repositories, production keys, first beta tag,
  and real N-1 stable tag do not exist yet; production signing, external
  publication, and clean-channel evidence remain explicitly unverified rather
  than simulated. Pull-request CI, including ephemeral RPM and repository
  signing, passed in
  <https://github.com/Nischoy-ai/topo/actions/runs/32625751259>. See
  [`docs/distribution.md`](distribution.md).
- **Verified in the previous slice (package artifacts):** package assembly uses
  the verified raw archives without rebuilding Topo and reproduces the DEB,
  RPM, and Helm outputs byte-for-byte from different absolute paths. CI
  installs/removes the amd64 DEB and RPM on Ubuntu and Fedora, verifies the
  exact raw-release payload, and proves the dormant service definition neither
  starts nor enables itself and leaves operator state intact. The Windows job
  builds amd64/arm64 MSI packages, then silently installs, payload-verifies,
  upgrades, and uninstalls amd64 while checking machine PATH, product identity,
  absence of automatic service registration, and state preservation. The Helm
  job requires an existing Secret and passes lint, install, upgrade, rollback,
  and uninstall in Kind under the hardened pod defaults. The final offline
  bundle is assembled from those verified artifacts and validates its own
  checksum manifest after extraction. Full Go/race, raw-release reproduction,
  native-package, Windows, Helm, and offline-bundle checks passed in GitHub CI
  run <https://github.com/Nischoy-ai/topo/actions/runs/32611267455>.
  Production Authenticode signing is implemented as a fail-closed tag-release
  gate but remains deliberately untriggered until signing secrets are
  configured and a reviewed `main` commit is intentionally tagged; pull-request
  CI makes no native-trust claim.
- **Verified in the previous slice (release supply chain):** the committed tree
  builds the Linux, macOS, and Windows amd64/arm64 matrix twice from different
  absolute source paths and reproduces every archive, checksum, and metadata
  byte-for-byte. Archive tests independently cover deterministic tar.gz/ZIP
  contents, executable modes, semantic release/pre-release validation, and
  rejection of version/commit injection. A locally extracted macOS arm64
  binary reports the embedded `v0.0.0-dev` version. Under exact Go 1.23.12,
  `gofmt`, `git diff --check`, `go vet ./...`, `go test -race
  -coverprofile=coverage.out ./...`, and Linux `go build -trimpath` pass, as
  do Windows amd64 vet/build and the full six-target reproducibility proof.
  The tag-only OIDC signing, attestation, and publication path deliberately
  remains untriggered until a reviewed `main` commit is intentionally tagged.
- **Verified in the previous slice (backup/restore):** `topo storage backup` creates a compact,
  transactionally consistent SQLite snapshot, protects and verifies it, and
  publishes without overwriting; `topo storage restore` read-only validates a
  backup and publishes a separately verified owner-only copy at a new path.
  File-backed recovery drills retain generated observations/assets/
  relationships and every audit/schedule/revocation table available across
  supported schema versions 1 through 4, then forward-migrate the restored
  copy. A forced version-3 migration conflict proves the entire v1-to-v4
  sequence rolls back to v1 without leaving the version-2 table behind.
  Corrupt input and existing destinations fail safely. Under Go 1.23,
  `gofmt`, `git diff --check`, `go vet ./...`, `go test -race
  -coverprofile=coverage.out ./...`, and `go build -trimpath ./cmd/topo` pass,
  as do `GOOS=windows GOARCH=amd64 go vet ./...` and the Windows build.
- **Verified in the previous slice (certificate revocation):** shared Memory/SQLite conformance
  covers immutable, idempotent, concurrent serial revocation; SQLite tests
  cover v1/v2/v3-to-v4 migration and revocation persistence across reopen;
  controller tests cover canonicalization, operator authorization, one audit
  event for an idempotent revoke, fail-closed lookup errors, denial of
  collector traffic and rotation, independent bearer fallback without trusting
  the revoked certificate identity, fresh-token re-enrollment recovery, and
  deterministic rotation/revocation race ordering. Under Go 1.23,
  `gofmt`, `git diff --check`, `go vet ./...`, `go test -race
  -coverprofile=coverage.out ./...`, and `go build -trimpath ./cmd/topo` pass,
  as do `GOOS=windows GOARCH=amd64 go vet ./...` and the Windows build. A live
  smoke test created serial `abcd` through the authenticated API against a
  file-backed SQLite controller, restarted the process against the same DB,
  and confirmed both the immutable revocation and its hash-chained audit event
  remained available. The race suite also exposed loopback-socket flakiness in
  `TestRunDeliversWhileControllerReachable` under parallel package load; that
  test now routes the real Sender request through the real controller handler
  with an in-process HTTP transport while preserving its one-second,
  multiple-delivery, and empty-spool assertions. Dedicated network and real-TLS
  integration tests still exercise sockets separately.
- **Verified in the previous slice (scheduling):** Under Go 1.23, `gofmt -l`
  (clean), `go vet ./...` (Linux and `GOOS=windows GOARCH=amd64`), `go
  test -race ./...`, `go build -trimpath ./cmd/topo`, and the Windows
  cross-compile build all pass. New tests: `internal/store/storetest`
  conformance subtests for schedule round-trip/upsert-replaces,
  not-found, delete, and list-all-collectors, run identically against
  `Memory` and the SQLite backend; SQLite-specific tests for a
  version-1-to-latest and a version-2-to-latest migration upgrade (the
  latter the more realistic near-term case) and for a schedule surviving
  reopen; a `JobStore.HasOutstanding` unit test; controller-level tests
  that creating a schedule produces a due job on the very next poll, that
  a second immediate poll does not produce a second job (no pile-up) even
  when a schedule is forced due again while the first job is still
  outstanding, that updating a schedule replaces it in place while
  preserving `created_at`, interval bounds are rejected, all three
  schedule endpoints require auth, deleting a schedule stops future jobs,
  and each of `schedule_created`/`schedule_updated`/`schedule_deleted` is
  audited. Also manually verified end to end: `topo serve -db-driver
  sqlite -db-dsn <path>`, created a schedule via `curl`, confirmed the
  next poll produced exactly one due job and a second immediate poll
  produced none, killed and restarted the controller against the same
  `-db-dsn`, confirmed the schedule was still there, then deleted it and
  confirmed polling stopped producing jobs.
- **Verified in the previous slices (persistent storage, audit log):** see
  PR #23 and PR #24 for the equivalent detail; both followed the same
  gofmt/vet/race-test/cross-compile/manual-smoke-test verification bar as
  every slice in this project.
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
  own — it was evaluated and deferred in the completed
  persistent-storage-and-scheduling milestone. Automatic background Vault token
  renewal for long-running processes, and support for leased
  dynamic-secrets engines beyond token renewal, remain deferred follow-ups.
  One agent instance per host (fixed systemd unit / Windows service name)
  is an intentional Agent MVP limitation, not tracked as a gap. Do not
  attempt to parse or assume ServiceNow's IRE response schema without a
  real instance to verify it against. Heartbeat
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
   present; application middleware enforces authorization per endpoint).
   A verified peer certificate satisfies collector data-plane authorization
   without the bearer key.
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
   certificate (the same authorization policy every other data-plane
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
- This completed milestone did not include revocation; M2.5 slice 2 now adds
  serial-specific early invalidation without changing the historical rotation
  contract described here.
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
  controller's CA reaches collector data-plane endpoints without a bearer
  `Authorization` header.
- A request presenting neither a verified client certificate nor a correct
  bearer key is rejected (401) by every configured protected endpoint.
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
  fallback for this endpoint specifically (unlike collector data-plane
  endpoints).
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
   version declaring `go 1.22` compatibility with the project's then-current
   `go 1.23.0` baseline — newer versions required `go 1.24` and were
   deliberately not used in that discovery slice. The M2.5 security-review
   preparation later raised the project baseline independently.
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
   `v0.52.0` — the last release declaring `go 1.23.0` compatibility when this
   discovery slice was implemented) to
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

## Completed milestone: persistent observation/audit storage and scheduling

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
1.23.0` compatibility with the project's then-current toolchain (newer releases
required `go 1.24`/`go 1.25` and were deliberately not used in that storage
slice, the same reasoning applied to every prior dependency pin in this
project). The M2.5 security-review preparation later raised the toolchain
independently while retaining this tested storage version. It is a
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

PostgreSQL is deliberately not used yet. It was evaluated as part of this
milestone, and the conclusion is:
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

1. **Done.** Persistent storage: a new `internal/store/sqlite` package
   implementing the existing `store.Repository` interface (observations,
   assets) plus a new `ListRelationships` method both `Memory` and the new
   SQLite backend implement — relationships were not previously queryable
   at all through `store.Repository`, even though `Memory.SaveObservation`
   received them in every envelope; this was a real gap fixed here, not
   scope creep, since retrofitting a persisted schema after the fact is a
   real migration cost. `model.StableRelationshipID` mirrors the existing
   `model.StableAssetID` scheme (hash of type/from/to). Saving an
   observation is now idempotent by `ObservationID` in both backends too
   (a resubmitted ID replaces in place rather than duplicating) — a second
   real gap found and fixed while defining the `Repository` contract
   formally for the first time. CLI: `topo serve -db-driver sqlite -db-dsn
   <path>` (default `-db-driver memory`, unchanged prior behavior). A
   shared black-box conformance suite (`internal/store/storetest`) runs
   identical assertions against both `Memory` and the SQLite backend
   through the `Repository` interface alone, so the two implementations
   cannot silently diverge in observable behavior; a `TestSQLiteDataSurvivesReopen`
   test and a `TestSQLiteRejectsNewerSchemaVersion` test cover behavior
   specific to the persistent backend. New `GET /v1/relationships`
   endpoint alongside the existing `GET /v1/assets` and `GET
   /v1/observations`. Enrollment tokens, heartbeats, and job state remain
   in-memory only — persisting them is a question for a later slice, not
   assumed now. See `docs/storage.md`.
2. **Done.** Immutable audit log: a new `internal/audit` package
   (`Event`/`Entry`, `Chain`, `VerifyChain`) implements the hash chain
   itself — each entry's stored hash covers its own content and the
   previous entry's hash, so editing, reordering, or removing an entry
   after the fact breaks the chain from that point forward, detectably via
   `VerifyChain`; "immutable" means this tamper-evidence, not that the
   underlying storage is physically write-once, and not cryptographic
   non-repudiation. `store.Repository` gained `AppendAuditEvent` and
   `ListAuditEntries`, implemented by both `Memory` and the SQLite backend
   (a new `audit_entries` table, schema version 2 — the `migrate` function
   was generalized from applying one fixed migration to applying every
   pending versioned migration in order, so an existing version-1 database
   upgrades in place rather than needing to be recreated). The controller
   appends an entry for the four actions named when this slice was
   scoped — `enrollment_token_issued`, `collector_enrolled`,
   `certificate_rotated`, `job_created` — best-effort with respect to the
   action itself (an audit-write failure is logged, not treated as
   grounds to fail or undo an action that already completed, since none of
   those actions' effects live in `store.Repository` to roll back). Detail
   fields are always short strings and never secret material — an
   enrollment token is referenced only by a truncated SHA-256 fingerprint.
   New `GET /v1/audit` endpoint (auth required like every other read
   endpoint); it returns entries as stored and does not itself re-verify
   the chain. See `docs/storage.md`.
3. **Done.** Server-side recurring discovery scheduling: `store.Repository`
   gained a `Schedule` type (`collector_id`, `job_type`,
   `interval_seconds`, `next_run_at`) and `UpsertSchedule`/`ListSchedules`/
   `GetSchedule`/`DeleteSchedule`, implemented by both `Memory` and the
   SQLite backend (a new `schedules` table, schema version 3). New
   `POST /v1/schedules` (upsert, keyed by `collector_id` — at most one
   schedule per collector), `GET /v1/schedules`, and
   `DELETE /v1/schedules/{collector_id}` endpoints. Deliberately no
   background ticker: a schedule only ever turns into an actual
   `model.Job` lazily, the moment its collector next polls `GET /v1/jobs`
   and the schedule is found due, reusing `POST /v1/jobs`'s existing
   collector-initiated-polling machinery outright rather than building a
   second, parallel job-dispatch path — Topo Agent is deliberately
   outbound-only, so a background ticker would have nothing to push to
   anyway. `JobStore` gained `HasOutstanding` so a schedule does not pile
   up a second job while an earlier one is still outstanding for that
   collector; when a job is actually queued, `next_run_at` advances to
   `now + interval_seconds` (never `old next_run_at + interval_seconds`),
   so a collector offline for several intervals catches up with exactly
   one job, not a backlog. Unlike enrollment tokens, heartbeats, and job
   state — all deliberately left in-memory-only by this milestone's
   earlier slices — a schedule *is* persisted under `-db-driver sqlite`:
   it is a standing operator policy, not short-lived or self-healing like
   a heartbeat or a single job, so silently losing it on restart would be
   a real, easy-to-miss operational surprise. Schedule changes are
   audited (`schedule_created`/`schedule_updated`/`schedule_deleted`).
   See `docs/scheduling.md`.

**This completes the persistent observation/audit storage and scheduling
milestone** — all three slices are done. See "Follow-on order" below for
what comes next.

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

## Completed milestone: M2.5 release readiness and security hardening

### Objective

Turn the implemented discovery/controller capabilities into a system that can
be operated and distributed safely. This milestone closes trust-boundary,
compromise-recovery, upgrade/restore, supply-chain, packaging, distribution,
and independent security-review gaps. It does not add new discovery protocols.

### Completion status

All seven slices below are implemented and merged, including remediation of
every security finding raised so far
(`TSR-2026-001`/`002`/`003`/`004`/`009` — see `docs/security-review.md`).
M3 work below proceeds without waiting on the two items that remain open as
tracked follow-up, not blockers: (1) the independent reviewer's retest of
those merged fixes — no finding is `Verified` until that happens, regardless
of this milestone's completion; and (2) real beta/N-1 package-channel
promotion evidence, deferred until the user authorizes external repository
and production signing-key provisioning. Neither omission is a
production-readiness claim; see "Release gates" in `ROADMAP.md`.

### Slices

1. **Done — operator versus collector authorization.** With
   `-api-key-ref` configured, the bearer key authorizes operator reads and
   control-plane mutations: assets, relationships, observations, audit,
   collector status, enrollment-token issuance, job creation/status, and all
   schedule operations. A verified collector certificate alone is limited to
   observation delivery, heartbeats, job polling/results, and certificate
   rotation; it receives 403 from operator endpoints. The verified peer
   identity overrides an observation body's `collector_id`, matching existing
   heartbeat and job-result binding. Bearer auth remains accepted on collector
   endpoints for compatibility, and an empty API key preserves evaluation
   mode. Consequently, the bearer key still carries operator authority and
   must not be distributed where certificate-only least privilege is desired.
2. **Done — certificate revocation and compromise recovery.** Revocation is
   an immutable record keyed by the exact canonical X.509 serial, not a
   collector-wide flag. Operator-only `POST /v1/certificate-revocations`
   creates it idempotently and `GET` lists it. SQLite schema version 4 makes
   the record survive restarts; the memory backend has identical semantics but
   remains evaluation-only. Collector authorization and rotation fail closed
   when the lookup is unavailable, and a revoked certificate receives 401.
   Enrollment/rotation return serials; rotation retains the old certificate
   until explicit revocation to prevent lost-response lockout. A controller
   mutex linearizes rotation signing with revocation writes in the supported
   single-controller process. Recovery is a fresh enrollment token and a new
   key/serial for the same collector ID, never unrevoke.
3. **Done — backup/restore and upgrades.** Provide `topo storage backup`
   and `topo storage restore` for verified SQLite snapshots. Neither command
   overwrites an existing destination. Exercise restore plus forward migration
   from every supported schema with retained observation/security/policy data,
   and make all pending migrations one transaction so any failure returns the
   database to its exact pre-upgrade schema. The supported downgrade procedure
   is to stop the controller and restore a pre-upgrade backup to a new path;
   Topo never attempts a lossy reverse migration or overwrites the failed
   database in place.
4. **Done — supply-chain release evidence.** Build deterministic raw
   archives for Linux, macOS, and Windows on amd64 and arm64 with exact Go
   1.25.13, `CGO_ENABLED=0`, trimmed paths, no implicit VCS stamp, and fixed
   archive metadata. Build twice from different absolute source paths and fail
   on byte drift. Publish sorted SHA-256 checksums, deterministic build
   metadata, an SPDX SBOM, a keyless Sigstore signature over the checksum
   manifest, and signed GitHub SLSA build/SBOM attestations. Pin every release
   action by immutable commit, verify signature and provenance before a tag can
   create a GitHub Release, and document independent consumer verification.
5. **Done — package artifacts.** Ship raw archives, DEB, RPM, MSI, Helm, and an
   offline bundle using the same immutable verified artifacts and documented
   upgrade/rollback paths. The host package installs one `topo` binary and
   platform service definitions without embedding credentials or silently
   starting an unconfigured service.
6. **Done — package-manager automation; operational evidence deferred.** Promote the package artifacts through a
   signed Nischoy-hosted APT repository, a signed Nischoy-hosted RPM repository,
   `Nischoy-ai/homebrew-tap`, Microsoft's `winget-pkgs` catalog, and an OCI Helm
   registry. GitHub Releases remain the immutable artifact source of truth;
   repository/catalog metadata must reference those exact artifacts and
   checksums rather than rebuilding per channel. Maintain stable and beta
   promotion, native repository signing/key rotation, and clean-machine fresh
   install, N-1-to-N upgrade, configuration preservation, uninstall/purge, and
   rollback tests. Additional ecosystems (Chocolatey, Scoop, AUR, Snap) follow
   demonstrated demand rather than blocking these initial native channels. A
   real beta and real N-1 stable promotion remain required but are deferred
   until external repositories and production signing credentials are
   explicitly authorized and provisioned.
7. **Current — external security review.** The review-preparation slice defines
   the threat-boundary scope, reproducible baseline, finding schema, severity
   policy, remediation ownership, and independent closure criteria. It also
   upgrades the release/security baseline to exact Go 1.25.13 and fixed
   `x/crypto`/`go-ntlmssp` versions after a baseline scan found 41 reachable
   vulnerabilities, adds a pinned zero-reachable-finding CI gate, and requires
   HTTPS/no redirects for external secret providers. Commission an independent
   review against an immutable `main` commit, remediate its findings, and
   retain independent retest evidence before any production-readiness claim.
   The first maintainer-audit remediation (`TSR-2026-001`) binds every
   enrollment token to its operator-selected collector ID at issuance and
   rejects a different enrollment identity without consuming the token. It is
   ready for independent retest after merge, not independently verified.
   `TSR-2026-002` and `TSR-2026-009` protect the live SQLite file before open
   and keep an in-progress backup snapshot beneath a private staging directory;
   they likewise require merge and independent retest. `TSR-2026-003` (low
   severity) routes a `workflow_dispatch` version input through `env:` instead
   of raw `${{ }}` interpolation in four `promote.yml` shell/PowerShell steps,
   closing a script-injection primitive into the job that later imports
   release-signing secrets; a new `scripts/check-workflow-interpolation.sh`
   check runs in ordinary CI to prevent recurrence. At discovery, a separate
   earlier step already constrained the input to a safe semver pattern before
   the four affected steps ran, so this was not independently exploitable —
   it is filed and fixed as defense-in-depth against that constraint moving,
   and still requires merge and independent retest like the others. The
   remaining findings still require remediation or a protocol-compliant
   disposition.

### Acceptance gates for slice 1

- Every operator endpoint rejects a verified collector certificate without the
  bearer key with 403; a missing/incorrect credential without a verified
  certificate receives 401.
- Every collector data-plane endpoint continues to accept a verified
  certificate, and the bearer-key path remains compatible with existing
  agents.
- Presenting a valid bearer key alongside a collector certificate authorizes
  operator access, so clients that automatically attach certificates do not
  break.
- An observation submitted over mTLS is stored under the verified certificate
  identity even if its body claims another collector.
- Leaving the API key empty preserves the existing open evaluation mode.
- Focused real-TLS tests and the full Go 1.23 Linux/Windows verification gates
  pass; README, security documentation, roadmap, and this handoff describe the
  same endpoint matrix and its remaining shared-bearer limitation.

### Deliberate non-goals for slice 1

- No new user/RBAC/SSO system and no second bearer-key type. This slice removes
  administrative authority from collector certificates; bearer-only collectors
  still possess the operator key by definition and should migrate to mTLS where
  least privilege is required.
- Certificate revocation intentionally did not share the slice-1 PR; slice 2
  supplies the durable design rather than an in-memory denylist.
- No removal of open evaluation mode or bearer compatibility. Tightening those
  defaults is a separately versioned compatibility decision.

### Acceptance gates for slice 2

- The repository contract is identical for Memory and SQLite: first revoke
  wins, repeats are immutable/idempotent, concurrent repeats create one record,
  and list/check operations agree.
- SQLite upgrades supported version-1, version-2, and version-3 databases to
  schema version 4 and preserves revocations across close/reopen.
- Operator bearer authorization is required to create/list revocations; an
  enrolled collector certificate alone receives 403.
- A revoked certificate receives 401 from collector data-plane endpoints and
  `POST /v1/rotate`. A storage-check failure returns 503 rather than allowing
  the credential.
- A valid bearer key remains an independent compatibility credential on
  collector endpoints, but identity is not taken from a simultaneously
  presented revoked certificate.
- Fresh-token re-enrollment of the same collector ID produces a distinct serial
  that works immediately while the old immutable record remains enforced.
- Enrollment and rotation responses/CLI output expose canonical serials;
  enrollment, rotation, and revocation audit detail references those serials.
- Revocation and rotation have a tested linearizable order inside the supported
  single-controller process; docs state that an already-authorized rotation can
  finish first and its new serial may need separate revocation.
- Full Go 1.23 formatting, vet, race/coverage, Linux build, Windows vet/build,
  and a persistent-restart smoke test pass.

### Deliberate non-goals for slice 2

- No CRL or OCSP publication and no TLS-handshake rejection. Enforcement is at
  Topo's application authorization layer after native mTLS verifies the peer.
- No collector-ID-wide revocation, unrevoke endpoint, or automatic revocation
  of the old serial during rotation. Explicit serials keep incident evidence
  immutable and avoid lost-response lockout.
- No HA/multi-controller coordination. The mutex defines races for the only
  supported deployment shape; revisit atomic issuance/revocation across
  processes when clustered controllers become real.
- No removal of bearer compatibility. If the shared key was also exposed, the
  operator must rotate it separately.

### Acceptance gates for slice 3

- `topo storage backup -db-dsn <database> -out <backup>` uses SQLite's
  transactionally consistent snapshot operation, verifies the completed file
  with `PRAGMA quick_check`, reports its schema version, and publishes it only
  after syncing it to durable storage.
- `topo storage restore -from <backup> -db-dsn <new-database>` validates the
  source read-only, copies it with owner-only permissions, verifies the copy,
  and never replaces an existing destination or modifies the source backup.
- Recovery drills restore real persisted observations/assets/relationships and
  the audit/schedule/revocation state available in each of schema versions 1,
  2, 3, and 4, then open the restored file with the current binary and retain
  the data through forward migration.
- Every pending schema migration is committed in one transaction. A failure in
  any later version leaves both `PRAGMA user_version` and every earlier schema
  object at the exact pre-upgrade state.
- Documentation covers backup before binary/package upgrade, controller
  shutdown for restore/cutover, forward upgrade, validation, rollback to a new
  database path with the old binary, retention, filesystem permissions, and
  the in-memory state that backups cannot preserve.
- Full Go 1.23 formatting, vet, race/coverage, Linux build, Windows vet/build,
  CLI smoke, and file-backed recovery-drill gates pass.

### Deliberate non-goals for slice 3

- No reverse/down migration. Restoring the pre-upgrade file is safer and keeps
  the failed/upgraded database available for diagnosis.
- No overwrite/`--force` mode and no live restore into a running controller.
  Cutover is an explicit stop, restore-to-new-path, start, and verify operation.
- No remote backup destination, scheduler, retention daemon, encryption layer,
  or PostgreSQL tooling. Operators may copy the verified owner-only snapshot to
  encrypted managed storage under their own retention policy.
- SQLite backup covers persisted repository state only. Enrollment tokens,
  heartbeats, and individual queued/in-flight jobs remain in memory and cannot
  be recovered from the database.

### Acceptance gates for slice 4

- One release command builds Linux, macOS, and Windows archives for amd64 and
  arm64 with exact Go 1.25.13 and embeds the semantic tag in `topo version`.
  Every archive contains the binary, `LICENSE`, and `README.md` under one
  version/platform directory.
- `CGO_ENABLED=0`, `-trimpath`, and `-buildvcs=false` remove host compiler,
  source path, and implicit working-tree metadata. Archive timestamps,
  uid/gid, modes, gzip headers, entry order, metadata JSON, and checksum order
  are fixed or derived only from explicit version/commit/toolchain inputs.
- CI exports the same commit into two different absolute paths, builds the full
  matrix independently, and requires every archive, `release-metadata.json`,
  and `SHA256SUMS` byte to match. The normal pull-request CI runs this check so
  reproducibility cannot remain an unexercised tag-only path.
- The semantic-tag workflow rejects malformed tags and commits not reachable
  from `origin/main`, uses only commit-pinned actions, and creates no GitHub
  Release until every build/evidence verification step passes.
- Syft emits an SPDX JSON SBOM. The workflow signs `SHA256SUMS` keylessly with
  its GitHub OIDC identity and creates signed GitHub SLSA provenance and SBOM
  attestations for every checksummed subject; their downloadable Sigstore
  bundles remain beside the release artifacts.
- Before publication, CI verifies the checksum signature against the exact
  `Nischoy-ai/topo/.github/workflows/release.yml@refs/tags/<version>` identity
  and GitHub Actions issuer, then verifies at least one archive through the
  GitHub attestation API.
- `docs/releases.md` gives maintainers a tag/release procedure and gives users
  checksum, Cosign identity-constrained signature, GitHub provenance, and local
  byte-reproduction commands. README, SECURITY, roadmap, AGENTS, and this
  handoff describe the same trust boundary.
- Full Go 1.25 formatting, vet, race/coverage, Linux build, Windows vet/build,
  release-tool unit tests, full-matrix local reproduction, and GitHub CI pass.

### Deliberate non-goals for slice 4

- No DEB, RPM, MSI/MSIX, Helm chart, offline bundle, Homebrew formula, APT/RPM
  metadata, or WinGet manifest. Those consume this slice's immutable archives
  in slices 5 and 6 rather than sharing one release mega-PR.
- No long-lived general artifact-signing private key in GitHub Actions. Keyless
  Sigstore identities and GitHub attestations cover raw artifacts; persistent
  native repository/installer keys are introduced only with their scoped
  storage and rotation design.
- No claim that generic artifact evidence satisfies operating-system trust.
  Authenticode remains a Windows package gate; macOS signing/notarization and
  APT/RPM OpenPGP signatures remain native distribution-slice gates.
- No first public version tag is created by this implementation PR. The release
  workflow is exercised by pull-request reproducibility checks; maintainers tag
  a reviewed `main` commit separately when release contents are intentionally
  frozen.

### Acceptance gates for slice 5

- Package assembly consumes the already-verified raw archives and never invokes
  `go build`. The `topo` payload extracted from every DEB, RPM, and MSI must
  match the corresponding raw-archive binary digest exactly.
- Linux amd64 and arm64 each receive a DEB and RPM containing `/usr/bin/topo`,
  the license/readme, and the hardened `topo-agent.service` definition. Package
  install scripts may create the dedicated system identity and reload systemd,
  but must never embed credentials, create an active configuration, enable, or
  start the unconfigured service.
- Windows amd64 and arm64 receive MSI packages with one stable upgrade identity,
  per-version product identities, machine-wide PATH registration, and no
  automatic Topo Agent service registration. The amd64 installer is exercised
  on a Windows runner through silent install, version check, upgrade, and silent
  uninstall; arm64 is structurally validated because hosted arm64 Windows test
  execution is not available.
- The release path Authenticode-signs each MSI with a protected release
  certificate and verifies the signature before publication. It fails closed
  when signing material is absent. Pull-request tests use no production key and
  make no native-trust claim.
- The Helm chart renders a non-root, read-only-root-filesystem controller with
  explicit resource limits and requires an existing Kubernetes Secret for the
  operator API key. The chart contains no credential value and passes lint,
  template, install, upgrade, rollback, and uninstall tests against an ephemeral
  cluster without rebuilding the application image.
- One deterministic offline bundle contains the raw archives, native packages,
  Helm chart, release metadata, documentation, and its own sorted checksum
  manifest. Every referenced payload verifies without network access after the
  bundle has been downloaded.
- Package artifacts and package metadata are incorporated into the release-wide
  sorted `SHA256SUMS`, SBOM/provenance subjects, signature verification, and
  GitHub Release only after package-content tests pass.
- Upgrade and rollback documentation requires a verified SQLite backup before
  replacing the binary/package, preserves operator-created configuration and
  state on ordinary uninstall/upgrade, and restores the backup with the old
  binary for database rollback rather than reverse-migrating in place.
- The full Go 1.25 Linux/Windows gates, raw-archive reproducibility proof,
  package-content tests, package-assembly reproducibility proof, and GitHub CI
  pass.

### Deliberate non-goals for slice 5

- MSI is the initial Windows installer; MSIX is not produced in parallel. One
  tested native format is preferable to two partially tested identities, and
  WinGet accepts MSI installers. Add MSIX only if Store/containerized-app
  requirements justify its separate identity and execution-alias design.
- No APT `Packages`/`InRelease`, RPM repository metadata, Homebrew formula,
  WinGet manifest, or Helm registry publication. Slice 6 promotes the exact
  package bytes and adds repository/catalog-native signatures and key rotation.
- No automatic service enrollment or generated credentials. Installing a host
  package makes the binary and dormant service definition available; an
  operator must supply configuration/secrets and explicitly enable the agent.
- macOS remains the signed raw archive consumed by Homebrew rather than gaining
  a separate PKG/DMG format. Developer ID signing/notarization is coupled to the
  Homebrew publication path in slice 6; the raw archive continues to carry the
  slice-4 Sigstore/GitHub evidence until that native distribution gate exists.
- Clean-machine channel promotion tests across every supported distribution and
  a real N-1 stable release remain slice-6 gates. Slice 5 tests package
  semantics directly with synthetic versions and native package tools.

### Acceptance gates for slice 6

- One deterministic promotion command verifies the authenticated GitHub
  Release checksum manifest and generates APT, RPM, Homebrew, WinGet, and OCI
  Helm inputs twice from different absolute paths. It rejects byte drift,
  release tampering, unsafe filenames, invalid semantic tags, prereleases in
  stable, and normal releases in beta.
- RPMs are OpenPGP-signed before their final GitHub Release checksums,
  attestations, and publication. macOS binaries are Developer-ID-signed and
  notarized before deterministic re-archiving. Authenticode remains mandatory
  for MSI. Every native-signing job fails closed without its protected material
  and the final evidence describes the post-signing bytes.
- APT publishes architecture-specific `Packages`/`Packages.gz`, by-hash files,
  a bounded-validity `Release`, clear-signed `InRelease`, and detached
  `Release.gpg`. User setup uses a repository-scoped key under
  `/etc/apt/keyrings` and Deb822 `Signed-By`, never global `apt-key` trust.
- RPM repositories retain the exact signed RPM release assets, generate
  per-architecture `repodata`, sign `repomd.xml`, and enable both `gpgcheck`
  and `repo_gpgcheck` in the published `.repo` file.
- Homebrew formulas select immutable GitHub Release archives and checksums for
  macOS/Linux amd64/arm64, pass strict audit, install/test/uninstall on macOS,
  and verify the installed macOS executable's Developer ID/notarization trust.
- WinGet multi-file manifests contain the exact Authenticode-signed amd64/arm64
  MSI URLs, SHA-256 digests, and deterministic product codes; pinned official
  Microsoft validation passes and the referenced x64 MSI installs, reports the
  expected version, and silently uninstalls before submission.
- Helm pushes the existing chart archive to GHCR without `helm package`, pulls
  the same semantic version, and compares the pulled chart byte-for-byte before
  publication succeeds.
- Beta and stable run through separately protected environments under one
  shared serialization lock because both mutate shared repositories.
  Stable requires an actual distinct N-1 stable GitHub Release and proves
  clean-machine install, N-1-to-N upgrade, state/configuration preservation,
  uninstall, and documented rollback before external mutation.
- Repository private keys are unavailable to ordinary CI. Full-fingerprint
  validation, protected-environment approval, scoped external-repository
  tokens, an old/new public-key overlap mechanism, expiry monitoring, and an
  incident rotation procedure are documented and fail closed.
- A real beta promotion and then a real N-1 stable promotion pass against the
  public HTTPS channels. The WinGet gate completes only when Microsoft's
  external validation/review merges the generated catalog pull request; CI
  must not claim control over that result.

### Deliberate non-goals for slice 6

- No submission to `homebrew/core`; the organization tap is the supported
  launch path until adoption and Homebrew eligibility justify upstreaming.
- No Chocolatey, Scoop, AUR, Snap, MSIX, PKG, or DMG in parallel. Their separate
  trust, identity, and lifecycle costs require demonstrated demand.
- No mutable replacement of an existing tag or GitHub Release asset. A bad
  release is corrected with a new semantic version and a new promotion.
- No claim that pull-request generation tests prove production native trust or
  public channel operation. Production keys are absent from PRs, and the first
  real beta/N-1 stable evidence cannot exist before reviewed tags and external
  repositories exist.

### Acceptance gates for slice 7 preparation

- `docs/security-review.md` identifies the review commit rule, principals,
  trust boundaries, attack surfaces, primary implementation/evidence, review
  tests, explicit exclusions, and sensitive-report handling.
- The release and review baseline uses exact Go 1.25.13. The old Go 1.23.12,
  `x/crypto v0.41.0`, and pre-release `go-ntlmssp` baseline finding is retained
  honestly; pinned `govulncheck v1.7.0` reports zero reachable vulnerabilities
  after the toolchain and dependency remediation and runs in ordinary CI.
- Vault and Kubernetes secret-provider clients require absolute HTTPS base
  URLs, normal server identity verification, and no redirects. Tests exercise
  real TLS and prove plaintext and redirected token-bearing requests fail.
- A reviewer can run one documented command to reproduce formatting, module
  verification, vet, known-vulnerability, race, Linux-build, and Windows-
  cross-build gates with the pinned toolchain.
- Findings have stable IDs, severity rationale, impact/reproduction, ownership,
  fix/regression evidence, and independent closure. Critical/high findings
  always block; medium accepted risk requires explicit maintainer and reviewer
  agreement with a bounded exposure and review date.
- The full exact-Go formatting, vet, vulnerability, race/coverage, Linux build,
  Windows vet/build, and release/package/distribution regression gates pass.

### Deliberate non-goals for slice 7 preparation

- Preparation and maintainer self-audit are not an independent penetration
  test, do not close the gate, and do not create a production-readiness claim.
- No external report or finding is fabricated. Commissioning, independent
  findings, fixes driven by those findings, and independent retest evidence
  remain the next gate work.
- No external package repository or production signing credential is
  provisioned. Real beta/N-1 promotion evidence remains a separate required
  operational gate until the user explicitly authorizes that provisioning.

### Distribution model for slices 4-6

One tagged CI release builds and tests the platform matrix once, then publishes
immutable GitHub Release artifacts: Linux/macOS/Windows archives, DEB, RPM,
MSI/MSIX, Helm chart, offline bundle, checksums, SBOM, signatures, and build
provenance. Package-manager automation promotes those exact bytes; it never
performs an independent rebuild. Native repository trust remains separate from
CI provenance: GitHub/Sigstore-style attestations do not replace the persistent
OpenPGP keys and rotation procedure required by APT/RPM, or Authenticode and
macOS signing/notarization where those platforms require them.

- **Debian/Ubuntu:** publish `.deb` files plus signed `Packages` and
  `InRelease` metadata under a Nischoy-controlled HTTPS APT repository. Provide
  a repository-specific key installed under `/etc/apt/keyrings` and a
  `Signed-By` source definition; never instruct users to trust a global
  `apt-key`. Initial command after one-time repository setup:
  `sudo apt install topo`.
- **Fedora/RHEL-compatible:** sign each RPM, generate repository metadata,
  sign `repodata/repomd.xml`, publish a `.repo` definition with package and
  metadata verification enabled, then support `sudo dnf install topo`.
- **macOS and Homebrew Linux:** maintain `Nischoy-ai/homebrew-tap`; its formula
  references immutable release archives/bottles and checksums. Initial command:
  `brew install nischoy-ai/tap/topo`. Submission to `homebrew/core` is a later
  adoption/eligibility step, not the launch dependency.
- **Windows:** publish a signed, silent-installable and uninstallable MSI/MSIX
  at an immutable publisher URL, then submit/update the `Nischoy.Topo` manifest
  in Microsoft's `winget-pkgs` repository so users can run
  `winget install Nischoy.Topo`.
- **Kubernetes:** publish the Helm chart to an OCI registry and test install,
  upgrade, rollback, and uninstall against the same application artifacts.

APT/RPM hosting may begin as either static Nischoy-controlled object storage
plus CDN and standard repository tools, or a managed repository service if its
operational cost is lower; that hosting decision does not change the signing,
promotion, or acceptance gates. Repository signing keys must be isolated from
ordinary build jobs. Stable and beta are explicit promotion channels, and only
stable passes after the full install/upgrade/rollback matrix succeeds.

## Current milestone: M3 — hybrid release candidate

### Objective

Extend discovery to hybrid/cloud estates and prove Topo at scale, per
`ROADMAP.md`'s M3 line:

- AWS Organizations, Azure tenants/subscriptions, and Kubernetes discovery.
- Source precedence and conflict/freshness visibility.
- Scale and upgrade testing at 1K, 10K, and 100K assets.
- SSO/RBAC commercial modules behind documented open interfaces.

### Slices

AWS/Azure/Kubernetes discovery are themselves three separate protocol
integrations, each roughly the size of the SNMP/VMware milestone above, not
one slice; the user confirmed Kubernetes discovery as the starting slice,
then AWS Organizations discovery as the second, then Azure tenant discovery
as the third — completing all three protocol integrations
`ROADMAP.md`'s M3 line names.

### Slice 1 — Kubernetes node and pod discovery (implemented, merged)

**Objective.** Extend discovery into a Kubernetes cluster's own inventory:
which nodes exist and which pods run on them, using only read-only
enumeration over the real Kubernetes API — no mutating verb is ever issued,
matching the read-only posture already established for VMware
(vSphere API) and SNMP (network devices).

**Scope.** `pkg/discovery/kubernetes` lists `v1.Node` and `v1.Pod` objects
across the cluster (all namespaces) via `k8s.io/client-go`'s REST client.
Asset identity is each object's Kubernetes UID (`metadata.uid`) — stable
across reschedules, IP reassignment, and node reboots, never the object
name or an IP address, matching the project's standing identity invariant.
Both map to the existing `model.AssetKubernetesObject` type (already
reserved in `pkg/model`, unused until now) with `Attributes["kind"]`
distinguishing `Node` from `Pod`, rather than adding new `AssetType`
constants per Kubernetes kind — the project has many more kinds than it has
fixed asset types, so a generic type plus a `kind` attribute scales the way
a per-kind constant would not. A `pod_runs_on_node` relationship connects
each pod to its node (resolved from `pod.Spec.NodeName`, matching how
`vm_runs_on_host` resolves VMware's VM-to-host relationship), skipped for
unscheduled pods with no assigned node. Node/pod IP addresses are recorded
as attributes only, never as separate `NetworkInterface` assets or as
identity — unlike a VM's or physical host's NIC, a pod's IP is expected to
change on every reschedule, so treating it as a stable sub-asset would
misrepresent it.

**Authentication and target model.** One or more API server URLs are
`discovery.Request.Targets`, matching the vCenter/WinRM/SNMP target-list
shape. Authentication is a bearer token (a Kubernetes ServiceAccount token
in production; the standard in-cluster and out-of-cluster auth model)
resolved through the existing credential-reference contract — never
accepted as a CLI argument. Production targets require HTTPS with normal
certificate verification; an explicit `-lab` flag, restricted to loopback
targets exactly like WinRM's `-lab-basic` and VMware's `-lab`, permits HTTP
and skipped certificate verification against the Topo Lab fixture below.

**Acceptance testing.** Unlike VMware (which has `vcsim`, an official
simulator built for this purpose), `client-go` has no equivalent real local
test double outside of `envtest`/`kubebuilder`, which requires downloading
platform-specific `kube-apiserver`/`etcd` binaries — not reliably available
in every build/CI environment and heavier than this slice needs. Also
ruled out: `client-go/kubernetes/fake`, the in-memory fake clientset —
it bypasses HTTP and JSON entirely (it satisfies the clientset Go interface
directly), so it would never exercise the plugin's actual request
construction or response decoding, unlike every other protocol's
acceptance test in this project. Matching SNMP's precedent instead (SNMP
also had no official simulator, since `gosnmp` is client-only): a
hand-rolled Topo Lab fixture (`pkg/lab`) serves the small set of real
Kubernetes REST API JSON responses (`GET /api/v1/nodes`,
`GET /api/v1/pods`) the plugin actually calls, over a real HTTP listener,
using the real `k8s.io/api` types for encoding — so the wire format and the
plugin's own `client-go` REST call and JSON-decode path are both genuinely
exercised, not simplified away, even though the server behind them is
hand-rolled rather than an official implementation. `topo lab
kubernetes-serve` exposes it for manual exploration, matching
`snmp-serve`/`winrm-serve`/`ssh-serve`. The two-scan idempotency acceptance
test (repeat scan, save to `store.Memory`, assert no duplicate assets) runs
against this fixture, the same shape as every prior protocol's acceptance
test. A real cluster (`kind`, a managed cluster, or any conformant API
server) was evaluated as an alternative fixture and deliberately not
required for this slice, since a hand-rolled fixture over the real REST
API shape needs no external binary download or cluster provisioning and is
exactly as reproducible as `vcsim`; real-cluster verification remains
explicitly unverified, matching the SNMP `authPriv`/real-VMware/real-Windows
posture — implemented and tested against a faithful fixture only, not yet
against a genuinely live system.

**Deliberate non-goals for this slice.** No workload-management object
kinds beyond Node and Pod (no Deployment, Service, ConfigMap, PVC, etc. —
real, scoped follow-ups for a later slice once this one's shape is
proven); no cluster-admission or mutating capability of any kind; no
in-cluster auto-config (`rest.InClusterConfig()`-style autodetection) —
targets and credentials are always explicit, matching every other
protocol; no CRD/custom-resource discovery. AWS and Azure discovery remain
separate, unstaged slices, not bundled into this one.

### Slice 2 — AWS Organizations account-structure discovery (implemented, merged)

**Objective.** Extend discovery into an AWS Organization's own account
structure: which accounts exist and how they are grouped into
organizational units under the organization's roots, using only read-only
`Describe`/`List` calls over the real AWS Organizations API — no mutating
action (create, invite, move, tag, or policy) is ever issued, matching the
read-only posture already established for VMware, SNMP, and Kubernetes.

**Scope.** `pkg/discovery/aws` calls `DescribeOrganization` and `ListRoots`,
then recursively walks `ListOrganizationalUnitsForParent` and
`ListAccountsForParent` from each root down to 5 levels of nested OUs — the
same nesting limit AWS Organizations itself enforces, kept as an explicit
code-level bound rather than trusted implicitly, the same defense-in-depth
posture every other Topo plugin applies to its own listings. Asset identity
is each object's AWS-assigned ID (12-digit account ID, `r-`/`ou-`/`o-`
prefixed root/OU/organization ID) — stable and immutable, never the
object's mutable friendly name, matching the project's standing identity
invariant. All four object kinds (Organization, Root, OrganizationalUnit,
Account) map to the existing `model.AssetCloudResource` type (reserved in
`pkg/model` since the storage milestone, unused until now) with
`Attributes["kind"]` distinguishing them, the same generic-type-plus-kind
choice already made for Kubernetes's Node/Pod objects. A single `member_of`
relationship connects every root, OU, and account to its immediate parent,
reused at every hierarchy level rather than one relationship type per
parent/child kind pairing.

**Authentication and target model.** One or more AWS Organizations API
endpoint URLs are `discovery.Request.Targets`, matching the
vCenter/WinRM/SNMP/Kubernetes target-list shape. Authentication is a static
AWS access key ID (plain, non-secret, like a username) plus a secret access
key and optional session token (for temporary/STS credentials) resolved
through the existing credential-reference contract — never accepted as a
CLI argument. `-region` is required and never defaulted or autodetected,
since AWS Organizations is only reachable from specific regional endpoints
depending on partition. Production targets require HTTPS; an explicit
`-lab` flag, restricted to loopback targets exactly like the other
plugins' own Lab flags, permits HTTP against the Topo Lab fixture below.

**Acceptance testing.** AWS has no official local simulator for the
Organizations API. LocalStack was evaluated and rejected: it runs as a
separate Docker container rather than an in-process Go fixture, heavier
than every other Topo Lab fixture and not reliably available in every
build/CI environment. Matching the Kubernetes/SNMP precedent instead: a
hand-rolled Topo Lab fixture (`pkg/lab/aws_organizations_server.go`) serves
the real AWS-JSON-1.1 wire responses for the four actions the plugin
actually calls, dispatched by the real `X-Amz-Target` header AWS itself
uses. Unlike the Kubernetes fixture (which encodes real `k8s.io/api` Go
types directly, since those carry public JSON struct tags),
`aws-sdk-go-v2` generates its (de)serializers from a service model rather
than JSON struct tags, so its types cannot be `encoding/json`-marshaled
directly; the fixture instead defines minimal local structs mirroring the
exact wire field names the generated deserializer expects — confirmed by
reading `aws-sdk-go-v2`'s own `deserializers.go`, then verified empirically
by running the real client against the fixture in this slice's own tests
(a mismatch would have shown up as zero-valued fields, not a passing test).
The wire format and the plugin's real client-side request construction and
response decoding are still genuinely exercised. Authentication is
verified as real AWS SigV4, not a simplified string comparison: the
fixture re-derives the expected `Authorization` header using the SDK's own
`v4.Signer` against the known Lab credential, over exactly the header set
the client's own `Authorization` header claims it signed (its
`SignedHeaders` component), and compares it to what the request actually
presented — the same technique a from-scratch signature verifier would
use, without hand-rolling the HMAC-SHA256 canonicalization itself; a
wrong-secret acceptance test is therefore a real cryptographic failure, not
a bypassed check. `topo lab aws-organizations-serve` exposes it for manual
exploration, matching `kubernetes-serve`. The two-scan idempotency
acceptance test runs the full `examples/lab/clean-500.json` scenario as 500
simulated accounts nested two levels deep under two organizational units
(plus one account attached directly to the root): 506 total assets (1
organization, 1 root, 4 OUs, 500 accounts) and 505 `member_of`
relationships, zero errors, stable and duplicate-free across a repeated
scan and a `store.Memory` save, additionally verified end-to-end via the
CLI at the same scale — the same shape as every prior protocol's
acceptance test. A real AWS Organization was evaluated as an alternative
fixture and deliberately not required for this slice, since it would need
a real AWS account and a nontrivial pre-existing account structure, neither
reproducible the way an in-process fixture is.

**Real-account verification (2026-08-25, post-merge).** The maintainer
provided a free-tier AWS account and a dedicated read-only IAM user for
live testing outside Topo Lab. Before Organizations was enabled, a real
`DescribeOrganization` call correctly returned a real
`AWSOrganizationsNotInUseException`, handled as a clean non-retryable
`aws_organizations_operation` error. After enabling Organizations, the
plugin correctly enumerated the real Organization, Root, and management
account; after a second member account was added, a repeat run picked it
up with no code changes. The exact four-action least-privilege IAM policy
`docs/aws.md` recommends was then substituted for the broader managed
policy on the same IAM user, and discovery produced identical results,
confirming the documented minimum-permission claim empirically rather
than by assertion alone. This remains a single-account-then-two-account
Organization with no organizational units created, so the recursive
`ListOrganizationalUnitsForParent` walk, the `AccessDeniedException`
permission-denied path, the delegated-administrator credential path, and
STS session-token usage are still verified only against the Lab fixture —
matching the SNMP `authPriv`/real-VMware/real-Windows/real-Kubernetes-
cluster posture for those specific remaining gaps. See "Verified against a
real, live AWS account" in `docs/aws.md` for the full account of what was
tested.

**Deliberate non-goals for this slice.** No per-account resource inventory
(EC2 instances, S3 buckets, IAM roles, etc. — a much larger and separately
scoped follow-up); no Service Control Policy or tag-policy content; no
account-creation or account-closure lifecycle events; no cross-account
role assumption built into the plugin itself — a caller who needs to
assume a role resolves the resulting temporary credentials through the
existing credential-reference contract, matching how every other Topo
discovery plugin treats credentials as always explicit. Azure discovery
remains a separate, unstaged slice, not bundled into this one.

### Slice 3 — Azure tenant subscription-structure discovery (implemented, merged)

**Objective.** Extend discovery into an Azure AD (Microsoft Entra ID)
tenant's own subscription structure: which subscriptions exist and how
they are grouped into management groups under the tenant's root
management group, using only read-only `Get`/`List` calls over the real
Azure Resource Manager (ARM) API — no mutating action (create, move, or
delete) is ever issued, matching the read-only posture already
established for VMware, SNMP, Kubernetes, and AWS.

**Scope.** `pkg/discovery/azure` authenticates via the Azure AD OAuth2
client-credentials grant, looks up the configured tenant's own details,
then calls a single recursive `Get` on the tenant's root management group
(`$expand=children&$recurse=true`) to retrieve the whole hierarchy in one
call — unlike AWS's per-parent `ListOrganizationalUnitsForParent`/
`ListAccountsForParent` walk, Azure's API returns the entire tree in one
request. Recursion is still bounded in code to 6 levels, matching Azure's
own real management-group nesting limit, kept as an explicit bound rather
than trusted implicitly — the same defense-in-depth posture AWS's 5-level
OU bound uses. A flat `GET /subscriptions` call enriches the tree's
subscription entries with state and display-name detail.

Asset identity is each object's **full ARM resource path**
(`/subscriptions/{guid}`, `/providers/Microsoft.Management/
managementGroups/{groupId}`, or `/tenants/{tenantId}`) — a deliberate,
Azure-specific refinement of the project's "never use a mutable name as
identity" invariant: Azure automatically creates a "Tenant Root Group"
whose short group name is, by Azure's own convention, identical to the
tenant's own GUID, so a Tenant asset and the root ManagementGroup asset
would collide on a bare-GUID identity even though they are different
resource kinds (caught empirically while testing this slice — the initial
implementation used the bare short name and produced a self-referential
`member_of` relationship where a management group pointed at itself). The
full ARM path disambiguates every object because it encodes the resource
type in its own path, the same way it would for a real Azure user
browsing the portal or CLI. All three kinds (Tenant, ManagementGroup,
Subscription) map to the existing `model.AssetCloudResource` type with
`Attributes["kind"]` distinguishing them, and a single `member_of`
relationship — reusing the same relationship type AWS's hierarchy uses —
connects every management group and subscription to its immediate parent.

**Authentication and target model.** One or more ARM API endpoint URLs
are `discovery.Request.Targets`, matching the vCenter/WinRM/SNMP/
Kubernetes/AWS target-list shape. Authentication is the standard Azure AD
OAuth2 client-credentials grant: a tenant ID, an application (service
principal) client ID (plain, non-secret, like a username), and a client
secret (resolved through the existing credential-reference contract)
exchange for a short-lived bearer token the plugin then presents on every
ARM call. This differs from Kubernetes's model (a long-lived
ServiceAccount token handed in directly): Azure AD access tokens are
short-lived by design, obtained via an app-registration credential rather
than distributed as a static secret, so the plugin performs the full
token-acquisition round trip itself. `-authority-url` is required and
never defaulted beyond its production default
(`https://login.microsoftonline.com`) or autodetected — sovereign clouds
(Azure Government, Azure China) use different authority and ARM hosts.
Production targets require HTTPS; an explicit `-lab` flag, restricted to
loopback targets, skips certificate verification against the Topo Lab
fixture below — but unlike Kubernetes's and AWS's `-lab` modes, it cannot
fall back to plain HTTP: `azidentity` (the Azure SDK's credential package)
unconditionally refuses a non-HTTPS authority host, with no client option
to override it, discovered empirically while building this slice's Lab
fixture. Topo Lab's Azure fixture therefore always serves HTTPS with a
freshly generated, self-signed, loopback-only certificate instead of the
plain-HTTP loopback the Kubernetes and AWS fixtures use.

**Acceptance testing.** Azure has no official local simulator for the ARM
API. Matching the Kubernetes/AWS precedent instead: a hand-rolled Topo Lab
fixture (`pkg/lab/azure_server.go`) serves the real wire responses for
the endpoints the plugin actually calls — the tenant's OpenID Connect
discovery document, the OAuth2 token endpoint, `GET /tenants`, the
recursive management-group `Get`, and `GET /subscriptions` — discovered by
running the real client against a capturing test double and reading its
exact request sequence, the same empirical-then-verified approach used to
confirm AWS's wire field names. As with the AWS fixture, `azure-sdk-for-go`
generates its (de)serializers from a service model without JSON struct
tags, so the fixture defines minimal local structs mirroring the exact
wire field names the generated deserializer expects, confirmed by reading
`azure-sdk-for-go`'s own `models_serde.go` files, then verified empirically
against the real client. Unlike AWS's SigV4 (a per-request signing scheme
that genuinely needs cryptographic re-derivation to test meaningfully),
Azure's ARM API has no per-request signing — a client obtains a bearer
token once via OAuth2 and presents it on every call, so verifying the
`client_id`/`client_secret` pair at the token endpoint and the bearer
token on every ARM call by plain equality is not a simplification here:
it is the real protocol, the same way Kubernetes's bearer-token fixture
check already was. `topo lab azure-serve` exposes it for manual
exploration, matching `kubernetes-serve`/`aws-organizations-serve`. The
two-scan idempotency acceptance test runs the full
`examples/lab/clean-500.json` scenario as 500 simulated subscriptions
nested two levels deep under two management groups (plus one subscription
attached directly to the root) — deliberately mirroring the AWS fixture's
shape: 506 total assets (1 tenant, 5 management groups including the
root, 500 subscriptions) and 505 `member_of` relationships, matching the
AWS slice's numbers by design, zero errors, stable and duplicate-free
across a repeated scan and a `store.Memory` save, additionally verified
end-to-end via the CLI at the same scale. A real Azure tenant was
evaluated as an alternative fixture and deliberately not required for
this slice, for the same reason a real AWS Organization wasn't for the
AWS slice; real-tenant verification remains explicitly unverified,
matching the SNMP `authPriv`/real-VMware/real-Kubernetes-cluster
posture — implemented and tested against a faithful fixture only, not yet
against a genuinely live Azure tenant.

**Real-tenant verification: attempted, pending (2026-08-25).** The
maintainer created a real Azure AD tenant, an app registration
(`topo`), and started the RBAC setup needed to test it — the exact
Azure-side equivalent of the AWS Organizations real-account test above.
It's blocked partway through: assigning the built-in **Reader** role to
the app registration at the Tenant Root Group scope requires Azure RBAC
write access there, which the maintainer's currently-elevated account
does not yet have (Entra ID directory roles and Azure RBAC are separate
systems; the one-time "Access management for Azure resources" elevation
in Entra ID → Properties bridges them but hasn't been completed yet).
Left as an explicit, tracked follow-up rather than silently dropped —
resume by completing that elevation, assigning Reader to the `topo` app
registration at the Tenant Root Group scope, generating a client secret,
and running `topo discover azure` against `management.azure.com` the same
way the AWS test ran against `organizations.us-east-1.amazonaws.com`.

**Deliberate non-goals for this slice.** No per-subscription resource
inventory (VMs, storage accounts, network resources, etc. — the Azure
counterpart to AWS's still-unstaged per-account resource inventory); no
Azure Policy or management-group policy content; no subscription-creation
or subscription-transfer lifecycle events; no credential chaining beyond
the client-credentials grant (no managed-identity or Azure CLI credential
fallback) — credentials are always explicit, matching every other Topo
discovery plugin. This completes the three protocol integrations
`ROADMAP.md`'s M3 line names (Kubernetes, AWS Organizations, Azure).

### Slice 4 — source precedence and asset conflict/freshness visibility (implemented, merged in PR 46)

**Objective.** Make Topo's resolved asset view explainable when more than one
discovery source reports the same stable asset. Preserve each source's latest
claim, select one current value through an explicit deterministic precedence
policy, and show operators both disagreements and observation freshness rather
than silently replacing one source with whichever observation arrived last.

**Deliverables.** The controller accepts an optional ordered
`-source-precedence` list of discovery plugin names (highest priority first).
Every asset claim is namespaced by site, collector, and plugin and retains its
first/latest observation identifiers and timestamps. `GET /v1/assets` remains
backward-compatible at the top level (`id`, `asset`, first/last observation
IDs) while adding the winning source, every contributing source, first/latest
observed timestamps, and field-level conflicts for `name`, identifiers, and
top-level attributes. Sources absent from the configured list rank after every
listed source; ties resolve by latest source observation and then stable source
identity, never request arrival order. SQLite schema version 5 persists and
backfills source claims transactionally from the immutable observation rows;
the memory and SQLite repositories continue to share one conformance suite.

**Acceptance gates.** Both repositories must prove that a lower-priority newer
claim cannot displace a higher-priority older claim; an out-of-order delivery
cannot roll back one source's current claim; equal-priority claims resolve
deterministically; conflicts identify every disagreeing source/value without
treating evidence timestamps as configuration conflicts; and source first/latest
times survive SQLite restart, backup/restore, and migration from every supported
schema. Controller tests must exercise the configured policy through the real
observation and asset endpoints. The exact Go 1.25.13 format, vet, race,
vulnerability, Linux build, and Windows cross-build gates must pass.

**Deliberate non-goals.** This slice does not correlate assets whose canonical
stable IDs differ, configure precedence separately per collector or site,
resolve relationship conflicts, delete claims merely because a later scan omits
an asset, impose a universal stale-after threshold, send alerts, or implement
the later 1K/10K/100K scale gate. Those require separate policy decisions and
must not be implied by visibility into timestamps and same-ID asset claims.

**Verification.** The full `scripts/security-review-checks.sh` gate passes on
the committed-tree candidate under exact Go 1.25.13: formatting and diff
checks, module verification, `go vet ./...`, pinned `govulncheck` with zero
reachable findings, the full race/coverage suite, Linux build, and Windows
amd64 vet/build. Helm lifecycle rendering remains covered by the ordinary CI
chart job; a local Helm binary was not installed in the development
environment, so no local chart-render claim is made.

### Slice 5 — ServiceNow-controlled Topo Relay MVP (implemented and merged in PR 47; experimental predecessor)

**Historical objective.** Let an operator use ServiceNow as the control panel for an
outbound-only Topo Relay installed inside a network segment: start or schedule
one of the Relay's locally preconfigured discovery profiles, observe Relay and
job status in ServiceNow, and reconcile the resulting CIs and relationships
through IRE. This copies the useful deployment shape of a MID Server without
impersonating ServiceNow's proprietary MID Server protocol or making
ServiceNow Topo's internal data model.

This scoped-app approach is retained as implementation/evidence history, not
the required final architecture. Slice 6 supersedes its control plane with
native ECC/MID behavior and requires no Topo application on the instance.

**Deliverables.** Add `topo relay run`, a long-running process that polls a
fixed Scripted REST API beneath the configured ServiceNow instance, advertises
only locally configured profile IDs/capabilities, claims at most one assigned
job at a time, executes the matching compiled-in discovery plugin, publishes
the observation through the existing ServiceNow IRE publisher, and reports a
bounded structured result. The ServiceNow job contains only `discover` plus a
profile ID: targets, host/server identity policy, limits, and credential
references remain in an owner-controlled local JSON configuration file and are
never supplied by a job. The first complete network path covers local and SSH
Linux profiles; later protocol-profile slices reuse the same queue contract.
An AES-256-GCM encrypted, bounded local delivery spool must retain a completed
observation/result before the first IRE attempt so a ServiceNow or network
outage cannot silently lose it. Provide the minimal scoped-application table,
role, Scripted REST resource, scheduled-enqueue, and manual-start definitions
as public repository artifacts and document how to configure a developer
instance without writing CMDB tables directly.

**Acceptance gates.** Tests must exercise a real HTTP polling/result round
trip, fixed path and bearer authentication, redirect refusal, bounded
responses, unknown job/profile rejection, a ServiceNow job's inability to
inject targets/options/commands/credentials, SSH host-key enforcement, local
credential-reference resolution, encrypted-spool retry after IRE or result
report failure, cancellation/deadlines, and repeated-job IRE identity
stability. A full simulated ServiceNow-to-SSH-to-IRE-to-result acceptance test
must finish with one terminal ServiceNow job and stable duplicate-free CIs.
Exact Go 1.25.13 format, vet, race, vulnerability, Linux build, and Windows
cross-build gates must pass. Real-instance validation must be recorded as real
evidence or left explicitly unverified.

**Deliberate non-goals.** This slice does not emulate the standard ServiceNow
MID Server/ECC Queue protocol, appear in the stock Discovery Schedule MID
Server selector, accept arbitrary scripts or network ranges, download plugins,
move secret values through ServiceNow, add WinRM/SNMP/VMware/cloud profiles,
provide multi-Relay failover for one Relay ID, claim production readiness, or
provision the deferred public Homebrew/package repositories and production
signing credentials.

**Verification.** `env GOTOOLCHAIN=go1.25.13
scripts/security-review-checks.sh` passes on the working-tree candidate:
format/diff/module checks, pinned `govulncheck` with zero reachable findings,
the full race/coverage suite, Linux vet/build, and Windows amd64 vet/build.
`node --check` passes for all four ServiceNow server-side JavaScript sources.
Automated evidence is intentionally simulator-backed; real scoped-app import,
OAuth/role configuration, scheduled execution, and IRE publication through the
new Relay remain pending against the available developer instance.

### Slice 6 — native ServiceNow ECC-compatible MID transport (current)

**Objective.** Replace slice 5's custom scoped-application control plane with
an ECC-compatible implementation that installs only on the Topo host and uses
ServiceNow's native MID Server records, selection model, Discovery schedules,
Discovery Status, and ECC Queue. Slice 5 remains a predecessor/experimental
transport until native behavior is proven; it is not the required final
architecture and is not removed in this slice.

**Deliverables.** Add `topo mid run` and a native ECC transport package that
uses the documented direct SOAP endpoint `/ecc_queue.do?SOAP` with a dedicated
ServiceNow user and credential references. The client accepts only an absolute
HTTPS instance origin, refuses credentials/path/query/fragment and redirects,
bounds and cancels every request and XML response, and polls a bounded set of
`output`/`ready` records addressed exactly to
`mid.server.<locally-configured-name>`. It durably journals a claimed record,
uses explicit `processing`/`processed` transitions, prevents two local
processes from sharing one MID identity, resumes an interrupted claim, and
deduplicates a result by `response_to` before inserting a correlated
`input`/`ready` record. The first dispatcher recognizes only the stock
`Heartbeat` topic; every other topic, including generic `Command`,
`SSHCommand`, PowerShell, JavaScript, Groovy, and unknown topics, receives a
bounded correlated denied/unsupported result and is never executed. A faithful
SOAP/ECC simulator exercises this transport in deterministic CI. Registration,
validation, heartbeat XML, and instance-derived liveness behavior must be
captured separately against a real ServiceNow instance rather than inferred
from the simulator.

**Acceptance gates.** Tests cover URL and credential validation, Basic-auth
SOAP requests, redirect refusal, exact agent/queue/state filters, response and
XML depth/record bounds, SOAP faults, claim/restart recovery, local duplicate-
process exclusion, result correlation/deduplication, Heartbeat-only dispatch,
and visible denial of every other topic without executing payload/name/source
content. Exact Go 1.25.13 format, diff, focused SOAP/ECC integration, vet,
race, Linux build, Windows amd64 vet/build, and the pinned security-review gate
must pass. Real evidence must state independently whether Topo appears in the
standard MID list, completes native validation, responds to the instance's
stock Heartbeat probe, and transitions Up/Down; any unobserved point remains
explicitly unverified.

**Deliberate non-goals.** This slice does not implement stock Discovery
`Command`, `SSHCommand`, PowerShell, JavaScript, Groovy, pattern, credential,
attachment, or sensor payload contracts; advertise `ALL` or generic
orchestration capabilities; accept ServiceNow selection metadata as local
authorization; execute any target operation; install a scoped application,
update set, custom table/API/Business Rule/scheduled script; remove the slice-5
predecessor; provision package channels or production signing credentials; or
claim official ServiceNow MID certification. The precise claim is an
ECC-compatible implementation until real interoperability evidence supports
more. A later focused slice must capture one sanitized stock Linux SSH
Discovery transaction from an official MID reference run before translating
any target-bearing topic, and every requested target/range must then intersect
a local allowlist before execution.

### Relationship to the M2.5 gate

M3 implementation proceeds independently of M2.5's two open follow-up
items (independent retest, real package-channel promotion evidence — see
"Completion status" under the M2.5 section above). Neither blocks starting
M3, and starting M3 does not change either item's status: no finding
becomes `Verified` and no production-readiness claim becomes true just
because M3 work has begun.

## Follow-on order

The M2.5 slices above are complete (see "Completion status" under that
milestone); M3's ServiceNow integration was prioritized by enterprise-pilot
evidence after the original Kubernetes/AWS/Azure/source-resolution slices.
The custom scoped-app Relay from slice 5 is retained as predecessor evidence,
not the required final architecture. The current work is slice 6's native ECC
transport and real-instance Heartbeat/registration/validation evidence. Only
after that evidence exists should a focused Linux SSH slice capture and
translate the exact stock Discovery transaction; WinRM, SNMP, VMware, cloud,
and orchestration topics follow separately rather than inheriting guessed
contracts or an `ALL` capability. The remaining larger scale gates leading
toward Topo Graph stay staged independently.

PostgreSQL was evaluated as part of the persistent-storage milestone and
deliberately deferred (see "Storage technology decision" under the
completed-milestone section above) — revisit it once an HA/clustered-
controller deployment shape is actually on the roadmap, not before.

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
