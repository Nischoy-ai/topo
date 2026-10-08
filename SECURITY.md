# Security policy

## Versions receiving fixes

The current published worker is **v0.1.0-beta.1**. The current ServiceNow XML
package is **0.4.6 preview 2**. Security fixes are developed on `main` and
published with release notes and compatible application/worker versions.
Use the current package for new installations and verify its release manifest.

## Report a vulnerability

Use [GitHub private vulnerability reporting](https://github.com/Nischoy-ai/topo/security/advisories/new).
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

Worker archives are built reproducibly with exact Go 1.26.8. Release workflows
use pinned actions, restricted tokens, signed checksums, SBOMs and GitHub
provenance attestations. Linux package/repository metadata is signed; protected
signing and promotion environments require review. The published beta has
passed real package-channel promotion and fresh public installation checks on
both Linux and Mac architectures.

The macOS beta is a Homebrew CLI formula without Apple Developer ID signing
or notarization. Windows and stable publication are planned. The manually
published ServiceNow XML preview has checksums but no cryptographic signature
or provenance attestation. Its checksum detects corruption; verify the download
origin as part of your installation process. Never disable platform security
protections to install Topo.

See [consumer verification](docs/releases.md#verify-a-downloaded-release),
[distribution evidence](docs/distribution.md#first-beta-operational-evidence),
and [ServiceNow package validation](docs/servicenow-validation.md).

## Security review

The [security review record](docs/security-review.md) identifies the evaluated
commits, fixed findings, regression evidence and retest status. Public records
distinguish automated/source review from a human penetration assessment and
independently verified remediation. A change of release or documentation does
not close an outstanding retest.
