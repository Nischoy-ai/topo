# Architecture

Nischoy Topo is an open-source, destination-neutral discovery data plane for
hybrid IT. It collects bounded evidence about infrastructure, normalizes
assets and relationships into a stable schema, and publishes them to
ServiceNow or another destination without making that destination the
discovery engine.

## Observation and identity model

The canonical `ObservationEnvelope` separates immutable source observations
from resolved assets. Each asset has a source-native identity, optional strong
identifiers, attributes, and evidence. Relationships refer to native
identities within an observation. IP addresses are mutable attributes and do
not determine identity.

When multiple site, collector, or plugin sources report the same stable asset,
Topo retains every source's latest claim. `topo serve -source-precedence`
selects a deterministic winner, while `GET /v1/assets` exposes contributing
sources, field conflicts, and first/latest observation timestamps. See [source
precedence and asset freshness](source-resolution.md).

## ServiceNow-controlled discovery

The supported ServiceNow-managed architecture separates its durable control
plane from disposable workers:

1. The Nischoy Topo scoped application owns profiles, schedules, runs, tasks,
   leases, result processing, IRE delivery, summaries, and retention.
2. Stateless workers poll ServiceNow over outbound HTTPS and expose no inbound
   listener.
3. A worker executes only a compiled-in operation that is enabled by its local
   startup policy. ServiceNow never supplies command text, scripts, WQL, OIDs,
   URLs, CMDB classes, fields, or relationships.
4. Workers return destination-neutral observations. The scoped application
   performs reviewed mapping, IRE preflight, and IRE apply.

The worker has no database, task journal, result spool, schedule store,
observation history, or retry queue. Read-only startup configuration may hold
its ServiceNow identity, TLS trust, pool/site assignment, concurrency ceiling,
target allowlist, and SSH host identities. See the [ServiceNow control-plane
design](servicenow-control-plane.md) and [worker contract](servicenow-worker.md).

The older scoped-app Relay and ECC-compatible MID transports remain bounded
experiments. They are not customer installation requirements or replacements
for ServiceNow Discovery.

## Public extension points

Topo keeps its main extension boundaries small:

- `discovery.Plugin`: capability description, configuration validation,
  connectivity checking, and discovery;
- `publisher.Publisher`: destination validation, preview, and batch
  publication; and
- `store.Repository`: immutable observations, per-source claims, relationships,
  resolved assets, audit data, and schedules.

The JSON Schema and Protobuf definitions under `api/` are the cross-process
contract. They remain `v1alpha1`; a breaking change must increment the schema
version.

## Product capabilities

- **Topo Relay** — agentless discovery collector deployed in a network segment.
- **Topo Agent** — outbound-only endpoint discovery agent.
- **Topo Hub** — self-hosted controller and local asset view.
- **Topo Connect** — ServiceNow and other CMDB publishers.
- **Topo Graph** — the future full CMDB product.

See [SECURITY.md](../SECURITY.md) for trust boundaries and deployment guidance,
and [ROADMAP.md](../ROADMAP.md) for implemented and planned capabilities.
