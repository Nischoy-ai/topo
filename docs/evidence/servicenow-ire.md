# IRE validation evidence

Historical evidence captured from source commit
`5439ef5b441e1145172190db94aa24ba4d809599`. Versions, dates and test
scopes below identify the tested artifacts; they are not installation steps.

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
  observed reconciliation mechanism for this tested class and payload. Topo's
  stable source keys and batch deduplication supply its matching inputs.
- **A previously-unknown real requirement: `discovery_source` on `cmdb_ci`
  is a registered choice field, not free text.** Submitting a payload with
  an unregistered `discovery_source` value fails with `INVALID_INPUT_DATA`
  (`"You need to provide a valid choice value from field
  [discovery_source] in table [cmdb_ci]"`) — this could only have been
  found by hitting a real instance; nothing in ServiceNow's public IRE
  documentation makes it obvious ahead of time. See
  [Configuration](../servicenow.md#configuration) below for the fix.
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
