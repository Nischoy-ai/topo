# Security policy

Nischoy Topo is pre-alpha and has no supported production release yet. Report vulnerabilities privately to the maintainers; do not open a public issue containing exploit details or credentials.

## Trust boundaries

Collectors and agents process data from untrusted infrastructure. Destination APIs and discovery targets must be treated as hostile. Plugins must validate all configuration, use bounded reads and deadlines, avoid locally constructed or user-supplied shell text, redact secrets, and return structured errors. A plugin must never accept arbitrary commands from the controller.

The controller's bearer-key authentication is an evaluation bootstrap, not the final enterprise trust model. Before production readiness, Nischoy Topo requires per-device enrollment, short-lived mTLS certificates, and rotation (all implemented — see [Collector enrollment](docs/enrollment.md)) plus revocation (still outstanding), encrypted persistent secrets, immutable audit events, signed artifacts and plugin manifests, SBOM generation, and external penetration testing.

## Deployment guidance

- Bind evaluation controllers to localhost or a private management interface.
- Use a long random API key and TLS-terminating reverse proxy.
- Use dedicated read-only discovery identities and restrict targets by allowlist.
- Verify SSH host keys with a managed `known_hosts` file. `-insecure-host-key` exists only for isolated Topo Lab evaluation.
- Require HTTPS with normal certificate and hostname verification for non-Lab WinRM targets. Production NTLMv2 never falls back to Basic authentication. Basic authentication and HTTP are restricted to the explicit loopback-only Topo Lab mode.
- Require SNMPv3 `authPriv` with SHA authentication and AES privacy for non-Lab SNMP targets; there is no weaker fallback. `noAuthNoPriv` is restricted to the explicit loopback-only Topo Lab mode.
- Require HTTPS with normal certificate verification for non-Lab VMware targets; there is no fallback. `-lab` (HTTP, skipped certificate verification) is restricted to loopback `vcsim` targets. Use a read-only vCenter role — the plugin never issues a configuration, power, or lifecycle operation.
- For a persistent controller, use `-db-driver sqlite -db-dsn <path>` and restrict the database file's permissions to the Topo process identity; the default `-db-driver memory` loses all discovery data on every restart. There is no encryption at rest yet — treat the database file itself as sensitive.
- Never place credentials in job options, labels, logs, or observation attributes.
- Pass credential provider references, never credential values, as CLI arguments. Restrict credential-file permissions to the Topo process identity.
- Review ServiceNow IRE preview output before enabling destination writes,
  and configure identification/reconciliation rules for every CI class Topo
  emits; see [ServiceNow publishing](docs/servicenow.md).

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

## Persistent storage

The controller's storage backend (`store.Repository`) is opt-in persistent: the default `-db-driver memory` keeps every prior release's behavior exactly (nothing survives a restart), and `-db-driver sqlite -db-dsn <path>` opts into a SQLite-backed store that does. There is no encryption at rest — the database file's confidentiality depends entirely on filesystem permissions, the same trust boundary this project already places on the enrollment CA's private key and Topo Agent's offline spool. Enrollment tokens, collector heartbeats, and job state remain in-memory only regardless of `-db-driver`; a controller restart still invalidates outstanding enrollment tokens and loses heartbeat/job history. `SaveObservation` is idempotent by observation ID in both backends — a collector retrying a delivery whose response was lost replaces the existing record rather than creating a duplicate, so retried delivery cannot be used to inflate stored observation counts. See [Persistent storage](docs/storage.md).

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
auth, mints a single-use one-hour token) and `POST /v1/enroll` (redeems the
token and signs a submitted CSR). The private key is generated on the
collector and never transmitted; only the CSR — a public key plus a
signature proving possession of the private key — crosses the network. A
malformed enrollment request is rejected before the token is redeemed, so
it cannot burn a valid token. The CA's own private key is protected by
filesystem permissions, matching every other private key in this project,
not a second application-level encryption layer. The token store is
in-memory only and does not survive a controller restart, matching every
other piece of controller state today. There is no certificate revocation
yet; a compromised collector certificate is contained only by its bounded
lifetime. Enrollment is opt-in and does not change any existing
bearer-key-authenticated behavior when `-ca-dir` is not set.

The issued certificate now authenticates live traffic: `topo serve -mtls`
runs a native TLS listener, issuing itself a server certificate from the
same CA (1-year TTL, reissued fresh on every start rather than persisted
or renewed while the process keeps running — collector certificate
rotation, below, does not change that, since the server certificate is
never persisted in the first place), and verifies client certificates
presented against that CA. A request with a certificate verified during
the TLS handshake reaches protected endpoints without the bearer API key.
The TLS layer still accepts a handshake with no client certificate at all
— a collector's first-ever request, `POST /v1/enroll`, has none to present
yet, authenticating instead with its one-time enrollment token — so
per-endpoint enforcement (a verified certificate or the bearer key)
happens in application-layer middleware, not the TLS handshake itself.
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
at startup, not reloaded live. There is still no certificate revocation;
rotation renews a certificate on the collector's own initiative but gives
the controller no way to invalidate one early. See
[Collector enrollment](docs/enrollment.md).

## Collector heartbeats

`POST /v1/heartbeats` is a lightweight liveness signal, distinct from
observation delivery, so the controller can tell a collector is alive
without waiting on the discovery/delivery interval, which is often 15
minutes or longer. Unlike `POST /v1/rotate`, it accepts either the bearer
API key or a verified mTLS client certificate — the same `auth()`
middleware every other data-plane endpoint uses — since a heartbeat only
asserts liveness rather than getting new certificate material issued to
it, so there is no analogous "any bearer-key holder can impersonate any
collector" risk to guard against. When a verified peer certificate is
present, its subject still overrides whatever `collector_id` the request
body claims, matching the same identity rule as certificate rotation, so
a collector authenticated by mTLS can never appear alive under a
different collector's identity; a bearer-key-authenticated heartbeat has
no such stronger signal and is recorded under whatever `collector_id` the
body states. `GET /v1/collectors` lists every collector's most recent
heartbeat and whether it falls within a fixed three-minute staleness
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
job for a collector — uses the same `auth()` middleware as every other
admin-style action in this project, including `POST /v1/enrollment-tokens`;
the shared bearer key or any verified collector certificate is accepted,
matching existing precedent rather than introducing a new admin-only
credential tier here. There is exactly one job type, `discover`, since it
is the only real capability `topo agent run` has; a request for any
other type is rejected at creation, not silently accepted and left
unrunnable. Job state is in-memory only, like the enrollment token store
and heartbeat store, and does not survive a controller restart. See
[Job delivery](docs/jobs.md).

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
