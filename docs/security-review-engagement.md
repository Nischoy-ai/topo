# Independent security review engagement brief — Nischoy Topo M2.5

This is the outbound engagement brief used to commission the independent
review required by `docs/security-review.md` and `AGENTS.md` current-priority
item 7. `docs/security-review.md` is the reviewer packet (scope, trust
boundaries, baseline, and finding/closure protocol); this document is the
brief sent to the independent reviewer to start the engagement. Preparing and
issuing this brief is not itself an independent review — the review,
remediation, and independent retest remain open until `docs/security-review.md`
gate closure is met.

---

Subject: Independent Security Review — Nischoy Topo M2.5

You are being engaged as an independent security reviewer for Nischoy Topo, an open-source, destination-neutral infrastructure discovery data plane.

Your objective is to perform a source-assisted design review and adversarial security assessment, report vulnerabilities and boundary mismatches, and independently retest remediations. This review is a release gate: maintainer self-assessment alone cannot close it.

## Repository

<https://github.com/Nischoy-ai/topo>

## Immutable review target

`c0cfb7848e6732590002265fccd7cf0fcbd8c7e9`

Confirm before testing that:

1. The checked-out commit exactly matches this SHA.
2. The working tree is clean.
3. All reported findings identify this commit, or a later remediation commit, as their evaluated target.
4. No results are based only on an uncommitted working tree.

Commands:

```sh
git checkout --detach c0cfb7848e6732590002265fccd7cf0fcbd8c7e9
git rev-parse HEAD
git status --porcelain
make security-review
```

## Project status

Topo is pre-alpha and has no supported production release. The purpose of this review is to determine whether the documented security boundaries are correctly implemented and to identify the remediation required before any future production-readiness claim.

Review preparation is complete, but the independent review, remediation, and independent retest are not.

Start with these documents:

- AGENTS.md
- docs/security-review.md
- docs/project-plan.md
- SECURITY.md
- README.md
- ROADMAP.md
- docs/enrollment.md
- docs/topo-agent.md
- docs/jobs.md
- docs/heartbeats.md
- docs/storage.md
- docs/credential-references.md
- docs/ssh-discovery.md
- docs/winrm-discovery.md
- docs/snmp.md
- docs/vmware.md
- docs/servicenow.md
- docs/releases.md
- docs/packages.md
- docs/distribution.md

## System overview

Topo has four primary security principals:

1. **Operator** — Holds the controller bearer credential and performs administrative reads and control-plane mutations.

2. **Controller, or Topo Hub** — Accepts observations, enrolls collectors, issues and rotates certificates, enforces certificate revocation, schedules and dispatches jobs, stores observations and audit state, and exposes operator APIs.

3. **Collector, either Topo Relay or Topo Agent** — Discovers infrastructure and delivers observations. An enrolled collector may authenticate with an mTLS client certificate and is intended to have data-plane authority only.

4. **Discovery targets and publisher destinations** — SSH, WinRM, SNMP, VMware, Vault, Kubernetes API, ServiceNow, webhook, and other remote endpoints must be treated as attacker-controlled.

The supported operational controller shape is one controller process with SQLite persistence. Multi-controller or HA operation is not supported.

## Security invariants to verify

Do not assume these claims are correct merely because they are documented. Verify them and report any exception, ambiguity, unsafe default, or incomplete enforcement.

### Controller authorization

- With an API key configured, operator reads and control-plane mutations require the bearer credential.
- A verified collector certificate alone must not authorize operator endpoints.
- Collector certificates are limited to observation delivery, heartbeats, polling and reporting their own jobs, and certificate rotation.
- A valid bearer credential may still be used on collector endpoints for compatibility and continues to carry operator authority.
- When both a certificate and bearer key are present, the authorization decision must remain deterministic and must not permit identity confusion.
- An mTLS-authenticated observation, heartbeat, job poll, or job result must be bound to the verified certificate identity, regardless of a conflicting collector ID in the request.
- Revoked certificates must not influence identity when a separate bearer credential is used.
- An unset API key is an explicitly documented evaluation mode, not a production mode. Test for accidental transitions into that mode and configuration ambiguity.
- Authentication and authorization errors must not disclose credentials or sensitive internal state.

### Enrollment, PKI, rotation, and revocation

- Enrollment tokens must be single-use, time-bounded, unguessable, and safe under concurrent redemption.
- Invalid CSRs must not consume valid tokens.
- CSRs must prove possession of the private key.
- A collector must not request or rotate into another collector's identity.
- Private keys must be generated locally and never transmitted.
- Certificate purpose, subject, validity, serial generation, and CA verification must be appropriate.
- Certificate rotation must require the current verified client certificate, with no bearer-only fallback.
- Rotation must create a fresh key and serial.
- Revocation records must be immutable, serial-specific, canonicalized consistently, and durable in SQLite.
- Revoked certificates must be rejected from every applicable data-plane endpoint and from rotation.
- Revocation lookup failures must fail closed.
- Rotation-versus-revocation concurrency must conform to the documented single-controller ordering.
- Fresh-token re-enrollment of the same collector identity must recover from compromise without removing the old revocation.
- The TLS listener intentionally permits a handshake without a client certificate so first-time enrollment can occur. Confirm that application authorization does not turn this into an mTLS bypass.
- Topo performs application-layer revocation rather than CRL or OCSP enforcement. Assess whether this boundary is implemented and documented safely.

### Agent, spool, heartbeats, and jobs

- Topo Agent must remain outbound-only and must not open an inbound command channel.
- Controller jobs must select only compiled-in operations. They must never supply arbitrary shell commands, PowerShell, scripts, executable paths, protocol queries, OIDs, or other operation text.
- Job polling and result submission must prevent cross-collector access.
- The documented at-most-once dispatch behavior must not create an authorization or state-confusion vulnerability.
- Recurring schedules must not create an unbounded backlog or uncontrolled polling loop.
- Heartbeat and job behavior must remain independent from the heavier discovery interval where documented.
- The offline spool must use authenticated encryption correctly, reject tampering, preserve intended ordering, and enforce its size bound.
- Review key creation, nonce handling, file permissions, path handling, symlink behavior, replacement behavior, corruption recovery, replay risks, and cleanup.
- Neither spool contents nor errors may expose secrets.
- A hostile controller response must not cause arbitrary execution, unsafe file writes, or unbounded resource consumption.

### Credential references and secret handling

Review the shared `env:`, `file:`, `vault:`, and `k8s:` credential-reference contract.

Verify:

- Secret values are not accepted as ordinary CLI arguments.
- References and resolved values are bounded.
- File references require appropriate absolute paths and regular files.
- Unsafe files, symlinks, special devices, or permission conditions are rejected where required.
- Errors, logs, audit details, observations, command lines, build output, and test output do not disclose resolved secrets.
- Vault and Kubernetes clients require absolute HTTPS base URLs.
- Normal certificate and hostname verification cannot be disabled.
- Provider base URLs reject embedded credentials, paths, queries, and fragments.
- Redirects are not followed, particularly for token-bearing requests.
- Responses and token files are bounded and cancellable.
- Error redaction remains effective for malformed, oversized, and hostile provider responses.
- Kubernetes access relies on the pod service account and can be constrained through least-privilege RBAC.
- Vault token lookup and renewal do not leak or incorrectly forward tokens.
- SSRF, DNS rebinding, URL parsing ambiguities, proxy behavior, redirect behavior, and TLS downgrade paths have been considered.

### Discovery protocols

Review SSH, WinRM, SNMP, and VMware discovery as hostile-protocol parsers and remote-execution boundaries.

Across all protocols, verify:

- Only compiled-in, reviewed operations can execute.
- Targets, jobs, observations, or controller input cannot introduce arbitrary commands, scripts, PowerShell, WQL, SOAP actions, CIM resource URIs, selectors, OIDs, SNMP operations, vSphere managed-object references, or property filters.
- Target and server identity verification is mandatory outside explicit loopback-only Lab modes.
- Reads, records, enumeration pages, outputs, headers, tokens, and cumulative results are bounded.
- Connections and operations use deadlines and cancellation.
- Concurrency is controlled.
- Parser failures and hostile output remain isolated to the affected target.
- Required-operation and optional-operation failure behavior does not permit identity confusion or silent corruption.
- Authentication never silently falls back to a weaker mode.
- Secrets are not included in URLs, observations, errors, or logs.
- Malformed protocol messages cannot cause panics, uncontrolled allocation, infinite loops, or unsafe retry behavior.

Protocol-specific expectations include:

- SSH uses a fixed audited command set and host-key verification.
- WinRM uses fixed CIM/WS-Management operations and one compiled-in software-inventory PowerShell command. Production requires NTLMv2 over HTTPS and must not fall back to Basic authentication.
- SNMP production mode requires SNMPv3 authPriv with SHA authentication and AES privacy. Weaker modes are restricted to explicit loopback Lab mode.
- VMware discovery is read-only, uses a fixed bounded property set, and requires verified HTTPS outside loopback vcsim Lab mode.

### Storage, audit, backup, and migration

Review both Memory and SQLite implementations, remembering that Memory mode is evaluation-only.

Verify:

- SQLite persistence correctly retains observations, relationships, schedules, audit records, and certificate revocations.
- Observation identity and update behavior cannot be abused to create cross-collector confusion or unbounded duplication.
- Database creation and permissions are safe.
- Backup and restore reject corrupt input and never overwrite an existing target.
- Restore uses a new path and cannot be redirected through path traversal, symlink, race, or unsafe filesystem behavior.
- Backups are transactionally consistent and verified before publication.
- All pending schema migrations are atomic as a group.
- A failed migration leaves the prior schema and data intact.
- Newer or unsupported schema versions fail safely.
- Malicious or corrupt database contents cannot cause unsafe file writes, command execution, panics, or silent security-policy loss.
- Certificate revocations cannot silently disappear during migration, backup, or restore.
- Audit-chain verification detects modification, removal, and reordering within its stated model.
- The audit log is only tamper-evident. It does not protect against an attacker who can rewrite the database and recompute the complete chain. Confirm this limitation is accurately enforced and documented.
- Sensitive database and backup contents are not mistakenly represented as encrypted at rest.

### Publishers and identity

Review publisher URL validation, response handling, identity mapping, and ServiceNow integration.

Verify:

- Publisher destinations require HTTPS and verified server identity.
- Redirects, URL credentials, ambiguous URL parsing, and SSRF do not leak bearer tokens.
- Response reads and diagnostics are bounded.
- Diagnostics cannot disclose credentials or uncontrolled sensitive response content.
- ServiceNow publishing uses IRE and does not write CMDB tables directly.
- Stable source-native identity is preserved and IP addresses are never promoted to long-lived device identity.
- Batch deduplication prevents duplicate source identities and duplicate relationships.
- Hostile observation attributes cannot introduce unsafe ServiceNow payload structure, log injection, or secret material.
- No code assumes an unverified ServiceNow response schema.

### Build, CI, release, packaging, and distribution

Perform source-assisted review of:

- `.github/workflows/`
- `Dockerfile`
- `Makefile`
- `scripts/`
- `internal/release`
- `internal/packagebuild`
- `internal/distribution`
- `packaging/`

Verify:

- Workflow permissions follow least privilege.
- Pull-request or other untrusted input cannot reach release credentials or privileged publication paths.
- Tags, versions, commit IDs, filenames, archive paths, workflow inputs, and downloaded artifacts are validated.
- Release tags must resolve to reviewed commits reachable from main.
- Actions are pinned to immutable commits.
- Artifact download and promotion paths resist substitution, path traversal, archive traversal, checksum confusion, and TOCTOU errors.
- Release and package processes do not rebuild or silently replace already verified payloads.
- Reproducibility checks meaningfully compare independent absolute source paths.
- Checksums, SBOMs, signatures, attestations, and provenance bind to the intended artifacts and workflow identity.
- Native signing steps fail closed when required credentials are absent.
- Production secrets cannot enter ordinary pull-request CI.
- DEB/RPM/MSI lifecycle scripts do not embed credentials, unexpectedly enable or start services, destroy operator state, or execute unsafe user-controlled content.
- The container, systemd unit, MSI, and Helm chart implement their documented non-root and least-privilege defaults.
- Helm configuration does not embed secret values and requires an existing Secret where documented.
- Distribution promotion cannot mutate external channels before all verification and lifecycle gates pass.
- Beta and stable channel rules cannot be confused or bypassed.
- Repository-key rotation logic and old/new public-key overlap are safe at the automation level.

### Maintainer findings requiring independent verification

The maintainers identified and remediated two categories before this engagement. Treat these as claims to verify, not as closed independent findings.

**1. Go toolchain and dependency vulnerabilities**

The previous Go 1.23.12 baseline reported 41 reachable vulnerabilities. The
original review baseline used exact Go 1.25.13 and `golang.org/x/crypto`
v0.52.0. The active post-review baseline now uses exact Go 1.26.8,
`golang.org/x/crypto` v0.56.0, `github.com/Azure/go-ntlmssp` v0.1.1, and pinned
`govulncheck` v1.7.0; the 2026-09-03 uplift remediates the newly published
reachable SSH deadlocks `GO-2026-6354` and `GO-2026-6355`.

The current scan reports zero reachable vulnerabilities. A module-level advisory remains for `golang.org/x/crypto/openpgp`, but Topo imports `x/crypto/ssh` rather than `openpgp`.

Confirm:

- The affected package and symbols are not imported or reachable.
- The dependency upgrade did not introduce an unsafe compatibility regression.
- CI and release workflows consistently enforce the intended toolchain and scanner.
- The vulnerability database timestamp and tool versions are retained with the evidence.

**2. Vault and Kubernetes plaintext/redirect behavior**

The adapters previously accepted plaintext HTTP despite documenting verified TLS. They now require strict HTTPS, validate base URLs, and refuse redirects.

Confirm:

- No plaintext, redirect, URL ambiguity, proxy, or alternate construction path remains.
- Tokens cannot be forwarded to a redirect destination.
- Regression tests cover the meaningful behavior rather than only validation helpers.
- TLS verification cannot be disabled indirectly.

## Known and documented limitations

Do not report a documented evaluation-only feature as a vulnerability merely because it is intentionally unsuitable for production. Do report it if the implementation can enter that mode unexpectedly, escape its stated restrictions, expose users to a misleading default, or contradict the documentation.

Known boundaries include:

- An unset controller API key is open evaluation mode.
- Plain HTTP controller operation is for local evaluation; operational deployments require native mTLS or a TLS-terminating reverse proxy.
- Memory storage is evaluation-only and loses revocations and other state on restart.
- SQLite and Topo-created backups are not encrypted at rest; confidentiality depends on filesystem and external storage controls.
- The bearer key remains a shared operator-authority credential and is accepted on collector endpoints for compatibility.
- Revocation is enforced by application authorization, not CRL or OCSP.
- The supported controller is a single process; HA coordination is not implemented.
- Enrollment tokens, heartbeats, and one-off job state remain in memory.
- Job delivery is at-most-once and does not redeliver after dispatch.
- The audit chain is tamper-evident, not tamper-proof or non-repudiable.
- Lab authentication and transport exceptions must be restricted to explicit loopback-only modes.

## Rules of engagement

Unless separately agreed in writing:

- Test only local, isolated, reviewer-controlled environments built from the target commit.
- Do not test Nischoy production systems, GitHub organization settings, third-party services, package catalogs, customer systems, or Internet hosts.
- Do not attempt social engineering, phishing, physical access, credential theft, denial of service against shared infrastructure, or persistence outside the test environment.
- Resource-exhaustion testing must use bounded local limits and stop before affecting shared infrastructure.
- Do not create public issues containing exploit details.
- Do not include credentials, private reports, signing material, customer data, or unsanitized proof-of-concept material in the public repository.
- Immediately report suspected critical or actively exploitable high-severity findings through the agreed private channel.
- Preserve enough sanitized evidence for deterministic reproduction.
- Clearly identify any scope area that could not be tested. Do not substitute simulation or assumption for missing real-system evidence.

## Explicit exclusions and deferred evidence

The following are not authorized by this engagement unless separately approved:

- Provisioning or mutating public APT or RPM repositories.
- Provisioning or using production OpenPGP, Authenticode, Developer ID, notarization, or other signing credentials.
- Creating real beta or N-1 stable promotions.
- Submitting or modifying Microsoft WinGet catalog pull requests.
- Publishing to Homebrew, GHCR, or other external package channels.
- Testing third-party infrastructure without written authorization.
- Accessing private Nischoy repositories or systems.

The promotion automation itself remains in review scope. Its external execution and resulting channel evidence are deferred.

Also outside the current review target:

- Real Windows Server compatibility fixtures and real Windows Service Control Manager validation.
- SNMP authPriv interoperability with real network equipment.
- VMware interoperability with a real vCenter or ESXi host beyond vcsim.
- Broader real-instance ServiceNow validation beyond the documented exercised classes.
- Cloud discovery, Kubernetes discovery, PostgreSQL, HA controllers, arbitrary plugin loading, and features not present in the target commit.

Kubernetes credential-provider behavior is in scope; future Kubernetes infrastructure discovery is not.

## Required methodology and initial response

Before testing, return a short review plan containing:

1. Reviewer identity, organization, and independence statement.
2. Named reviewers and relevant areas of expertise.
3. Confirmed target commit.
4. Proposed dates and estimated effort.
5. Source-review, static-analysis, dynamic-testing, fuzzing, and manual-testing methods.
6. Test environments and operating systems.
7. Scope coverage mapped to the security boundaries above.
8. Any proposed exclusions, assumptions, or access requirements.
9. Private reporting and urgent-escalation channels.
10. Retest availability and expected turnaround.

Do not silently omit a boundary. If effort is insufficient for the complete scope, explicitly identify the uncovered areas so the engagement can be adjusted.

## Finding format

Assign each finding a stable identifier such as TSR-2026-001 and include:

- ID and concise title
- Severity: critical, high, medium, low, or informational
- Severity rationale
- CWE or other relevant taxonomy, where useful
- Reviewed commit
- Affected component, files, symbols, endpoints, or workflows
- Affected principals and trust boundary
- Preconditions and attacker capabilities
- Confidentiality, integrity, and availability impact
- Reproduction steps
- Sanitized proof of concept
- Observed behavior
- Expected secure behavior
- Exploitability and environmental assumptions
- Recommended remediation
- Suggested regression-test strategy
- Detection or incident-response considerations, if applicable
- Status
- Retest result, reviewer, date, remediation commit, and evidence reference

Use these statuses:

- Open
- Triaged
- Fixing
- Ready for retest
- Verified
- Accepted risk
- Duplicate

Report documentation errors and misleading security claims when they materially affect secure deployment, even if the underlying code is functioning as written.

## Severity and closure rules

- Critical and high findings block release and production-readiness claims. Closure requires a merged fix, regression coverage, and independent retest.
- Medium findings also block production readiness unless fixed and retested or explicitly accepted by both the maintainer and independent reviewer.
- A medium accepted risk must identify the bounded exposure, compensating control, owner, expiry or review date, and rationale.
- Low and informational findings require explicit ownership and disposition.
- A maintainer assertion does not mark an independent finding verified.
- Mark a finding Verified only after testing the exact merged remediation commit.
- If a fix only partially addresses a finding, keep the original finding open or create a clearly linked residual finding.

## Required deliverables

1. **Scope confirmation and test plan** — Delivered before active testing.

2. **Baseline evidence** — Include the exact commit, clean-tree confirmation, tool versions, vulnerability-database timestamp, and output from `make security-review`.

3. **Interim critical/high notifications** — Delivered immediately through the private escalation channel.

4. **Draft technical report** — Include all findings, evidence, severity rationale, uncovered areas, and recommended remediation.

5. **Final report** — Include:
   - Executive summary
   - Scope and methodology
   - Target commit and dates
   - Architecture and trust-boundary assessment
   - Complete finding register
   - Positive controls verified
   - Uncovered or partially covered areas
   - Residual-risk assessment
   - Clear statement that external package-channel evidence was not tested
   - Recommended remediation order

6. **Finding register** — Provide a structured CSV, JSON, or equivalent table that can map: finding ID → affected commit → remediation PR/commit → regression test → retest result.

7. **Retest report** — For every remediated or accepted finding, identify:
   - Original finding
   - Remediation commit
   - Tests performed
   - Result
   - Remaining limitations
   - Reviewer and date
   - Evidence reference

## Evidence handling

Retain privately:

- The final report
- Detailed proof-of-concept material
- Raw scanner and test output
- Reviewer notes needed to reproduce findings
- Finding-to-fix-to-retest mapping

Public documentation must contain only sanitized summaries and evidence references approved for disclosure. Do not place exploit details, secrets, private reports, customer data, or signing material in the public repository.

## Questions the final report must answer

1. Can a collector obtain operator authority without the bearer credential?
2. Can one collector impersonate another or access another collector's jobs?
3. Can a revoked certificate continue using any certificate-authorized operation?
4. Can enrollment, rotation, or their races mint an unintended identity?
5. Can any controller or job input cause arbitrary remote execution?
6. Can malicious protocol or provider responses cause unbounded resource consumption, panic, secret disclosure, or unsafe retries?
7. Can credentials escape through redirects, logs, errors, process arguments, artifacts, or diagnostics?
8. Can spool, database, backup, restore, or package paths be abused for traversal, overwrite, symlink, or integrity attacks?
9. Can release or promotion automation publish substituted or unverified bytes?
10. Can untrusted pull-request or workflow input reach privileged credentials or publication actions?
11. Do deployment artifacts uphold their documented least-privilege defaults?
12. Are any documented production/evaluation boundaries inaccurate, ambiguous, or easy to enter unintentionally?
13. Which risks remain after remediation, and what explicitly prevents a production-readiness claim?

## Gate closure

This review gate closes only when:

1. The agreed scope has been independently completed against an immutable main commit.
2. Every finding has a stable record.
3. Blocking findings have merged fixes and regression tests.
4. The reviewer has independently retested those exact remediation commits.
5. Accepted risks meet the documented standard.
6. The final report and retest evidence are retained.
7. Sanitized report references and remediation evidence are recorded in `docs/project-plan.md`.
8. Remaining production gates continue to be represented accurately.

Closing this review does not waive or fabricate the separately required real beta and N-1 package-channel promotion evidence. Until both gates close, Topo remains pre-alpha and must not be represented as production-ready.
