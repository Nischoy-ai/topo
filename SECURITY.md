# Security policy

Nischoy Topo is pre-alpha and has no supported production release yet. Report vulnerabilities privately to the maintainers; do not open a public issue containing exploit details or credentials.

## Trust boundaries

Collectors and agents process data from untrusted infrastructure. Destination APIs and discovery targets must be treated as hostile. Plugins must validate all configuration, use bounded reads and deadlines, avoid locally constructed or user-supplied shell text, redact secrets, and return structured errors. A plugin must never accept arbitrary commands from the controller.

The controller's bearer-key authentication is an evaluation bootstrap, not the final enterprise trust model. Operator and collector authorization are separated for certificate-authenticated collectors: operator reads and control-plane mutations require the bearer key, while collector certificates are limited to the data plane. Individual collector certificates can be revoked durably by serial number. Before production readiness, Nischoy Topo still requires encrypted persistent secrets, signed plugin manifests, completed real package-channel promotions, and external penetration testing. Raw release archives now have reproducible builds, an SBOM, keyless signatures, and provenance; DEB/RPM/Helm/offline artifacts preserve those verified payloads; and production release automation fails closed without RPM, Authenticode, Developer ID, and notarization credentials. The channel automation and rotation boundary are implemented, but no production key or public promotion has yet exercised them. The reviewer scope, maintainer pre-review findings, and remediation/closure rules are in [External security review](docs/security-review.md); that preparation is not an independent assessment.

## Deployment guidance

- Bind evaluation controllers to localhost or a private management interface.
- Use a long random API key and TLS-terminating reverse proxy.
- Use dedicated read-only discovery identities and restrict targets by allowlist.
- Verify SSH host keys with a managed `known_hosts` file. `-insecure-host-key` exists only for isolated Topo Lab evaluation.
- Require HTTPS with normal certificate and hostname verification for non-Lab WinRM targets. Production NTLMv2 never falls back to Basic authentication. Basic authentication and HTTP are restricted to the explicit loopback-only Topo Lab mode.
- Require SNMPv3 `authPriv` with SHA authentication and AES privacy for non-Lab SNMP targets; there is no weaker fallback. `noAuthNoPriv` is restricted to the explicit loopback-only Topo Lab mode.
- Require HTTPS with normal certificate verification for non-Lab VMware targets; there is no fallback. `-lab` (HTTP, skipped certificate verification) is restricted to loopback `vcsim` targets. Use a read-only vCenter role — the plugin never issues a configuration, power, or lifecycle operation.
- For a persistent controller, use `-db-driver sqlite -db-dsn <path>`. Topo creates or tightens the live database and SQLite sidecars to POSIX mode `0600`, rejects a symlink as the final database/sidecar path, and stages backups inside a mode-`0700` private directory before publishing a mode-`0600` file. Keep the containing directories non-writable by untrusted users; on Windows, apply an NTFS ACL for the Topo service identity because Go file modes do not replace ACLs. The default `-db-driver memory` loses all discovery data, audit log entries, recurring discovery schedules, and certificate revocations on every restart. Losing revocations re-enables otherwise valid compromised certificates, so memory mode is not an operational compromise boundary. Before every binary/package upgrade, run `topo storage backup` with the currently installed binary and retain the verified snapshot. Restore only to a new path while the controller is stopped; never overwrite the failed/upgraded database. There is no encryption at rest or in Topo-created backups yet — copy them into encrypted, access-controlled storage and treat every database file as sensitive. See [backup, restore, and upgrade procedures](docs/storage.md#backup-and-restore).
- The audit log (`GET /v1/audit`) is tamper-evident, not tamper-proof: it detects an edited, reordered, or removed entry via `internal/audit.VerifyChain`, but does not prevent someone with direct database access from rewriting the whole chain undetected if they also have write access to `-db-dsn`. Treat database file access as equivalent to audit log access; export or forward `GET /v1/audit` to a separate, append-only sink if you need audit records to survive a compromise of the controller host itself.
- Never place credentials in job options, labels, logs, or observation attributes.
- Pass credential provider references, never credential values, as CLI arguments. Restrict credential-file permissions to the Topo process identity.
- Review ServiceNow IRE preview output before enabling destination writes,
  and configure identification/reconciliation rules for every CI class Topo
  emits; see [ServiceNow publishing](docs/servicenow.md).
- Before installing a downloaded raw release, verify both its `SHA256SUMS`
  keyless Sigstore bundle and its GitHub artifact attestation; checking an
  unsigned digest alone detects corruption but not an attacker who replaced
  both the artifact and digest. See [release artifacts and
  verification](docs/releases.md#verify-a-downloaded-release).

## Release supply chain

Semantic release tags must resolve to a commit already reachable from `main`.
The tag workflow uses exact Go 1.25.13, CGO disabled, path/VCS stamping removed,
fixed archive metadata, and two independent source paths; any byte difference
blocks publication. Release actions are pinned to immutable commit digests.
`SHA256SUMS` is signed keylessly by the tag workflow's GitHub OIDC identity, and
GitHub stores signed SLSA provenance and SPDX SBOM attestations for the archive
digests. The workflow verifies both signature and provenance before it creates
the GitHub Release, and no long-lived general artifact-signing secret is stored
in Actions.

Consumers must constrain Sigstore verification to the exact Nischoy Topo
release workflow identity and GitHub Actions OIDC issuer, then verify the
individual archive against the authenticated checksum manifest. GitHub
attestation verification independently binds its digest to this repository,
commit, tag event, and workflow. Release evidence is additive: it does not
replace APT/RPM repository OpenPGP keys, Windows Authenticode, macOS code
signing/notarization, or their key-rotation processes. Production release jobs
isolate and require all three native identities; protected promotion jobs
verify them, add signed repository metadata, and expose an old/new public-key
overlap mechanism. Those controls remain unverified in production until real
beta and N-1 stable promotions pass. Repository private keys never enter
ordinary CI, and distribution tokens have no Topo source write scope. See
[release artifacts and verification](docs/releases.md) and
[package-manager distribution](docs/distribution.md).

## Controller authorization boundary

When `topo serve` is configured with `-api-key-ref`, the bearer key is the
operator credential. It is required for inventory/audit reads, collector
status reads, enrollment-token issuance, certificate revocation/listing, job
creation/status reads, and all schedule operations. A verified enrolled certificate without the bearer key
receives `403 Forbidden` from those endpoints. The same certificate can
deliver observations, send heartbeats, poll and report its own jobs, and
rotate itself; its subject binds the collector identity for each of those
identity-bearing operations, including observation delivery.

The bearer key remains accepted on collector endpoints for compatibility with
agents that have not enrolled. It therefore still carries operator authority:
do not distribute it to a collector when certificate-only least privilege is
required. If no API key is configured, both endpoint classes retain the
existing unauthenticated evaluation behavior; do not expose that mode to an
untrusted network. `POST /v1/enroll` is authenticated by its one-time token,
`POST /v1/rotate` is certificate-only, and `GET /healthz` is open. Revoking a
certificate does not revoke a separately possessed bearer key; rotate that
key too when an incident may have exposed both credentials.

## SSH discovery

The Linux SSH plugin never accepts a command from a controller job. Its commands are compiled into the binary and matched exactly by the Topo Lab SSH frontend. Passwords and private keys are resolved through `env:` or absolute-path `file:` references; neither is accepted as a command-line value or emitted in observations. Each command has a deadline and a bounded output buffer. Package and service permission failures produce partial inventory, while failures of identity or hardware commands reject that target's inventory.

## WinRM discovery

The Windows plugin's operation set consists of compiled-in WS-Management action, CIM resource URI, and WQL tuples plus one compiled-in PowerShell command for software inventory. Targets and jobs cannot provide SOAP actions, resource URIs, queries, PowerShell, or command text. The Topo Lab frontend independently matches the same exact tuples and command argument vector, rejects mismatched SOAP body operations, filters, selectors, shell options, enumeration contexts, and command IDs, and refuses arbitrary executables. Optional volume, service, and patch collection uses fixed, read-only CIM queries. Software collection reads only the 64-bit and 32-bit machine-wide uninstall registry views; it does not use `Win32_Product`, collect uninstall command strings, or inspect per-user hives.

Non-Lab targets must use HTTPS; Go's standard TLS hostname and certificate verification remains enabled. Production `ntlm` mode implements NTLMv2 over server `NTLM` or `Negotiate` challenges, disables HTTP/2 to retain connection affinity, caps authentication headers and tokens, and never answers a Basic-only challenge. It does not implement Kerberos/SPNEGO. The CLI resolves the password from an `env:` or absolute-path `file:` reference rather than a value flag. Lab Basic remains explicitly limited to loopback Topo Lab endpoints.

Each CIM or software operation has a deadline, responses and cumulative command output are bounded, enumeration pages, receive messages, objects, and software records are capped, and target concurrency is controlled. Remote shell and command identifiers are length- and control-character-checked before reuse, and created shells are deleted after command completion or failure while the operation context remains active. Required identity/hardware failures reject that target; optional network, volume, service, patch, or software permission and parse failures retain a partial host and identify the affected operation. The concurrent mixed 500-Linux/500-Windows simulated acceptance gate passes; reviewed real-system compatibility fixtures are still required before claiming the Windows milestone complete.

## SNMP discovery

The SNMP plugin queries a fixed, compiled-in set of MIB-II OIDs (`system` and `interfaces` groups only); targets and jobs cannot supply an OID, community string, or arbitrary SNMP operation. Asset identity is the SNMPv3 engine ID discovered during the USM handshake, never an IP address. Production requires `authPriv` — SHA authentication and AES privacy — with no fallback to `authNoPriv` or `noAuthNoPriv`; those weaker levels are accepted only with the explicit `-lab` flag and a loopback target, the same restriction pattern as WinRM's Basic-authentication Lab mode. Authentication and privacy passphrases resolve through the same `env:`/`file:`/`vault:`/`k8s:` credential-reference contract as every other Topo secret and are never accepted as command-line values. The interface-table walk is bounded to 4096 entries so a malformed or hostile agent cannot force unbounded memory use. `authPriv` uses gosnmp's own client-side USM implementation and has not yet been verified against a real device — Topo Lab's `noAuthNoPriv`-only hand-rolled agent proves the plugin's own message framing and parsing logic, not interoperability with real network equipment. See [SNMP network device discovery](docs/snmp.md).

## VMware discovery

The VMware plugin creates a read-only property-collector container view scoped to `HostSystem` and `VirtualMachine` objects and retrieves a fixed property set; targets and jobs cannot supply a managed object reference, property filter, or any write operation — no configuration, power, or lifecycle operation is ever issued. Asset identity is a host's hardware UUID or a VM's VC-managed instance UUID (falling back to its BIOS UUID only for a standalone ESXi host with no vCenter to assign one), never an IP address or vCenter inventory path. Production requires HTTPS with normal certificate verification; `-lab` (HTTP, skipped certificate verification) is restricted to loopback `vcsim` targets, the same restriction pattern as WinRM's Basic-authentication Lab mode and SNMP's `-lab`. The username and password resolve through the same `env:`/`file:`/`vault:`/`k8s:` credential-reference contract as every other Topo secret and are never accepted as command-line values or embedded in a target URL — a target containing credentials is rejected outright. Host and VM listings are each bounded to 100,000 objects. Listing hosts is required; listing VMs is optional and degrades to host-only inventory on failure rather than failing the whole target. Real vCenter/ESXi verification has not been performed — the two-scan idempotency and fault-isolation acceptance tests run against `govmomi/simulator` (`vcsim`) with TLS and real credential enforcement deliberately turned on, not a real vCenter. See [VMware vCenter discovery](docs/vmware.md).

## Persistent storage and the audit log

The controller's storage backend (`store.Repository`) is opt-in persistent: the default `-db-driver memory` keeps every prior release's behavior exactly (nothing survives a restart), and `-db-driver sqlite -db-dsn <path>` opts into a SQLite-backed store that does. There is no encryption at rest — the database file's confidentiality depends entirely on filesystem permissions, the same trust boundary this project already places on the enrollment CA's private key and Topo Agent's offline spool. Topo establishes owner-only POSIX modes before SQLite opens a new live database or starts copying a backup rather than trying to repair exposure afterward; the containing directory/Windows ACL remains an operator boundary. Enrollment tokens, collector heartbeats, and one-off job state remain in-memory only regardless of `-db-driver`; a controller restart still invalidates outstanding enrollment tokens and loses heartbeat/job history. Recurring discovery schedules and certificate revocations are persisted with SQLite because losing either is a silent policy/security change. `SaveObservation` is idempotent by observation ID in both backends — a collector retrying a delivery whose response was lost replaces the existing record rather than creating a duplicate, so retried delivery cannot be used to inflate stored observation counts.

`-source-precedence` is a resolution policy, not an authorization boundary.
It determines which plugin's same-ID asset claim appears as the current value
and exposes every competing claim and timestamp through operator-only
`GET /v1/assets`; it does not make a source trusted to authenticate requests or
let one collector act as another. mTLS observation identity remains bound to
the verified certificate subject. Bearer-key compatibility still carries its
documented broader authority, so an operator must protect that key rather than
assuming a high precedence rank makes bearer-submitted data trustworthy. See
[source precedence and asset freshness](docs/source-resolution.md).

`topo storage backup` creates a transactionally consistent SQLite snapshot
inside an already-private staging directory, verifies it, and refuses to
replace an existing destination. `topo storage
restore` validates the source read-only and publishes a verified owner-only copy
at a new path. Pending schema upgrades commit in one transaction; a later
migration failure leaves the database at its starting schema. Topo deliberately
does not implement reverse migrations or a force-restore mode: rollback uses the
old binary and a pre-upgrade backup restored to a new path, preserving the
failed database for diagnosis. Database backups contain persisted security
state, including audit entries and certificate revocations, and therefore have
the same confidentiality and integrity sensitivity as the live database.

`GET /v1/audit` returns a hash-chained log of enrollment token issuance, collector enrollment, certificate rotation/revocation, job creation, and schedule changes. Each entry's hash covers its own content and the previous entry's hash, so `internal/audit.VerifyChain` detects an entry edited, reordered, or removed after the fact — a Merkle/hash-chain class guarantee, not cryptographic non-repudiation, and not protection against someone who can rewrite the underlying database file wholesale (see "Deployment guidance" above). Audit detail values are always short strings and never secret material: an enrollment token is referenced only by a truncated SHA-256 fingerprint, never the token itself. Appending the hash-chain entry is best-effort; the durable `certificate_revocations` row is the authoritative enforcement record even if its supplementary audit append fails. See [Persistent storage and the audit log](docs/storage.md).

## Credential references

The shared resolver accepts `env:<name>`, `file:<absolute-path>`,
`vault:<path>#<field>`, and `k8s:[<namespace>/]<secret-name>#<field>`. It
bounds references to 4 KiB and resolved values to 1 MiB, accepts only regular
files, preserves credential bytes exactly, and never includes a resolved value
in an error. Consumer-specific validation applies tighter limits where needed.
Environment variables can be exposed through process inspection or inherited
by child processes on some systems, so restricted mounted files, Vault, or
Kubernetes Secret references are preferred for managed deployments. The Vault
provider verifies the Vault server's TLS identity and never disables
certificate verification; connection settings (address, token, mount) come
from environment variables, never the reference itself. The Kubernetes
provider authenticates in-cluster with the pod's own service account token
and relies entirely on that service account's RBAC grants for least-privilege
scoping — Topo does not enforce which Secrets may be read beyond what
Kubernetes itself authorizes.

The Vault and Kubernetes API adapters require absolute HTTPS base URLs and
normal server-identity verification. They reject URL credentials, paths,
queries, and fragments and do not follow redirects, so provider bearer tokens
cannot be sent in plaintext or forwarded to a redirect target. Their responses
and token-file reads remain bounded and cancellable.

## Topo Agent

The outbound-only agent (`topo agent run`) only makes outbound HTTPS/HTTP
requests to a configured controller URL; it never listens for inbound
connections and never accepts jobs or remote commands. It authenticates with
the same bearer API-key contract as any other controller client. When the
controller is unreachable, observations are buffered to a local spool
encrypted with AES-256-GCM using a key from the same credential-reference
contract (never a CLI argument); spool files and their directory are created
with owner-only permissions, tampering is detected via AEAD authentication
rather than silently returning corrupted data, and total spool size is
bounded so an extended outage cannot grow it without limit. Collector
enrollment and outbound mTLS now exist (see below): `topo agent run
-mtls-cert-dir` authenticates with the enrolled certificate instead of, or
alongside, the bearer API-key contract.

The packaged Linux systemd unit (`packaging/systemd/topo-agent.service`)
runs as a dedicated non-root system user with an empty capability set,
`NoNewPrivileges=yes`, `ProtectSystem=strict`, and the other hardening
directives verified with `systemd-analyze verify`; only its `StateDirectory`
is writable. `topo agent install`/`uninstall` on Windows register the
service with automatic start and restart-on-failure and never write a
resolved secret value into the service's persisted command line — only the
credential *reference* (`env:`/`file:`/`vault:`/`k8s:`) is stored, matching
every other Topo credential consumer. Windows service registration is
verified by cross-compilation and code review only; it has not yet been
exercised against a real Windows Service Control Manager, so treat it as
unverified on real Windows until that gate closes, the same posture already
applied to WinRM real-host compatibility.

## Collector enrollment

The controller can act as its own certificate authority (ECDSA P-256,
self-signed, `-ca-dir`) and issue collectors short-lived (90-day) client
certificates through `POST /v1/enrollment-tokens` (existing bearer-key
auth, mints a single-use one-hour token bound to an operator-selected
collector ID) and `POST /v1/enroll` (redeems the token only for that identity
and signs a submitted CSR). A request for another collector identity is
rejected without consuming the token. The private key is generated on the
collector and never transmitted; only the CSR — a public key plus a
signature proving possession of the private key — crosses the network. A
malformed enrollment request is rejected before the token is redeemed, so
it cannot burn a valid token. The CA's own private key is protected by
filesystem permissions, matching every other private key in this project,
not a second application-level encryption layer. The token store is
in-memory only and does not survive a controller restart. A collector
certificate can be revoked early by its exact serial through operator-only
`POST /v1/certificate-revocations`; `GET /v1/certificate-revocations` lists
the immutable records. With SQLite, these records survive controller restarts;
the memory backend loses them and is evaluation-only. Enrollment is opt-in and does not change any existing
bearer-key-authenticated behavior when `-ca-dir` is not set.

The issued certificate now authenticates live traffic: `topo serve -mtls`
runs a native TLS listener, issuing itself a server certificate from the
same CA (1-year TTL, reissued fresh on every start rather than persisted
or renewed while the process keeps running — collector certificate
rotation, below, does not change that, since the server certificate is
never persisted in the first place), and verifies client certificates
presented against that CA. A request with a certificate verified during
the TLS handshake reaches collector data-plane endpoints without the bearer
API key; it does not gain operator authority.
The TLS layer still accepts a handshake with no client certificate at all
— a collector's first-ever request, `POST /v1/enroll`, has none to present
yet, authenticating instead with its one-time enrollment token — so
per-endpoint enforcement happens in application-layer middleware, not the
TLS handshake itself.
`topo agent enroll -controller-ca-cert` pins the controller's self-signed
CA certificate (distributed out-of-band alongside the enrollment token) so
the bootstrap enrollment request itself can complete against an `-mtls`
controller, whose certificate an ordinary HTTPS client would otherwise not
trust.

A collector's certificate can be renewed before its 90-day expiry with
`POST /v1/rotate`, authenticated by the certificate being renewed rather
than a new token — and deliberately with no bearer-API-key fallback for
this one endpoint, since accepting the shared key here would let any
holder mint a certificate for any collector ID, defeating per-collector
identity entirely. The reissued certificate's identity always comes from
the peer certificate the TLS handshake already verified, never from
anything the client claims in the CSR or request body, so a collector can
only ever rotate its own certificate. Rotation generates a fresh key pair,
not just a fresh certificate for the existing key. `topo agent rotate` is
the collector-side command; it overwrites the same certificate directory
`topo agent enroll` wrote, and a running `topo agent run` must be
restarted afterward to pick up the renewed certificate — it is loaded once
at startup, not reloaded live. Rotation leaves the old certificate valid to
avoid a lost-response lockout; after verifying the new serial, the operator
should explicitly revoke the old one. A revoked certificate cannot rotate.
Revocation-versus-rotation races are linearized within Topo's supported
single-controller process: a rotation already authorized finishes before a
competing revocation returns, while a revocation that wins first makes the
rotation return 401. See
[Collector enrollment](docs/enrollment.md).

Revocation is enforced in application authorization after the CA-verifying TLS
handshake: a revoked serial receives 401 on collector certificate endpoints,
including rotation, and a revocation-store lookup failure fails closed with
503. Topo does not publish a CRL or OCSP responder, so the TLS handshake itself
can still succeed. Native `topo serve -mtls` must receive the peer certificate;
a TLS-terminating reverse proxy that does not forward a cryptographically
trustworthy identity cannot enforce this boundary. Revocations are immutable
and serial-specific. Compromise recovery is a fresh one-time token and
re-enrollment of the same collector ID, producing a fresh key and serial; there
is deliberately no unrevoke operation. See
[Revoking and recovering a certificate](docs/enrollment.md#revoking-and-recovering-a-certificate).

## Collector heartbeats

`POST /v1/heartbeats` is a lightweight liveness signal, distinct from
observation delivery, so the controller can tell a collector is alive
without waiting on the discovery/delivery interval, which is often 15
minutes or longer. Unlike `POST /v1/rotate`, it accepts either the bearer
API key or a verified mTLS client certificate — the collector data-plane
authorization policy — since a heartbeat only asserts liveness rather than
getting new certificate material issued to
it, so there is no analogous "any bearer-key holder can impersonate any
collector" risk to guard against. When a verified peer certificate is
present, its subject still overrides whatever `collector_id` the request
body claims, matching the same identity rule as certificate rotation, so
a collector authenticated by mTLS can never appear alive under a
different collector's identity; a bearer-key-authenticated heartbeat has
no such stronger signal and is recorded under whatever `collector_id` the
body states. Operator-only `GET /v1/collectors` lists every collector's most
recent heartbeat and whether it falls within a fixed three-minute staleness
window. Heartbeat state is in-memory only, like the enrollment token
store, and does not survive a controller restart. A failed heartbeat is
logged and dropped — never spooled or retried the way a failed
observation delivery is — since a stale heartbeat has no lasting value
once the next one supersedes it. Heartbeats are always available, unlike
enrollment/mTLS/rotation: they require no CA, no `-mtls`, and no opt-in
flag, only whichever credential a collector already presents. See
[Collector heartbeats](docs/heartbeats.md).

## Job delivery

Topo Agent is deliberately outbound-only and never accepts inbound
connections, so a controller cannot push work to it; instead an operator
queues a job with `POST /v1/jobs`, and the collector picks it up by
polling `GET /v1/jobs` on the same `-heartbeat-interval` cadence it
already uses for liveness heartbeats. `GET /v1/jobs` marks a job
dispatched the moment it is returned, so a job is delivered at most
once — there is no redelivery if a collector crashes between polling and
reporting a result via `POST /v1/jobs/{id}/result`. Polling and result
reporting are identity-bound the same way as `POST /v1/rotate` and
`POST /v1/heartbeats`: a verified mTLS peer certificate's subject always
overrides whatever `collector_id` the caller claims in a query parameter
or request body field, so a collector can only ever poll for and report
its own jobs, never another collector's; a bearer-key-only request has no
such stronger signal and uses the claimed value as-is, the same
limitation heartbeats already have. `POST /v1/jobs` itself — queuing a
job for a collector — and `GET /v1/jobs/{id}` are operator endpoints and
require the configured bearer key; a collector certificate alone is not
accepted. There is exactly one job type, `discover`, since it
is the only real capability `topo agent run` has; a request for any
other type is rejected at creation, not silently accepted and left
unrunnable. Job state is in-memory only, like the enrollment token store
and heartbeat store, and does not survive a controller restart. See
[Job delivery](docs/jobs.md).

## Server-side recurring discovery scheduling

`POST /v1/schedules` lets an operator set a recurring `discover` cadence
for a collector, upserted and keyed by `collector_id` (at most one
schedule per collector); `GET /v1/schedules` lists every schedule, and
`DELETE /v1/schedules/{collector_id}` removes one. All three are operator
endpoints and require the configured bearer key; a collector certificate
alone is not accepted.
`interval_seconds` is bounded to between 60 and 604800 (one week) — below
the minimum a misconfigured schedule could hammer a collector on every
poll, and there is no server-side rate limit protecting against that
beyond this bound. There is no background ticker: a schedule only becomes
an actual job the moment its collector next polls `GET /v1/jobs` and the
schedule is found due, reusing job delivery's existing
collector-initiated-polling trust model exactly (see "Job delivery"
above) rather than introducing a second one. If a job of the schedule's
type is already outstanding for that collector, the controller does not
queue a second one, so a slow or temporarily unreachable collector cannot
be made to accumulate a growing backlog by a schedule alone. Unlike
enrollment tokens, heartbeats, and one-off job state, a schedule is
persisted under `-db-driver sqlite` — a lost recurring-discovery policy on
restart is a silent, indefinitely-lasting behavior change an operator is
unlikely to notice, unlike a lost heartbeat or a lost single job. Schedule
creation, update, and deletion are each recorded in the audit log (see
"Persistent storage and the audit log" above). See
[Server-side recurring discovery scheduling](docs/scheduling.md).

## ServiceNow publishing

Topo's ServiceNow IRE payload builder deduplicates by `source_native_key`
and by relationship `(type, from, to)` within a batch, and is validated to
produce an identical `(source_native_key, className)` set across
independently repeated Topo Lab discovery scans — the condition ServiceNow's
own IRE relies on to reconcile a CI rather than create a duplicate one.
That condition has been verified against a real ServiceNow developer
instance for the `cmdb_ci_computer` class, and for an `IRERelation`
between it and `cmdb_ci_network_adapter`: submitting the same
`sys_object_source_info` twice reconciles both items to the same CIs
(`operation: UPDATE` against the original `sysId`s, matched via
`sys_object_source`) and the relation between them to `operation:
NO_CHANGE` — none of the three duplicated. Coverage of Topo's other CI
classes against a real instance remains open. ServiceNow's IRE response
schema is still not parsed: `PublishBatch` treats any 2xx response as
published and any non-2xx as rejected without depending on
response fields, since that schema is proprietary and only partially
observed so far, and requires an absolute HTTPS instance URL and a bearer
token supplied through the same credential reference contract as every
other Topo secret. See [ServiceNow publishing](docs/servicenow.md).
