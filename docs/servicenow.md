# ServiceNow publishing

Topo publishes to ServiceNow through the documented
[Identification and Reconciliation API](https://www.servicenow.com/docs/r/api-reference/rest-apis/c_IdentifyReconcileAPI.html),
using the Engine (IRE) `enhanced` operation
(`POST /api/now/identifyreconcile/enhanced`). It
never writes `cmdb_ci` tables directly. Each item carries
`sys_object_source_info` — a stable `source_name`/`source_native_key` pair —
which is how ServiceNow's IRE recognizes "this is the same configuration
item I've seen before" across repeated scans rather than creating a new,
duplicate CI each time.

Topo is the discovery engine. ServiceNow is a destination and reconciliation
authority, not the source of commands or Topo's internal data model. The
supported path is therefore Topo-owned scheduling and compiled-in discovery,
followed by direct publication to IRE. A customer does not install a Topo MID
replacement, scoped application, update set, custom table, Business Rule, or
sensor to use this path.

ECC Queue is not the CMDB ingestion boundary. It carries MID probe requests and
results; instance-side Business Rules and sensors decide whether an `input`
record is processed and what it means. A syntactically valid ECC result with no
matching native processor does not become a CI merely because it exists in
`ecc_queue`. ServiceNow documents that sensor processing is topic-specific in
[Discovery probes and sensors](https://www.servicenow.com/docs/r/xanadu/it-operations-management/discovery/c_DiscoveryProbesAndSensors.html),
while [Discovery](https://www.servicenow.com/docs/r/it-operations-management/discovery/r-discovery.html)
is a separate subscription. Topo does not claim that publishing through IRE
grants or emulates that subscription.

The custom scoped-app Relay and `topo mid run` remain experiments, not customer
requirements. The real official-MID evidence and the reason ECC is not the
product ingestion path are recorded in
[Experimental ServiceNow ECC-compatible MID transport](servicenow-mid.md);
the separate scoped-app prototype is in
[experimental scoped-app Relay](servicenow-relay.md).

The managed mode uses the Nischoy Topo scoped application for schedules, runs,
leases, raw results, application-side mapping and IRE delivery. Stateless
`topo worker run` processes execute reviewed `local.v1` or `ssh_linux.v1`
operations. The published [native XML package](servicenow-update-set.md)
installs the application and required indexes; developer source is defined in
ServiceNow Fluent with SDK 4.9.0. Workers use seven custom Scripted REST
resources and never call IRE directly or reuse the direct publisher's OAuth
authority. See [managed-worker behavior](servicenow-worker.md) and
[package validation](servicenow-validation.md) for their separate real-system
and simulator evidence.

`topo publish servicenow` is the supported non-experimental operator workflow
over the existing IRE mapper and publisher. It reads the JSON Lines observation
format emitted by Topo discovery, previews the exact request locally by
default, and writes only when the operator supplies `-apply`. Preview does not
resolve a credential or make a network request. Apply resolves the bearer token
through the shared credential-reference contract, submits the exact payload to
ServiceNow's documented non-committing
`POST /api/now/identifyreconcile/queryEnhanced` endpoint, and calls the write
endpoint only if that server-side preflight reports neither an error nor a
warning. The command emits both preflight and apply outcomes as structured JSON
delivery status.

```sh
./bin/topo discover local > observation.jsonl

# Offline preview: no token is read and no request is sent.
./bin/topo publish servicenow \
  -input observation.jsonl \
  -instance https://example.service-now.com > ire-preview.json

# Explicit write through IRE.
./bin/topo publish servicenow \
  -input observation.jsonl \
  -instance https://example.service-now.com \
  -token-ref file:/absolute/path/to/servicenow-token \
  -apply
```

`-input -` reads stdin. Apply defaults to three attempts with one-second
bounded exponential backoff; only transport failures, HTTP 429, and 5xx are
retried. Use `-max-attempts` (1-5), `-retry-delay` (at most 30 seconds), and
`-timeout` (at most ten minutes) to lower those bounds. HTTP 4xx,
`hasError:true`, `hasWarning:true`, unreadable, malformed, or oversized responses, and
other ambiguous outcomes are returned visibly and are not replayed
automatically because an apply request may already have left an incomplete
identification record. The non-committing preflight behavior is ServiceNow's
documented contract; it is not a Topo-specific simulation.

The input is bounded to 10 MiB, 100 JSONL envelopes, 1 MiB per envelope, and 64
JSON nesting levels. One request is bounded to 1,000 unique items, 2,000 unique
relationships, 4 MiB of JSON, and a 1 MiB response. The instance must be a bare
absolute HTTPS origin with no URL credentials, path, query, or fragment;
redirects are refused and every request is cancellable.

## Reviewed mapping boundary

The supported path does not turn an imported observation into a generic CMDB
writer. It accepts only these asset mappings:

| Topo asset type | ServiceNow class |
| --- | --- |
| `host` | `cmdb_ci_computer` |
| `network_interface` | `cmdb_ci_network_adapter` |

Every item receives only `name`, the registered `discovery_source`, and
`last_discovered`; network adapters may also receive the reviewed
`mac_address` field. Arbitrary observation attribute names are not copied to
IRE. The only accepted relationship is
`host_has_interface` -> `Owns::Owned by`, and its endpoints must be a host and
network interface present in the same bounded input. Unknown asset types,
unknown/raw relationship names, dangling endpoints, and a repeated
`source_native_key` that changes class are rejected before credential
resolution. Service/cloud/Kubernetes mappings and VMware relationships require
separate reviewed slices.

For a disposable developer instance, the sanitized
`examples/servicenow/ire-validation.jsonl` fixture exercises both supported
classes in one three-item batch plus two reviewed relationships. Its source
keys and names are visibly prefixed `topo-ire-validation`; applying it creates
or updates real CMDB/IRE state, so always inspect the default local preview
first and do not use the fixture against a production instance. Repeating the
same apply is the real-instance reconciliation test: the same source keys must
resolve to the original CIs and the same relationship rows rather than create
duplicates.

Volumes, software packages, and virtual machines are deliberately rejected at
this boundary. A 2026-08-29 real-instance preflight showed that the default
rules require a disk containment relationship, a software matching key, and a
VM hosting/runs-on relationship. Topo does not guess those fields or publish
partial CIs. Each class can be added later with its exact identification,
dependency, relationship, and repeat-reconciliation contract backed by real
evidence.

This architecture deliberately does not make Topo appear in ServiceNow's
standard MID Server selector or drive native Discovery Schedule and Discovery
Status records. Customers that require a ServiceNow-side control experience
need a separately supported integration surface, such as a reviewed scoped
application, IntegrationHub ETL integration, or Service Graph Connector; none
is silently installed by the Topo binary.

## Configuration

`Preview` never makes a network call; use it to inspect the exact payload
before enabling writes. `PublishBatch` requires an absolute HTTPS instance
URL and, outside dry-run, a bearer token — see
[Credential references](credential-references.md) for how to supply it
without an ordinary CLI value.

The strongest setup verified for a machine publisher uses ServiceNow's
[OAuth client-credentials grant](https://www.servicenow.com/docs/r/platform-security/authentication/client-credential-grant.html)
and inbound REST token restrictions:

1. Enable `glide.oauth.inbound.client.credential.grant_type.enabled` and create
   a dedicated active machine/internal-integration user. Grant only the native
   `asset` role required by the
   [IRE API](https://www.servicenow.com/docs/r/api-reference/rest-apis/c_IdentifyReconcileAPI.html),
   not `admin`, `itil`, or a shared human account.
2. Create an active OAuth API endpoint for external clients with client type
   **Integration as a Service**, bind **OAuth Application User** to that
   machine user, use short-lived opaque access tokens, securely scope it, and
   enable **Enforce Token Restrictions**. Store the generated client secret in
   an owner-readable credential file or external secret provider.
3. Create one authentication scope and bind it to the OAuth entity. Add two
   REST API authentication-scope records for **Identification and
   Reconciliation API**, method `POST`, version `latest`, restricted exactly to
   `/now/identifyreconcile/queryEnhanced` and
   `/now/identifyreconcile/enhanced`.
4. Create an OAuth inbound authentication profile for that OAuth entity. Add
   two active, non-global API Access Policies, again restricted to those exact
   POST resources and latest version, and attach only that inbound profile.
   Leave every apply-all setting off. This second layer is required when token
   restrictions are enforced; a scope string alone is not the access policy.
5. Request short-lived tokens from `/oauth_token.do` with
   `grant_type=client_credentials` and the configured scope. Write only the
   returned access token, with no trailing newline, to an owner-readable file;
   pass `file:/absolute/path` to Topo. The credential-reference contract
   preserves file bytes exactly, so a newline-producing formatter will make
   the bearer token invalid.

Store the resulting access token in an `env:`, owner-readable absolute
`file:`, `vault:`, or `k8s:` reference. Do not place the access token, OAuth
client secret, or a user password in the command line, observation file,
preview output, or chat. Access tokens are time-bounded; refresh them outside
this initial manual workflow rather than treating one captured token as a
permanent credential.

## Credential-free local-network boundary

`topo discover local` inventories the laptop and its own interfaces. It does
not scan or classify other LAN devices. Without device credentials, Topo's
current reviewed discovery operations cannot establish those devices'
hostname, OS, hardware identity, or CI class, and an IP address is not accepted
as a long-lived device identity. Publishing ARP-cache rows as computers would
therefore create misleading CIs.

A future credential-free neighbor slice may passively observe bounded local
neighbor protocols and publish only identities it can support with stable
evidence (for example, a network-adapter identity backed by a MAC address),
under a local allowlist and without arbitrary probes. Until that slice is
implemented and validated, use explicit SSH, WinRM, SNMPv3, VMware, cloud, or
Kubernetes credentials for remote discovery. No credential-free LAN-device
claim is made by the laptop validation above.

## Discovery-source registration

Before enabling destination writes, confirm identification rules for the
supported classes and register the exact source value **Nischoy Topo** on
`cmdb_ci.discovery_source`. This applies to direct IRE publishing and to the
current managed-app XML package. The managed package currently installs the
app and indexes; it does not register this Global choice.

As a ServiceNow administrator in **Global**, open **System Definition →
Choice Lists**. Filter **Table = cmdb_ci**, **Element = discovery_source**,
and **Value = Nischoy Topo**. If an active matching choice exists, retain it;
do not insert a duplicate. If none exists, create a choice with:

| Field | Value |
| --- | --- |
| Table | `cmdb_ci` |
| Element | `discovery_source` |
| Label | `Nischoy Topo` |
| Value | `Nischoy Topo` |
| Language | `en` |
| Inactive | false |

If a matching choice is inactive or customer-customized, review it through
normal change control before enabling or replacing it. Keep other sources and
translations intact. Capture the change in a customer-owned Global update set.
ServiceNow documents [choice-list capture by update sets](https://www.servicenow.com/docs/r/application-development/system-update-sets/customizations-tracked-update-sets.html).

Topo uses the registered value as both the IRE discovery source and stable
source name. A missing choice causes IRE to reject the request with
`INVALID_INPUT_DATA`; verify the source before starting scans. If a direct
publisher uses a different configured source, register that exact value instead.

## Validation records

See [IRE evidence](evidence/servicenow-ire.md) for repeat-scan, reconciliation
and authorization results with their exact class and batch scope.
