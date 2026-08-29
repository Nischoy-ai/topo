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

## What is validated, and how

Duplicate-CI prevention has two halves: what Topo sends, and what
ServiceNow's IRE does with it. Both halves are now backed by evidence:
Topo's own outbound payload is verified through this project's own test
suite (no ServiceNow instance needed to run in CI), and ServiceNow's actual
reconciliation behavior has been verified against a real instance — see
[Verified against a real instance](#verified-against-a-real-instance)
below. Together they establish that Topo's payload is what a real
ServiceNow IRE actually needs to recognize "this is the same configuration
item I've seen before," not just a self-consistent claim about what Topo
sends.

Specifically, what Topo's own test suite proves without needing an
instance:

- **No duplicate items or relationships within one request.** `mapPayload`
  deduplicates by `source_native_key` (last observation wins, matching
  `store.Memory`'s resolved-asset semantics) and by `(type, from, to)` for
  relationships, even when the input spans several envelopes — for example,
  a batch of buffered observations covering the same host twice. Earlier,
  Topo could emit two IRE items with the same `source_native_key` in one
  request if the input contained the same asset more than once.
- **Idempotent across independent scans.** `TestMapPayloadIsIdempotentAcrossRepeatedLabScans`
  runs a Topo Lab estate through discovery twice (the same two-scan pattern
  the SSH and WinRM acceptance gates already use) and asserts the mapped
  `(source_native_key, className)` set is identical both times — proving
  Topo's own mapping is stable, not just within a batch but across
  independently repeated discovery runs.
- **Correct, idempotent wire requests.** `TestPublishBatchSendsIdempotentRequestsAcrossRepeatedLabScans`
  runs the same two-scan payloads through `PublishBatch` against a fake IRE
  endpoint and asserts the actual HTTP requests — method, path, auth header,
  and the source keys they carry — match on both scans.
- **Response visibility without assuming a schema.** `PublishBatch` captures
  the bounded response body in `Diagnostics` for operator review. It recognizes
  the documented `hasError: true` and `hasWarning: true` semantic bits at any
  JSON nesting depth and rejects that query/publication, without coupling Topo
  to the rest of ServiceNow's release-dependent response schema.

## Verified against a real instance

Validated 2026-08-19 against a real ServiceNow developer instance
(`POST /api/now/identifyreconcile/enhanced` called directly with the exact
payload shape `mapPayload` produces), not a mock or an assumption:

- **Submitting an item once creates a new CI.** A `cmdb_ci_computer` item
  with a fresh `sys_object_source_info` (`source_name`/`source_native_key`)
  came back `"operation":"INSERT"` with a new `sysId`, and
  `identificationAttempts` showed `sys_object_source NO_MATCH` — expected,
  since nothing existed yet to match against.
- **Resubmitting the identical `source_native_key` reconciles to the same
  CI, not a duplicate.** The second submission — same `source_name` and
  `source_native_key`, different `last_discovered` timestamp — came back
  `"operation":"UPDATE"` against the **same `sysId`** as the first
  submission, with `identificationAttempts` showing
  `sys_object_source MATCHED` on exactly the `source_name`/
  `source_native_key` pair `sys_object_source_info` carries. This is the
  actual mechanism this project has flagged as unverified since the
  ServiceNow IRE duplicate-CI validation milestone: it is the precondition
  Topo's own payload construction (deduplication, idempotency across
  scans) was built to satisfy, and it now has real evidence behind it, not
  just an assumption about how `sys_object_source_info` would be used.
- **A previously-unknown real requirement: `discovery_source` on `cmdb_ci`
  is a registered choice field, not free text.** Submitting a payload with
  an unregistered `discovery_source` value fails with `INVALID_INPUT_DATA`
  (`"You need to provide a valid choice value from field
  [discovery_source] in table [cmdb_ci]"`) — this could only have been
  found by hitting a real instance; nothing in ServiceNow's public IRE
  documentation makes it obvious ahead of time. See
  [Configuration](#configuration) below for the fix.
- **A failed submission can still leave a partial record behind.** The
  request that failed on the choice-field error came back
  `"operation":"INSERT_AS_INCOMPLETE"` with an `incompleteSysIds` entry —
  ServiceNow created a stub tied to the failed identification attempt even
  though the response reported `hasError: true`. It was not visible
  through the normal class-table Table API afterward (a `DELETE` against
  it 404'd), so it appears to be an internal identification-engine
  artifact rather than a real CMDB record, but operators should not assume
  a `hasError: true` response left nothing behind.
- **`IRERelation` payloads reconcile too, not just items.** A single
  request submitting two items (`cmdb_ci_computer` and
  `cmdb_ci_network_adapter`) plus a `relations` entry referencing them by
  their in-request index (`{"type":"Owns::Owned by","parent":0,"child":1}`
  — the exact relationship type `relationFor("host_has_interface")`
  produces) came back with both items `INSERT`ed and the relation itself
  `INSERT`ed as a new `cmdb_rel_ci` row. Resubmitting the identical
  payload came back with both items `UPDATE`d (matched via
  `sys_object_source`, same `sysId`s as before) and the relation reported
  **`"operation":"NO_CHANGE"`** — not a second `INSERT` — and a direct
  `cmdb_rel_ci` table query after each submission confirmed exactly one
  relationship row throughout, same `sys_id` both times. Relationship
  types are also a registered value (`cmdb_rel_type`, checked read-only
  via the Table API before submitting, the same lesson as
  `discovery_source`) rather than free text; an unregistered type would
  likely fail the same way an unregistered discovery source did, though
  that specific failure mode was not separately provoked here.

Additional 2026-08-29 real-instance evidence exercises the complete supported
operator workflow and a larger batch:

- A client-credentials token scoped to only the two IRE POST resources
  successfully called `queryEnhanced` and `enhanced`; the same token received
  HTTP 401 from an unrelated Table API resource.
- A real `topo discover local` observation preflighted and applied 22 items
  (one `cmdb_ci_computer`, 21 `cmdb_ci_network_adapter`) and 21
  `Owns::Owned by` relations with no errors or warnings. The standard CMDB
  lists showed exactly one matching laptop CI and 21 adapters created that
  day under discovery source `Nischoy Topo`.
- Repeating the identical observation produced 22 `NO_CHANGE` item results
  and 21 `NO_CHANGE` relation results on apply, with no errors or warnings.
  No duplicate laptop or adapter CI was created.
- A separate six-item preflight was intentionally attempted against the three
  previously proposed mappings. ServiceNow returned `hasWarning:true`:
  `cmdb_ci_disk` and `cmdb_ci_vm_instance` lacked required dependencies, and
  `cmdb_ci_spkg` lacked its identification key. The CLI correctly withheld the
  apply request. Those mappings were then removed from the supported boundary.

**What this does not yet cover:** classes beyond `cmdb_ci_computer` and
`cmdb_ci_network_adapter`, relationship types beyond `Owns::Owned by`,
retirement/deletion, larger batches than the 22-item/21-relation laptop run,
or the full IRE response schema. `PublishBatch` recognizes only the observed
`hasError` and `hasWarning` semantic bits; a 2xx response with either set is
rejected, while other successful response details remain bounded diagnostics
rather than a version-coupled contract.

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

Before enabling destination writes: confirm identification rules exist for
every class Topo emits, and — a real requirement discovered during
validation, not a hypothetical — register the discovery source name
(`"Nischoy Topo"` by default, matching `Config.DiscoverySource`) as a
valid choice value for `cmdb_ci`'s `discovery_source` field. From an
account with rights to write `sys_choice`:

```sh
curl -s -u "$SN_USER:$SN_PASS" -H 'Content-Type: application/json' \
  -X POST "$SN_INSTANCE/api/now/table/sys_choice" \
  -d '{"name":"cmdb_ci","element":"discovery_source","label":"Nischoy Topo","value":"Nischoy Topo","language":"en","inactive":"false"}'
```

Without this, every write is rejected with `INVALID_INPUT_DATA` — it is
not optional, and there is no fallback default that lets an unregistered
source through.
