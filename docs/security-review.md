# Security review record

This record summarizes review scope, remediations and verification status.
Deployment controls and confidential reporting are described in
[the security policy](../SECURITY.md).

## Scope and review provenance

The recorded source review targeted immutable commit
`c0cfb7848e6732590002265fccd7cf0fcbd8c7e9`. It covered controller authorization,
enrollment and revocation, credential providers, reviewed discovery operations,
SQLite/backup boundaries, publishers and distribution workflows.

The first external source review was performed using Grok/xAI, with a report
dated 2026-08-23. This is an AI-assisted review record; it is not a commissioned
human penetration-test certification. Maintainer audits and regression tests
provide additional evidence. The full engagement and working reports are
retained privately.

## Remediation status

| Finding | Change | Public evidence | Status |
| --- | --- | --- | --- |
| TSR-2026-001 | Bind each single-use enrollment token to the operator-selected collector ID | [PR #35](https://github.com/Nischoy-ai/topo/pull/35), implementation `0e61e03` | Fixed; independent retest pending |
| TSR-2026-002 | Restrict live SQLite files and sidecars; reject unsafe final symlinks | [PR #37](https://github.com/Nischoy-ai/topo/pull/37), implementation `da08ab3` | Fixed; independent retest pending |
| TSR-2026-009 | Protect the entire backup creation window in a private staging directory | [PR #37](https://github.com/Nischoy-ai/topo/pull/37), implementation `da08ab3` | Fixed; independent retest pending |
| TSR-2026-003 | Route workflow inputs through environment variables; reject raw interpolation in run steps | [PR #38](https://github.com/Nischoy-ai/topo/pull/38), implementation `b69ba8a` | Fixed; independent retest pending |
| TSR-2026-004 | Reject URL userinfo and refuse redirects in authenticated publisher, agent and enrollment clients | [PR #39](https://github.com/Nischoy-ai/topo/pull/39), implementation `cd93790` | Fixed; independent retest pending |

The publisher finding was initially reported as TSR-2026-001 in
[issue #36](https://github.com/Nischoy-ai/topo/issues/36); TSR-2026-004 is its
canonical identifier because the enrollment finding already used TSR-2026-001.
A fix is marked independently verified only after a reviewer retests the exact
remediation commit. Maintainer tests do not change that status.

## Reproducible checks

The current release/security baseline is exact Go 1.26.9. Run:

```sh
scripts/security-review-checks.sh
```

The gate checks formatting, module integrity, vet, pinned vulnerability scanning,
race-enabled tests, builds and Windows cross-compilation. Focused regressions
cover token identity mismatch/races, file permissions and backup staging,
workflow interpolation, URL rejection and credential-safe redirect denial.
CI logs and immutable remediation PRs record results. A historical zero-finding
scan applies to its tested commit and dependency versions; rerun it after changes.

## Release and compatibility evidence

Published beta distribution has been exercised through signed APT/RPM channels
and Homebrew on amd64/arm64. See [distribution evidence](evidence/distribution.md).
Stable/N-1 promotion, Windows publication and Apple notarization remain separate
release work. Protocol guides state their real-host versus simulator coverage.
ServiceNow XML installation and IRE results are scoped in the
[package validation record](servicenow-validation.md) and [IRE evidence](evidence/servicenow-ire.md).

## Build-tool dependencies

The pinned ServiceNow SDK 4.9.0 is a build dependency. The shipped application
has no npm runtime dependency tree. The recorded SDK dependency audit found
nine moderate and two high transitive npm advisories (see the dated
[worker evidence](evidence/servicenow-worker.md)); this remains a build-tool
exposure and is not covered by a zero-reachable-finding Go scan. That historical
audit is not a fresh npm assessment or a claim that these advisories are cleared.
Keep the SDK lock file under review and isolate builds from production secrets.

## 2026-10-08 build baseline update

The [main CI scan](https://github.com/Nischoy-ai/topo/actions/runs/37857414708)
on `5439ef5b441e1145172190db94aa24ba4d809599` found nine reachable advisories
in Go 1.26.8 / `golang.org/x/net` 0.57.0 after earlier PR checks had passed.
The earlier results remain evidence for their scan dates, not a current
vulnerability-free claim. Go 1.26.9 and `x/net` 0.60.0 are the scanner's fixes.
See the [official Go patch release](https://go.dev/doc/devel/release#go1.26.9).

| Advisory | Affected build dependency | Patch required |
| --- | --- | --- |
| GO-2026-6617 | Go `net/http`, `x/net` | Go 1.26.9, `x/net` 0.60.0 |
| GO-2026-6613 | Go `net/http` | Go 1.26.9 |
| GO-2026-6612 | Go `net/http`, `x/net` | Go 1.26.9, `x/net` 0.60.0 |
| GO-2026-6611 | Go `net/http`, `x/net` | Go 1.26.9, `x/net` 0.60.0 |
| GO-2026-6610 | Go `net/http`, `x/net` | Go 1.26.9, `x/net` 0.60.0 |
| GO-2026-6608 | Go `mime/multipart` | Go 1.26.9 |
| GO-2026-6607 | Go `crypto/tls` | Go 1.26.9 |
| GO-2026-6605 | Go `net/http` | Go 1.26.9 |
| GO-2026-6603 | Go `net/http`, `x/net` | Go 1.26.9, `x/net` 0.60.0 |

Release preparation updates the exact compiler pins and module dependency.
Existing worker artifacts are not rebuilt or relabeled. A new signed worker
must pass the full pinned security gate and protected release/promotion review
before being offered as the patched channel build. Changing the XML's maturity
label does not patch a worker or expand its measured compatibility.

The patched preparation worktree passed `scripts/security-review-checks.sh`
on 2026-10-08 with exact Go 1.26.9: module verification, vet, a zero-finding
`govulncheck` v1.7.0 scan, full race tests, native build and Windows vet/build.
This is source validation, not evidence of publication or independent retest.


## Published worker status after the baseline update

The zero-finding source scan above covers the patched source, not the installed
`v0.1.0-beta.1` binary. That historical build used Go 1.26.8 and `x/crypto`
0.56.0. The new `v0.4.6-beta.1` candidate uses Go 1.26.9, `x/net` 0.60.0 and
`x/crypto` 0.57.0. Its [release build](https://github.com/Nischoy-ai/topo/actions/runs/37862256387)
passed scanning, reproduction and package/Mac installation checks, but awaits
protected signing review. No patched public-channel installation is established
yet. [Worker availability](distribution.md#release-availability) records that
boundary. Historical signatures, test results and findings retain their scope.
