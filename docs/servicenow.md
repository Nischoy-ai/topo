# ServiceNow publishing

Topo publishes to ServiceNow through the Identification and Reconciliation
Engine (IRE) `enhanced` API (`POST /api/now/identifyreconcile/enhanced`). It
never writes `cmdb_ci` tables directly. Each item carries
`sys_object_source_info` — a stable `source_name`/`source_native_key` pair —
which is how ServiceNow's IRE recognizes "this is the same configuration
item I've seen before" across repeated scans rather than creating a new,
duplicate CI each time.

```sh
./bin/topo discover -format servicenow-preview local
```

## What is validated, and how

Duplicate-CI prevention has two halves: what Topo sends, and what
ServiceNow's IRE does with it. This project only has access to the first
half — there is no ServiceNow instance available to develop or test against,
and ServiceNow's IRE response schema is proprietary and undocumented outside
an instance's own scripted REST API definitions. So "duplicate-CI
validation" here means: **Topo's outbound payload is proven idempotent and
duplicate-free**, which is the necessary precondition for ServiceNow's own
reconciliation logic to work correctly. It is not a claim about ServiceNow's
behavior itself.

Specifically:

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
  the (bounded) response body in `Diagnostics` for operator review, but does
  not parse or depend on any particular field of it.

## What is explicitly deferred

- **ServiceNow's own IRE identification/reconciliation behavior.** Whether a
  given ServiceNow instance actually matches and updates one CI across
  scans depends on that instance's identification rules, reconciliation
  definitions, and discovery source configuration — none of which Topo
  controls or can validate without one. Configure identification rules for
  each `cmdb_ci_*` class Topo emits (see [`classFor`](../pkg/publisher/servicenow/servicenow.go))
  keyed on the discovery source and `source_native_key`, and validate
  against a real or sandboxed instance before production use.
- **The IRE response schema.** `PublishBatch` treats any 2xx as published
  and any non-2xx as rejected; it does not parse per-item created/matched/
  updated status, because that schema is proprietary and unverified here.
- **Duplicate-CI validation against a real instance.** This remains an open
  gate before claiming production readiness, the same posture this project
  already takes with WinRM real-host compatibility and Windows Topo Agent
  service verification: implemented and tested against what Topo controls,
  not yet exercised against the real external system.

## Configuration

`Preview` never makes a network call; use it to inspect the exact payload
before enabling writes. `PublishBatch` requires an absolute HTTPS instance
URL and, outside dry-run, a bearer token — see
[Credential references](credential-references.md) for how to supply it
without an ordinary CLI value. Review preview output with a ServiceNow
administrator and confirm identification rules exist for every class Topo
emits before enabling destination writes.
