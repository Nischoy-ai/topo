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
<https://github.com/Nischoy-ai/topo/pull/35>; the complete pinned
`scripts/security-review-checks.sh` gate passes under exact Go 1.25.13 with
zero reachable vulnerabilities.

This is maintainer triage and remediation, not independent verification. The
finding remains open for independent retest, the other maintainer-audit findings
remain to be remediated or dispositioned under the protocol below, and the M2.5
gate remains open.

## Reproducing the baseline

From a clean checkout of the review commit, run:

```sh
make security-review
```

The script requires exact Go 1.25.13, verifies module checksums and formatting,
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
