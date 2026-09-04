# External security review

This document is the reviewer packet and remediation protocol for the M2.5
external security review gate. Topo is pre-alpha. Preparing this packet and
running maintainer checks does not constitute an independent review, and no
production-readiness claim is permitted until the review, remediation, and
independent retest are complete.

The review must identify one immutable Git commit reachable from `main` as its
target. The final report, every finding, every fix, and every retest must name
that commit or the later remediation commit it evaluated. Reports against an
uncommitted working tree are not acceptance evidence.

## System and trust boundaries

Topo has four security principals: an operator, a controller (Topo Hub), a
collector (Topo Relay or Topo Agent), and each untrusted discovery target or
publisher destination. The supported operational controller is one process
with SQLite persistence. Memory storage, an unset API key, plaintext
controller HTTP, and the `-lab` protocol modes are evaluation-only boundaries.

| Boundary | Hostile or sensitive input | Required security property | Primary implementation and evidence |
| --- | --- | --- | --- |
| Controller HTTP/mTLS | Request bodies, query values, bearer credentials, peer certificates | Bounded parsing; operator/data-plane separation; certificate identity binding; fail-closed revocation | `internal/controller`, `internal/enrollment`, authorization/mTLS/revocation tests |
| Enrollment and recovery | One-time tokens, CSRs, serials, CA files | Single-use bounded tokens; proof of private-key possession; short-lived identity; immutable serial revocation; deterministic rotation/revocation ordering | `internal/enrollment`, `docs/enrollment.md` |
| Agent delivery and local spool | Controller responses, observations, jobs, local spool files | Outbound-only behavior; no arbitrary work; authenticated encryption; bounded storage; tamper rejection | `internal/agent`, `docs/topo-agent.md`, `docs/jobs.md` |
| Credential providers | References, provider responses, bearer tokens, secret bytes | References instead of CLI values; verified HTTPS; no redirects; bounded reads; cancellation; redacted errors; least privilege | `pkg/credentialref`, `docs/credential-references.md` |
| SSH/WinRM/SNMP/VMware discovery | Remote handshakes, protocol frames, inventory output | Compiled-in operations only; target identity verification; deadlines; controlled concurrency; bounded reads; per-target fault isolation | `pkg/discovery`, protocol documentation and integration tests |
| Storage, audit, and upgrades | Observations, policy state, database and backup files | Transactional persistence; non-overwriting verified recovery; migration rollback; durable revocation; tamper-evident audit chain | `internal/store`, `internal/audit`, `docs/storage.md` |
| Publishers | Destination URLs and responses, normalized observations | HTTPS identity verification; bounded responses; stable destination-neutral identity; no direct CMDB-table writes | `pkg/publisher`, `docs/servicenow.md` |
| Build and distribution | Source tags, dependencies, release assets, workflow inputs, signing identities | Reviewed-main provenance; reproducible artifacts; least-privilege workflow tokens; native signing; no per-channel rebuild | `internal/release`, `internal/packagebuild`, `internal/distribution`, `.github/workflows`, release/package/distribution docs |
| Deployment artifacts | Container, Helm, systemd, MSI configuration | Non-root defaults; constrained writable state; no embedded/generated credentials; no silently started service | `Dockerfile`, `packaging/`, package and agent lifecycle tests |

Reviewers should treat discovery targets, destination endpoints, controller
clients, local unprivileged users, package metadata, and every network response
as attacker-controlled. Direct root/administrator access to the controller
host, enrollment CA key, signing credentials, or an external secret store is
outside Topo's process boundary, but unsafe handling of those assets by Topo is
in scope.

## Review scope

The independent review includes:

- source-assisted design review and adversarial testing of every boundary in
  the table above;
- authentication/authorization bypass, collector impersonation, certificate
  enrollment/rotation/revocation races, and recovery behavior;
- request smuggling, parser/resource-exhaustion, SSRF, redirect and TLS
  downgrade, path/archive traversal, unsafe file permissions, and overwrite
  behavior;
- secret exposure through CLI arguments, process configuration, errors, logs,
  observations, audit details, artifacts, and CI output;
- arbitrary-operation injection through controller jobs or SSH, WinRM, SNMP,
  and VMware inputs;
- spool tampering/replay, observation identity confusion, job cross-collector
  access, and schedule pile-up;
- SQLite corruption/migration/backup boundaries and the stated limits of the
  hash-chained audit log;
- release, package, promotion, and deployment automation, including workflow
  permissions and untrusted-input handling.

The following evidence remains deliberately outside this review-preparation
slice, not waived or simulated:

- a real beta and a real N-1 stable package-channel promotion, because the
  external repositories and production signing credentials have not been
  provisioned;
- Microsoft's merge of the first WinGet submission;
- the real-host and real-service compatibility evidence already listed in the
  project handoff (Windows fixtures/SCM, SNMP `authPriv`, VMware, and broader
  ServiceNow classes).

The promotion automation itself remains in review scope. Only provisioning,
publication, and the resulting real-channel evidence are deferred. Cloud and
Kubernetes discovery, HA controllers, PostgreSQL, arbitrary plugin loading,
and other roadmap features that do not exist in this commit are not review
targets.

## Maintainer pre-review baseline

On 2026-08-23, the first `govulncheck` baseline against the previously pinned
Go 1.23.12 release toolchain reported 41 reachable vulnerabilities in the Go
standard library, `golang.org/x/crypto`, and
`github.com/Azure/go-ntlmssp`. The pre-review remediation moved the build and
release baseline to exact Go 1.25.13, upgraded `x/crypto` to `v0.52.0` and
`go-ntlmssp` to `v0.1.1`, and added a pinned `govulncheck v1.7.0` CI gate. The
same scan then reported zero reachable vulnerabilities. Its verbose output
retains one module-level advisory for the unmaintained
`golang.org/x/crypto/openpgp` package; Topo imports `x/crypto/ssh`, not
`openpgp`, so the scanner reports no imported package or reachable symbol for
that advisory. Reviewers should verify that boundary rather than treating it as
silenced.

The same self-audit found that the Vault and Kubernetes credential adapters
accepted plaintext HTTP despite documenting verified TLS. Both now require an
absolute HTTPS base URL, reject embedded credentials/path/query/fragment
components, and refuse redirects so provider bearer tokens cannot be forwarded
to a redirect target. Verified-TLS, plaintext-rejection, redirect-rejection,
bounded-read, cancellation, and redaction tests run under the race detector.

These are maintainer findings and fixes, not independent-review results. The
independent reviewer must evaluate the fixes and may reopen them.

## Maintainer remediation status

A 2026-08-23 maintainer audit of merged `main` commit
`c0cfb7848e6732590002265fccd7cf0fcbd8c7e9` identified
`TSR-2026-001`: enrollment tokens were single-use and time-bounded but were not
actually bound to the collector identity named when they were distributed.
The first focused remediation requires `POST /v1/enrollment-tokens` to name and
validate one `collector_id`, stores that identity with the token, and permits
redemption only for the same identity. A mismatch uses the generic invalid-token
response and does not consume the token. Regression coverage includes the token
store, the real controller API, concurrent correct/mismatched redemption,
bounded request parsing, response identity, and audit redaction/identity.
The implementation is commit `0e61e03` in
<https://github.com/Nischoy-ai/topo/pull/35>, merged to `main` as `30faccb`;
the complete pinned
`scripts/security-review-checks.sh` gate passes under exact Go 1.25.13 with
zero reachable vulnerabilities.

The same audit identified `TSR-2026-002`: under a normal process umask, SQLite
created the live database with default mode `0644`, exposing observations,
resolved state, audit entries, schedules, and revocations to other local users.
The focused fix accepts a filesystem path rather than a SQLite URI, exclusively
pre-creates a missing file and applies mode `0600` before SQLite can open it,
tightens an existing regular file to the same mode, rejects final database and
sidecar symlinks, and opens with SQLite `mode=rw` so deletion cannot trigger an
unprotected recreation. WAL, shared-memory, and rollback-journal sidecars are
protected before and after open; SQLite-created sidecars inherit the main-file
mode.

It also fixes the same root cause in `TSR-2026-009`: `VACUUM INTO` previously
created a complete backup beside its destination and Topo applied mode `0600`
only after the copy finished. Backup and restore staging now occurs beneath a
fresh mode-`0700` directory created before SQLite receives the path. The
completed snapshot is still changed to `0600`, integrity-checked, synced, and
hard-linked without overwrite to its final destination, after which the private
directory is removed. Regression tests set process umask to `0000`, require
`0600` for the live database, WAL/SHM sidecars, and published backup, require
`0700` throughout unpublished staging, verify cleanup, tighten a pre-existing
`0644` database, and reject database/sidecar symlinks. Existing round-trip,
every-schema recovery, migration rollback, corruption, and non-overwrite tests
remain unchanged and passing. The complete pinned
`scripts/security-review-checks.sh` gate passes under exact Go 1.25.13 with
zero reachable `govulncheck` findings, the full race/coverage suite, Linux
vet/build, and Windows amd64 vet/build. The implementation is commit `da08ab3`
in <https://github.com/Nischoy-ai/topo/pull/37>.

A 2026-08-24 maintainer self-audit of the release/distribution automation in
scope (see "Review scope" above: "release, package, promotion, and deployment
automation, including workflow permissions and untrusted-input handling")
found `TSR-2026-003`: four steps in `.github/workflows/promote.yml` (the
Homebrew formula test, and three steps of the WinGet manifest
validation/exercise job) interpolated the operator-supplied
`workflow_dispatch` input `inputs.version` directly into a `run:` shell or
PowerShell script body via a raw `${{ inputs.version }}` expression, instead
of passing it through `env:` and referencing a shell/`$env:` variable.
GitHub's expression substitution happens before the script is generated, so
an actor-controlled string spliced this way is a script-injection primitive
into the runner process — one that in this workflow later imports
`REPOSITORY_SIGNING_PRIVATE_KEY`, `WINDOWS_SIGNING_PFX_PASSWORD`, and other
release-signing secrets as environment variables a hijacked shell could read.
Severity is low, not critical, because of two independent constraints
already in the same workflow at the time of discovery: `workflow_dispatch`
requires repository write access to trigger at all, and an earlier
same-run step (`Validate immutable release and channel inputs`) rejects any
`inputs.version` that does not match
`^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$` before the
four affected steps ever run, which excludes shell/PowerShell metacharacters
from the value that reaches them. No proof-of-concept payload was
constructed or run against a real workflow; the finding is a static-analysis
result plus the general GitHub Actions script-injection primitive
(<https://docs.github.com/actions/security-guides/security-hardening-for-github-actions#understanding-the-risk-of-script-injections>),
not a demonstrated compromise. It is filed and fixed anyway because
`inputs.version`'s constraint lives in a different job step than its use, so
a later edit to that regex, to the `channel`/`previous_version` inputs, or a
refactor that reorders steps could silently reopen it with no change to the
four affected lines. The fix routes `inputs.version` through each step's
`env:` block and references `$VERSION` (bash) or `$env:VERSION` (PowerShell)
instead, matching every other step in the same workflow. A regression check,
`scripts/check-workflow-interpolation.sh`, scans every `.github/workflows/*.yml`
`run:` step for a raw `${{ inputs.` or `${{ github.event.` expression and
fails the build if one is found; it runs in ordinary CI
(`.github/workflows/ci.yml`) on every pull request, not only in
`scripts/security-review-checks.sh`, since a workflow-file change is exactly
the kind of edit CI should already be checking. The implementation is commit
`b69ba8a` in <https://github.com/Nischoy-ai/topo/pull/38>.

These are maintainer triage and remediations, not independent verification.
`TSR-2026-001`, `TSR-2026-002`, `TSR-2026-003`, and `TSR-2026-009` remain open
for independent retest, the other maintainer-audit findings remain to be
remediated or dispositioned under the protocol below, and the M2.5 gate
remains open.

## Independent review

An independent reviewer, Grok (xAI, no commercial/employment/development
relationship with Nischoy-ai), completed the first independent review under
this engagement against immutable target commit
`c0cfb7848e6732590002265fccd7cf0fcbd8c7e9` — the same pre-remediation
baseline the maintainer self-audit above also reviewed — and delivered a
draft technical report dated 2026-08-23. Scope, methodology, and reproduced
baseline evidence (exact Go 1.25.13, `govulncheck v1.7.0` zero reachable
findings, `gofmt`/`vet`/`mod verify`/race suite/Windows cross-compile all
passing) matched the engagement brief above. At the maintainer's direction,
the finding was filed as a public GitHub issue rather than a private
channel.

**Key outcomes:** no critical or high findings. One medium finding. The
reviewer independently verified the core security invariants this document
claims — collector cannot obtain operator authority without the bearer
credential; cross-collector isolation under mTLS; fail-closed revocation;
compiled-in discovery operations only; durable serial revocation; atomic
schema migration; least-privilege packaging defaults — and independently
confirmed the maintainer's pre-remediation `govulncheck`/toolchain and
Vault/Kubernetes strict-HTTPS claims, rather than accepting them from
documentation alone. Real external package-channel promotion evidence was
out of scope by engagement rule, same as it is for every gate in this
document. The full report is retained outside the public repository per the
sensitive-report handling above; this section summarizes it.

**Finding numbering note:** the reviewer's report and GitHub issue
<https://github.com/Nischoy-ai/topo/issues/36> label this finding
`TSR-2026-001`. That ID was already assigned in this document, before the
review began, to the unrelated enrollment-token-scope finding (see
"Maintainer remediation status" above, fixed in PR #35). The reviewer's own
methodology cites reading this document, so the collision is most likely an
oversight rather than a deliberate renumbering; it is corrected here as
`TSR-2026-004` — the next unused ID in this project's register — to keep one
finding per stable ID project-wide. The GitHub issue title and a comment on
it record the same correction for cross-reference. This is a bookkeeping
correction only; it does not change the finding's content, severity, or
evidence.

### TSR-2026-004 — Publisher HTTPS clients follow redirects and accept URL userinfo

**Severity:** Medium. **CWE-601** (URL Redirection to Untrusted Site),
**CWE-200** (Exposure of Sensitive Information). Requires an
operator-configured destination (attacker-controlled or later compromised,
or a URL with embedded credentials), but the boundary was weaker than
Topo's own credential-provider contract (`pkg/credentialref/vault` and
`pkg/credentialref/kubernetes`, both of which already reject userinfo and
refuse redirects) and could leak a bearer token via redirect or embed
credentials directly in a configured URL.

**Affected components (as reviewed):** `pkg/publisher/webhook/webhook.go`
(`Validate`, `PublishBatch`) and `pkg/publisher/servicenow/servicenow.go`
(`Validate`, `PublishBatch`). `Validate` in both checked only
`scheme == "https"` and a non-empty host — not userinfo, path, query, or
fragment — and `PublishBatch`'s default `http.Client{Timeout: 30 * time.Second}`
had no `CheckRedirect` override, so it followed redirects using Go's
default policy. The reviewer also noted a related, lower-risk residual:
`internal/agent/sender.go` and `internal/enrollment/client.go` construct
similarly bare default `http.Client` instances for the (operator-owned)
controller URL.

**Remediation:** `pkg/publisher/webhook/webhook.go` and
`pkg/publisher/servicenow/servicenow.go` now reject a URL with embedded
userinfo in `Validate` (ServiceNow's `InstanceURL` is a base address the
code itself appends a fixed API path to, so it additionally rejects a
non-root path, query, and fragment, mirroring
`pkg/credentialref/vault`'s `validateHTTPSAddress`). Both publishers'
default HTTP client (used whenever `Config.HTTPClient` is nil) now sets
`CheckRedirect` to `http.ErrUseLastResponse`, so a 3xx response is returned
to the caller — and rejected as a non-2xx result — instead of being
followed with the bearer token attached. The related residual is folded
into the same change: `internal/agent/sender.go`'s `NewSender` and
`internal/enrollment/client.go`'s `validControllerURL` (shared by `Enroll`
and `Rotate`) apply the same userinfo/path/query/fragment rejection, and
all three of `NewSender`'s, `Enroll`'s, and `Rotate`'s HTTP clients now
refuse redirects the same way.

**Regression tests:** table-driven `Validate`/`NewSender`/
`validControllerURL` rejection tests for userinfo (and, for the
base-address forms, path/query/fragment); an `httptest`-backed redirect test
per affected client proving the redirect target is never contacted and the
bearer credential is never sent to it. The complete pinned
`scripts/security-review-checks.sh` gate passes under exact Go 1.25.13
(`govulncheck` could not be reached from this sandbox's network policy, not
a code effect; it runs normally in CI), including the full race suite,
Linux build, and Windows amd64 vet/build.

**Status:** Fixed and ready for independent retest — **not** independently
verified. Per the reviewer's own recommended remediation order, closure
requires the original reviewer (or an equivalent independent party) to
retest the exact remediation commit and mark the finding `Verified`; a
maintainer or coding-agent fix alone cannot close a finding that originated
from an independent review, the same rule that already applies to
`TSR-2026-001`/`002`/`003`/`009` above. Implementation:
`cd93790` in <https://github.com/Nischoy-ai/topo/pull/39>.

## Reproducing the baseline

The active post-review build baseline moved to exact Go 1.26.8 and
`golang.org/x/crypto` 0.56.0 on 2026-09-03 after the gate found the newly
published reachable SSH deadlocks `GO-2026-6354` and `GO-2026-6355`. This does
not alter the immutable Go 1.25.13 evidence recorded for earlier review
commits; it is the baseline reviewers should use for new commits.

From a clean checkout of the review commit, run:

```sh
make security-review
```

The script requires exact Go 1.26.8, verifies module checksums and formatting,
runs `go vet`, the pinned `govulncheck v1.7.0` source scan, the full race suite,
the Linux build, and the Windows amd64 vet/build. Govulncheck contacts the
public Go vulnerability database, so record the database modification time in
the review evidence and rerun it at remediation closure. CI runs the same
vulnerability gate on every pull request and push to `main`, and the tag
workflow reruns it before building release artifacts.

The reviewer should retain, privately where necessary:

- target commit, review dates, reviewer identity, methodology, and tool
  versions/database timestamps;
- command output for the baseline and each remediation/retest commit;
- sanitized proof-of-concept steps and affected trust boundary for every
  finding;
- the final report and a finding-to-fix-to-regression-test-to-retest mapping.

Do not put exploit details, credentials, customer data, private reports, or
production signing material in the public repository.

## Finding and remediation protocol

Assign stable IDs such as `TSR-2026-001`. Track each finding privately until
coordinated disclosure is safe, using these required fields:

| Field | Required content |
| --- | --- |
| ID and title | Stable identifier and concise description |
| Severity | Critical, high, medium, low, or informational, with rationale |
| Boundary and impact | Affected component/principal, prerequisite, confidentiality/integrity/availability effect |
| Reproduction | Sanitized deterministic steps and the reviewed commit |
| Status | Open, triaged, fixing, ready for retest, verified, accepted risk, or duplicate |
| Remediation | Owner, fix commit/PR, regression test, and any operator action |
| Closure | Independent retest result, reviewer, date, and evidence reference |

Critical and high findings block every release and require a merged fix,
regression coverage, and independent retest. Medium findings also block the
production-readiness claim unless fixed and retested or explicitly accepted by
the maintainer and independent reviewer with a bounded exposure, compensating
control, owner, and review date. Low/informational findings may be scheduled
only with the same explicit ownership and rationale. A maintainer assertion
alone never marks an independently reported finding verified.

## Gate closure

The M2.5 external-review gate closes only when:

1. an independent reviewer completes the agreed scope against an immutable
   `main` commit;
2. every finding is tracked under the protocol above without secret or exploit
   disclosure in public artifacts;
3. blocking findings are fixed with regression tests and independently
   retested, and any accepted risk meets the documented standard;
4. the final baseline, report reference, remediation commits, and retest
   evidence are recorded in `docs/project-plan.md`; and
5. all other production gates remain represented accurately.

In particular, external review closure does not fabricate or waive the still
required real beta/N-1 package-channel evidence. Until both gates close, Topo
remains pre-alpha with no supported production release.
