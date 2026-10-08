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

The current release/security baseline is exact Go 1.26.8. Run:

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
and Homebrew on amd64/arm64. See [distribution evidence](distribution.md#first-beta-operational-evidence).
Stable/N-1 promotion, Windows publication and Apple notarization remain separate
release work. Protocol guides state their real-host versus simulator coverage.
ServiceNow XML installation and IRE results are scoped in the
[package validation record](servicenow-validation.md) and [IRE guide](servicenow.md).
