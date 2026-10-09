# Deployment security

The ServiceNow-managed installation consists of the scoped application and an
outbound worker. ServiceNow provides user authentication, role enforcement,
configuration and discovery-history storage. The worker does not require a
Topo controller, controller API key, SQLite database or local result spool.

## Where SSH passwords live

For the published managed-worker release, SSH usernames and passwords live in
the customer's ServiceNow instance, in `x_664635_topo_ssh_credential`.
Passwords use **Password2**, ServiceNow's reversible encrypted field type.
This is platform-managed encryption: ServiceNow code with the required
authority can decrypt the field. It is not an external-vault boundary or a
guarantee against a compromised instance administrator. See ServiceNow's
[Password2 key-management documentation](https://www.servicenow.com/docs/r/xanadu/platform-security/platform-encryption/password-2way-encrypted-fields.html)
and have the instance administrator verify the encryption/key configuration
and recovery policy for their platform version.

Only the credential-custodian role administers credential records. Generic
Table API access is disabled. Tasks contain a binding identifier, never the
password. The broker checks the authenticated worker, current attempt, lease,
profile, protocol and target binding before returning a password over HTTPS.
The response is marked `no-store`; the worker uses it in operation memory
and does not persist it. Host keys and target authorization are checked
against files controlled by the worker operator.

Use a dedicated non-privileged SSH account. Rotate its password at the target
and update the protected ServiceNow record, then validate a scan before
resuming schedules. Deactivate the binding and credential, cancel work and
revoke worker OAuth access when retiring a deployment. Include protected
credential recovery in the instance backup/clone procedure; do not export
decrypted secrets for support or migration.

Standalone discovery accepts Vault KV2 and Kubernetes credential references.
The published managed-worker release uses Password2; it does not resolve
managed SSH credentials through Vault. Do not enter a Vault reference as a
Password2 password. Organizations requiring secrets to stay outside ServiceNow
must use an appropriate standalone credential-reference deployment until a
managed provider has its own released implementation and acceptance evidence.

See [worker setup](pilot-quickstart.md), [broker contract](servicenow-worker.md)
and [credential references](credential-references.md).

## Optional standalone controller

`topo serve` is a separate component. If deploying it, require an operator
API key from a credential reference, TLS for remote access, and enrolled mTLS
collector identities. The API key grants operator authority; it is not a
per-user SSO identity. Limit its distribution, protect it as an administrator
credential, and rotate it after suspected exposure. No-key and memory-backed
modes are restricted to isolated evaluation. See [enrollment](enrollment.md).

Place the SQLite database, its WAL/sidecar files, CA private keys, and the
backup staging and destination directories on encrypted, access-controlled
storage. Encryption keys must be managed separately from copied backups.
Permissions protect files from other local accounts; they do not encrypt
files or protect them from privileged host access. Topo does not implement
transparent database or backup encryption. Include encrypted backup recovery
and key recovery in the deployment's restore drill. See [storage and
non-overwriting restore](storage.md).

## Artifact trust and review

APT/RPM authenticate signed repositories and packages; the Homebrew worker
archives have signed checksum and provenance evidence. Follow [release
verification](releases.md#verify-a-downloaded-release) before an offline install.
Linux setup uses native repository configuration, without executing a
downloaded shell installer.

The current Homebrew release does not have Apple Developer ID signing or
notarization. The native XML release currently has checksum evidence; worker
signatures do not cover that XML. A detached signature authenticates only
the bytes its manifest covers; it is not ServiceNow Store certification,
Apple notarization or a security assessment. Never disable operating-system
security protections during installation.

The [security review record](security-review.md) retains exact findings and
verification status. An automated source review cannot be presented as a
commissioned human penetration assessment. Independent finding retests and
any external assessment must identify the reviewed commit and actual scope.
