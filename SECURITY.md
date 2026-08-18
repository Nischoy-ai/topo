# Security policy

Nischoy Topo is pre-alpha and has no supported production release yet. Report vulnerabilities privately to the maintainers; do not open a public issue containing exploit details or credentials.

## Trust boundaries

Collectors and agents process data from untrusted infrastructure. Destination APIs and discovery targets must be treated as hostile. Plugins must validate all configuration, use bounded reads and deadlines, avoid locally constructed or user-supplied shell text, redact secrets, and return structured errors. A plugin must never accept arbitrary commands from the controller.

The controller's bearer-key authentication is an evaluation bootstrap, not the final enterprise trust model. Before production readiness, Nischoy Topo requires per-device enrollment, short-lived mTLS certificates, rotation/revocation, encrypted persistent secrets, immutable audit events, signed artifacts and plugin manifests, SBOM generation, and external penetration testing.

## Deployment guidance

- Bind evaluation controllers to localhost or a private management interface.
- Use a long random API key and TLS-terminating reverse proxy.
- Use dedicated read-only discovery identities and restrict targets by allowlist.
- Verify SSH host keys with a managed `known_hosts` file. `-insecure-host-key` exists only for isolated Topo Lab evaluation.
- Require HTTPS with normal certificate and hostname verification for non-Lab WinRM targets. Production NTLMv2 never falls back to Basic authentication. Basic authentication and HTTP are restricted to the explicit loopback-only Topo Lab mode.
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
enrollment now exists (see below) but is not yet wired into the agent's
live traffic; the agent still authenticates with the bearer API-key
contract until the next slice of that milestone.

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
bearer-key-authenticated behavior when `-ca-dir` is not set. The issued
certificate is not yet used to authenticate any live request — that is the
next slice of this milestone. See [Collector enrollment](docs/enrollment.md).

## ServiceNow publishing

Topo's ServiceNow IRE payload builder deduplicates by `source_native_key`
and by relationship `(type, from, to)` within a batch, and is validated to
produce an identical `(source_native_key, className)` set across
independently repeated Topo Lab discovery scans — the condition ServiceNow's
own IRE relies on to reconcile a CI rather than create a duplicate one.
That is the extent of what this project can verify: there is no ServiceNow
instance available here, so ServiceNow's own identification/reconciliation
logic and its IRE response schema are neither exercised nor assumed.
`PublishBatch` treats any 2xx response as published and any non-2xx as
rejected without parsing response fields, and requires an absolute HTTPS
instance URL and a bearer token supplied through the same credential
reference contract as every other Topo secret. See
[ServiceNow publishing](docs/servicenow.md).
