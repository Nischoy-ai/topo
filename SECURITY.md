# Security policy

## Versions receiving fixes

The current application release is **Topo 0.4.6 Beta**. The patched worker
`v0.4.6-beta.1` is available through signed Linux APT/RPM repositories and
the official Homebrew Beta tap. Upgrade earlier workers using the
[package installation guide](docs/distribution.md#upgrade-an-existing-worker).

Security fixes are maintained on `main`. Record `topo version` when reporting
an issue. Exact source scans and published-build status are retained in the
[security review record](docs/security-review.md).

## Report a vulnerability

Private vulnerability reporting is enabled. Use
[Report a vulnerability](https://github.com/Nischoy-ai/topo/security/advisories/new)
on the repository's **Security → Advisories** page; reports are confidential,
not public GitHub issues. Sign in to GitHub to submit a report.
Include the affected version or commit, configuration, reproduction steps,
expected behavior, and impact. Use synthetic credentials and sanitized data.
Do not publish credentials, customer observations or exploit details in an issue.
Maintainers will triage the report, coordinate remediation and publish an
advisory when appropriate. No response-time SLA is currently offered.

## Credential and target controls

- Discovery executes compiled-in, reviewed operations. Controllers and jobs
  cannot supply arbitrary SSH commands, PowerShell or scripts.
- SSH verifies host keys against an operator-managed `known_hosts` file.
  WinRM, VMware and remote credential providers verify HTTPS certificates.
  Weaker protocol modes are restricted to isolated simulation.
- Targets are explicitly authorized locally. Discovery uses bounded reads,
  deadlines, cancellation and controlled concurrency.
- CLI arguments accept credential references rather than secret values.
  Env/file, Vault KV2 and Kubernetes Secret providers redact secret material
  from errors. Never put credentials in labels, observations or job options.
- ServiceNow stores managed SSH passwords in Password2 fields. A dedicated
  credential-custodian role manages them. Worker identities cannot read the
  credential table; the broker requires an authorized, live task attempt and
  the worker independently checks its local target policy.
- Use separate least-privilege discovery, worker, credential-administration
  and direct IRE publisher identities. Restrict worker OAuth to the seven
  documented custom resources and deny generic Table API access.

See [managed-worker setup](docs/pilot-quickstart.md),
[worker security](docs/servicenow-worker.md), and
[credential references](docs/credential-references.md).

## Controller and storage controls

The ServiceNow-managed worker does not run `topo serve`, create a controller
database or use the controller's API key. ServiceNow authenticates worker
requests with scoped OAuth and stores application configuration, credentials
and discovery history. See [deployment security](docs/deployment-security.md)
for the credential lifecycle and the controls for each deployment mode.

With an API key configured, operator reads and mutations require the bearer
key. Verified collector certificates authenticate only the collector data
plane, bind request identity, and support serial-specific revocation. The
bearer key retains operator authority, including when used on a collector route.
No-key and memory-backed controller modes are for evaluation.

The persistent controller supports one process with SQLite. Topo restricts
database, sidecar and backup permissions, uses transactional migrations, and
provides verified, non-overwriting backup/restore. Database and backup files
are not encrypted by Topo; protect their storage with encryption and OS access
controls. The audit chain detects changes but is not a write-once external log.
Enrollment tokens, heartbeats and individual jobs remain in memory; recurring
schedules and revocations persist with SQLite.

Agent offline spools use authenticated AES-256-GCM encryption with a
credential-referenced key. Certificate rotation requires restarting the agent
to load its renewed files. Recovery from compromise uses revocation and fresh
enrollment. See [storage](docs/storage.md), [enrollment](docs/enrollment.md),
and [agent operation](docs/topo-agent.md).

## ServiceNow publication

Topo uses the documented IRE API with stable source identities rather than
writing CMDB tables directly. Direct publication previews by default; apply
performs non-committing IRE preflight, rejects unsupported mappings and limits
retries. The supported mapping covers computers, network adapters and their
ownership relationships. Register the documented discovery-source choice and
review CMDB identification/reconciliation rules before applying observations.

See [IRE setup](docs/servicenow.md) and
[XML installation and recovery](docs/servicenow-update-set.md).

## Release verification

Release builds produce reproducible worker archives with exact Go 1.26.9. Release workflows
use pinned actions, restricted tokens, signed checksums, SBOMs and GitHub
provenance attestations. Linux package/repository metadata is signed; protected
signing and promotion environments require review. The
[distribution record](docs/evidence/distribution.md#046-beta-worker) identifies
the published build and public-channel installation checks. A source scan does
not patch binaries already installed on customer hosts.

The macOS beta is a Homebrew CLI formula without Apple Developer ID signing
or notarization. Windows and stable publication are planned. The manually
published ServiceNow XML package has checksums but no cryptographic signature
or provenance attestation. Its checksum detects corruption; verify the download
origin as part of your installation process. Never disable platform security
protections to install Topo.

See [consumer verification](docs/releases.md#verify-a-downloaded-release),
[distribution evidence](docs/evidence/distribution.md),
and [ServiceNow package validation](docs/servicenow-validation.md).

## Security review

The [security review record](docs/security-review.md) identifies the evaluated
commits, fixed findings, regression evidence and retest status. Public records
distinguish automated/source review from a human penetration assessment and
independently verified remediation. A change of release or documentation does
not close an outstanding retest.
