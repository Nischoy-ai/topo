# ServiceNow publishing

Topo publishes to ServiceNow through the Identification and Reconciliation
Engine (IRE) `enhanced` API (`POST /api/now/identifyreconcile/enhanced`). It
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

The repository already contains the bounded IRE mapper and publisher used by
the Relay experiment, and the CLI can preview the exact mapped payload. A
non-experimental operator workflow that invokes that publisher from Topo's own
controller or CLI is still a product gap. It must be staged separately with
credential references, preview-before-write behavior, bounded retries, and
clear delivery status before this direction is described as a complete
customer installation flow.

```sh
./bin/topo discover -format servicenow-preview local
```

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
  the `hasError: true` semantic bit observed during real-instance validation at
  any JSON nesting depth and rejects that publication, without coupling Topo
  to the rest of ServiceNow's proprietary response schema.

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

**What this does not yet cover:** `cmdb_ci_computer` and
`cmdb_ci_network_adapter` were exercised, single- and two-item requests,
with one relationship between them — `mapPayload`'s other classes
(`cmdb_ci_disk`, `cmdb_ci_spkg`, `cmdb_ci_vm_instance`) share the same
mechanism (inherited from `cmdb_ci` and the same `identifyreconcile`
endpoint) but have not individually been submitted to a real instance.
Larger multi-item batches, multiple relations in one request, and this
instance's specific identification/reconciliation rule configuration for
classes beyond the default were also not exercised. The full IRE response
schema is still not parsed by `PublishBatch`; only the real-instance-observed
`hasError` semantic bit is recognized. A 2xx response with `hasError: true` is
rejected, while other successful response details remain bounded diagnostics
rather than a version-coupled contract.

## Configuration

`Preview` never makes a network call; use it to inspect the exact payload
before enabling writes. `PublishBatch` requires an absolute HTTPS instance
URL and, outside dry-run, a bearer token — see
[Credential references](credential-references.md) for how to supply it
without an ordinary CLI value.

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
