# ServiceNow-controlled stateless Topo architecture

## Status

This document records the approved target architecture for a managed Topo
deployment in which the Nischoy Topo scoped application is the discovery
control plane and ServiceNow is the only durable operational datastore. It was
agreed on 2026-08-29. It is a design and implementation plan, not a claim that
the control plane is implemented or validated yet.

The existing direct `topo publish servicenow` workflow remains the supported
standalone publication mode. The scoped-app Relay and ECC-compatible MID
transport remain experiments. This architecture supersedes the Relay as the
target for a ServiceNow-managed mode; it does not make ECC a product path or
claim compatibility with native ServiceNow Discovery schedules, probes,
patterns, sensors, or Discovery Status.

## Decisions and constraints

1. Topo does not use `ecc_queue` for discovery work or results. Public ECC
   transport documentation does not define the release-dependent topic and
   payload contracts consumed by stock probes, patterns, and sensors.
2. A Topo worker keeps no durable operational state. It has no local database,
   schedule store, task journal, result spool, observation history, or retry
   queue. Multiple disposable workers may serve one worker pool.
3. The Nischoy Topo ServiceNow application is the control panel and the sole
   durable store for profiles, schedules, runs, tasks, leases, result chunks,
   delivery outcomes, and worker status.
4. Native ServiceNow tables are used only where there is a documented,
   supported contract. Configuration items and relationships enter native
   CMDB tables through IRE; Topo-specific control-plane state uses scoped
   application tables.
5. Raw result chunks expire after successful IRE processing. Normalized CIs,
   relationships, source identity, and bounded run summaries remain according
   to customer lifecycle policy.
6. Discovery credentials may be stored either in ServiceNow using a protected
   encrypted credential record or outside ServiceNow in a supported secret
   provider. Both modes require real-system security and protocol tests.
7. Read-only startup configuration is permitted on a Topo worker. Every worker
   must enforce a deployment-controlled local target allowlist in addition to
   the target policy selected in ServiceNow.
8. ServiceNow expresses declarative intent only. It can select a reviewed,
   versioned operation and profile, but cannot supply shell commands,
   PowerShell, JavaScript, Groovy, WQL, arbitrary OIDs, executable URLs, or
   other executable content.

## Architecture

```text
┌──────────────────────────────────────────────────────────────────────┐
│ Nischoy Topo scoped application on ServiceNow                       │
│                                                                      │
│ Profiles · target scopes · schedules · credential bindings          │
│ Runs · partitioned tasks · leases · results · retention             │
│ Worker health · IRE processing · audit · operator workspace         │
└──────────────────────────┬───────────────────────────────────────────┘
                           │ outbound HTTPS polling from each worker
                  ┌────────┴─────────┐
                  │ Scoped REST API  │
                  └────────┬─────────┘
             ┌─────────────┼──────────────────┐
             ▼             ▼                  ▼
     ┌──────────────┐ ┌──────────────┐  ┌──────────────┐
     │ Topo worker  │ │ Topo worker  │  │ Topo worker  │
     │ pool A/site 1│ │ pool A/site 1│  │ pool B/site 2│
     │ stateless    │ │ stateless    │  │ stateless    │
     └──────┬───────┘ └──────┬───────┘  └──────┬───────┘
            │ compiled-in, reviewed discovery operations │
            └────────────────┬────────────────────────────┘
                             ▼
                   destination-neutral observations
                             │
                             ▼
                    Nischoy result endpoint
                             │
              schema/mapping validation and deduplication
                             │
                  IRE preflight followed by apply
                             │
                             ▼
                 native CMDB CIs and relationships
```

Workers require no inbound listener. They initiate every connection to the
configured ServiceNow origin over verified HTTPS and refuse redirects. A
worker that cannot reach ServiceNow stops claiming work; it does not create a
local backlog.

## Deployment modes

### Standalone mode

An operator or an external scheduler runs Topo discovery and uses
`topo publish servicenow`. Topo calls the documented IRE REST API directly.
No scoped application is required. This mode is implemented today.

### ServiceNow-managed mode

The Nischoy Topo scoped application owns discovery configuration and runtime
state. One or more `topo worker run` processes poll the application, execute
leased tasks, and return destination-neutral observations. The application
performs the reviewed ServiceNow mapping and IRE submission. This document
defines that target mode.

The modes share discovery plugins, observation schemas, stable source identity,
credential-reference semantics, IRE mapping rules, and security bounds. They
must not develop separate discovery implementations.

## ServiceNow data placement

### Native platform and CMDB records

| Data | Native placement and rule |
| --- | --- |
| Configuration items | Appropriate `cmdb_ci*` classes, written only through IRE. |
| CI relationships | `cmdb_rel_ci`, written only through IRE. |
| Source identity | IRE-managed source records using stable `source_name` and `source_native_key`; worker identity must never become asset identity. |
| Users, roles, OAuth, ACLs | Native ServiceNow security facilities. |
| Application-record audit | Native auditing where it can be enabled without recording secret values or raw secret fields. |
| Bounded large result bodies | `sys_attachment` attached to a Nischoy result record when an ordinary field is insufficient; never an unbounded arbitrary upload. |

The application must use a documented IRE interface available to scoped
applications and must reproduce the established preflight-first behavior:
perform the non-committing equivalent of `queryEnhanced`, reject every warning
or error, and apply only a clean payload. The exact in-instance API and scoped
access must be verified against the real developer instance before coding it as
a product contract. The application never writes CMDB CI tables directly.

### Nischoy scoped application tables

The names below are proposed logical names. Confirm their final generated
scope prefix and field types during application implementation.

| Table | Purpose |
| --- | --- |
| `x_nischoy_topo_worker_pool` | A deployment zone, site, required capability set, concurrency policy, and allowed profile set. |
| `x_nischoy_topo_worker` | Ephemeral process identity, authenticated deployment identity, version, capabilities, current lease count, and last heartbeat. |
| `x_nischoy_topo_profile` | Immutable revision of a reviewed declarative discovery profile. |
| `x_nischoy_topo_target_scope` | CIDRs, exclusions, sites, and worker-pool bindings selected by an operator. |
| `x_nischoy_topo_schedule` | Recurrence, active window, profile revision, target scope, and enabled state. |
| `x_nischoy_topo_run` | One scheduled or manual execution and its terminal summary. |
| `x_nischoy_topo_task` | One bounded partition, state, attempt count, lease owner/token digest, lease expiry, deadline, and terminal result. |
| `x_nischoy_topo_result` | Idempotency key, chunk metadata, bounded summary, processing state, and optional attachment reference. |
| `x_nischoy_topo_credential_binding` | Mode, protocol, allowed profile/target scope, and reference to a protected ServiceNow credential record or external provider reference. |
| `x_nischoy_topo_ire_delivery` | Preflight/apply state, item/relation counts, bounded redacted diagnostics, and timing. |

Indexes and uniqueness constraints must cover schedule/run relationships, task
claim selection, lease expiry, `(task, attempt, chunk_number)`, and IRE delivery
idempotency. Records received through scoped REST resources are never accepted
through a generic Table API grant.

### Native Discovery tables that are deliberately not used

Do not insert or update:

- `ecc_queue` or `ecc_agent`;
- native Discovery Schedule or Discovery Status tables;
- native probe, pattern, or sensor definitions and runtime tables; or
- native MID capability, application, or IP-range relationships.

Those records participate in stock Discovery's ECC and sensor lifecycle.
Creating look-alike records without the associated supported contracts would
produce misleading UI state and release-dependent behavior. The Nischoy
application provides its own clearly labelled Topo Schedules, Topo Runs, Topo
Tasks, and Topo Workers views. A future documented ServiceNow extension point
may replace a custom table only after real evidence proves its semantics.

## Control-panel behavior

The Nischoy application provides:

- versioned discovery profiles with schema validation;
- target scopes and exclusions;
- worker pools, capabilities, version, health, and current load;
- schedules and an explicit **Run now** action;
- run progress, per-partition status, cancellation, retry, and failure detail;
- IRE preflight/apply outcomes and links to reconciled CIs;
- credential-binding administration separated from discovery-operation roles;
- retention configuration; and
- a complete audit trail for configuration changes, claims, credential access,
  cancellations, retries, and terminal outcomes without secret values.

ServiceNow is authoritative for whether a job should run. A worker is
authoritative for whether a requested operation and target are locally allowed
and safe to execute.

## Stateless worker contract

A worker starts with read-only deployment policy supplied by systemd,
Kubernetes, an external secret manager, or an equivalent mechanism:

- one absolute HTTPS ServiceNow origin;
- TLS trust configuration;
- an OAuth/workload-identity credential reference;
- worker pool and site labels;
- a local CIDR/hostname allowlist and explicit exclusions;
- external secret-provider configuration when used; and
- local ceilings for concurrency, deadlines, response sizes, target counts,
  result counts, and memory.

This is configuration, not durable operational state. Topo must not rewrite it.
If the local allowlist is unavailable or invalid, the worker fails closed.

The worker generates a random process/boot identifier in memory, reports its
compiled-in capabilities, and polls for work. It stores no worker registration
record locally. On restart it registers as a new process under the authenticated
deployment identity.

The worker never writes:

- a SQLite or other local database;
- a task/claim journal;
- an observation or result spool;
- discovery credentials;
- schedules or profiles; or
- a retry queue.

Secrets and observations exist only in bounded process memory for the current
attempt. Best-effort memory clearing does not replace process isolation, least
privilege, or host security.

## Scoped REST surface

Exact names may change, but the semantic surface is fixed and deliberately
small:

| Resource | Purpose |
| --- | --- |
| `POST /workers/register` | Record process identity, version, capabilities, pool, and local policy digest. |
| `POST /workers/heartbeat` | Report liveness and load; receive cancellation hints. |
| `POST /tasks/claim` | Atomically lease one eligible bounded task. |
| `POST /tasks/{id}/renew` | Extend an owned lease and observe cancellation. |
| `POST /tasks/{id}/credential` | Resolve the task's authorized ServiceNow-managed credential for the current attempt. |
| `POST /tasks/{id}/results` | Upload one bounded, checksummed, idempotent observation chunk. |
| `POST /tasks/{id}/complete` | Declare success or a structured redacted failure after all chunks are acknowledged. |

Every resource requires a narrowly scoped worker identity. OAuth token
restrictions and API access policies must allow only these methods and
resources. A worker identity does not receive generic Table API, CMDB, IRE,
application-administration, schedule, credential-table, or reporting access.

Requests and responses are bounded and cancellable. Instance URLs reject
userinfo, paths, queries, and fragments; clients refuse redirects. Responses
set `Cache-Control: no-store` where they can contain task or credential data.

## Declarative task contract

A task is immutable after it is leased and contains identifiers and bounded
policy, not executable text. A representative wire object is:

```json
{
  "task_id": "uuid",
  "run_id": "uuid",
  "attempt_id": "uuid",
  "lease_token": "random-secret",
  "lease_expires_at": "2026-08-29T22:00:00Z",
  "operation": "lan_discovery.v1",
  "profile_id": "uuid",
  "profile_revision": 4,
  "target_partition": {
    "cidrs": ["192.168.10.0/26"]
  },
  "credential_binding_id": "uuid-or-empty",
  "deadline": "2026-08-29T22:05:00Z"
}
```

The supported operations form a compiled-in registry. Initial implementation
uses `local.v1`; later reviewed slices may add `lan_discovery.v1`,
`ssh_linux.v1`, `winrm.v1`, `snmpv3.v1`, or other existing plugins. A worker
rejects unknown operation versions with a bounded result so the task does not
hang.

The application validates profile fields against the operation version's
schema. An operator cannot turn a fixed field into arbitrary command, script,
query, OID, URL, or payload content. Worker-local maximums override every
requested limit.

Requested targets are intersected with the worker's deployment-controlled
allowlist. A ServiceNow target-scope record is selection policy, not complete
network authorization. An empty intersection fails visibly without making a
network connection.

## Partitioning, leasing, and horizontal scale

The application expands one run into bounded independent tasks. Partitioning
is deterministic for a profile revision and target-scope revision. It avoids a
single record that can monopolize one worker and permits any eligible worker to
claim the next partition.

The state machine is:

```text
planned → ready → leased → running → results_received → ire_processing
                    │                                  │
                    │                                  ├→ complete
                    │                                  └→ failed
                    ├→ cancelled
                    └→ lease expired → ready (new attempt)
```

The claim resource, not the worker, selects the record and establishes its
lease. Its implementation must provide an atomic conditional transition from
`ready` to `leased` and return a random lease token exactly once to the caller.
Only a digest of that token is stored. A plain read followed by an unrelated
record update is not sufficient concurrency control. The exact ServiceNow
transaction/locking technique must be proven with a real-instance race test in
which many clients claim the same available task.

The system promises at-least-once execution, not exactly once. If a worker
crashes or loses connectivity, the lease expires and another worker creates a
new attempt. Duplicate work is safe because:

- result ingestion is unique by `(task_id, attempt_id, chunk_number)`;
- late results require the matching live lease and attempt;
- Topo asset identity is stable across workers and attempts;
- worker/process identity never enters `source_native_key`;
- mapping deduplicates items and relationships within each delivery; and
- IRE reconciles repeated stable source identity.

Cancellation is cooperative. A cancelled or expired task causes renew and
result endpoints to reject new work; the worker cancels its operation context
and reports a bounded terminal outcome when still connected.

## Result ingestion and IRE

Workers submit destination-neutral Topo observations. They do not submit
arbitrary ServiceNow class names, CMDB field names, relationship types, or IRE
payloads.

For each chunk the application:

1. authenticates the worker and verifies the current task, attempt, and lease;
2. enforces content type, byte, nesting, envelope, item, relationship, and
   chunk-number bounds;
3. verifies the checksum and idempotency key;
4. validates the Topo observation schema;
5. stores the bounded chunk and acknowledges it;
6. maps only reviewed Topo asset/relationship types to reviewed ServiceNow
   classes, fields, and relationships;
7. deduplicates stable source identities;
8. performs IRE preflight and refuses to apply on every warning, error,
   oversized response, or ambiguous outcome;
9. applies only a clean preflight payload; and
10. records bounded redacted diagnostics and links the run to resulting CIs
    when a supported response contract permits it.

The first implementation reuses only the real-instance-validated
`cmdb_ci_computer`, `cmdb_ci_network_adapter`, and `Owns::Owned by` mapping.
Every additional class or relationship is a focused slice with real
preflight, apply, repeat-reconciliation, dependency, and error evidence.

## Result retention

All durable result state is in ServiceNow. Recommended defaults are:

| Data | Successful terminal run | Failed or ambiguous run |
| --- | --- | --- |
| Raw result chunks/attachments | 24 hours | 7 days |
| Bounded IRE diagnostics | 30 days | 30 days |
| Run/task summaries and audit | Customer lifecycle policy | Customer lifecycle policy |
| CMDB CIs, relationships, and source identity | Native CMDB lifecycle | Not applicable |

Retention is configurable within hard application bounds. A cleanup process
deletes an attachment before its result metadata record. It deletes successful
raw data only after the run has a durable terminal IRE outcome. Failed data is
never retained indefinitely by accident. Cleanup actions are audited with IDs
and counts, not payload content.

If "all raw data forever" becomes a requirement, it needs a separate capacity,
cost, legal-retention, and ServiceNow table/attachment performance decision; it
is not part of this architecture.

## Credential bindings

A task contains only a credential-binding identifier. It never contains a
password, private key, token, or decrypted credential.

### ServiceNow-managed encrypted credentials

The scoped application uses a dedicated protected credential record with
metadata such as protocol, username, active state, allowed profile, and allowed
target scope. Secret fields use ServiceNow's supported encrypted field type
(including Password2 where appropriate), never a plain string.

Required controls:

- the credential table is unavailable through the generic Table API;
- default read, list, report, export, and attachment access is denied;
- UI access requires a dedicated credential-administrator role;
- secret fields are excluded from auditing, history, notifications, display
  values, reference qualifiers, and logs;
- only a server-side credential broker may access the decrypted value;
- the broker verifies authenticated worker identity, live task/attempt/lease,
  protocol, profile, and target-scope binding before release;
- credential retrieval is audited without the value; and
- the response is bounded, redacted on error, marked `no-store`, and usable
  only for the current attempt.

Topo adds a ServiceNow credential-reference provider whose input is a binding
identifier and whose resolver calls the broker just in time. It retains the
resolved value only in operation memory. The exact supported scoped API for
reading Password2 fields, encryption behavior, ACL behavior, clone/backup
implications, and application upgrade behavior must be tested against the real
developer instance. Do not use an undocumented decryption trick or claim that
Password2 provides an external-vault security boundary.

### External secret provider

The binding stores a non-secret provider reference such as:

```text
vault:customer/topo/linux#password
```

The worker resolves the reference using its deployment workload identity and
Topo's existing bounded provider implementation. ServiceNow chooses the
binding, but the secret value never transits or persists in ServiceNow. Initial
support reuses `vault:` and `k8s:` providers; other enterprise providers require
focused adapters and tests.

### Credential-mode acceptance matrix

Both modes must prove:

- correct credentials complete the same compiled-in operation;
- wrong, missing, revoked, or inaccessible credentials produce redacted
  structured errors;
- an expired lease, another worker, another attempt, or a cancelled task cannot
  obtain a ServiceNow-managed credential;
- a binding cannot be used outside its protocol, profile, or target scope;
- secret values do not appear in tasks, results, observations, attachments,
  IRE payloads, errors, logs, labels, metrics, audit, or test output;
- timeouts and cancellation cover credential resolution as well as discovery;
- a worker crash leaves no durable secret or result locally; and
- independently repeated discovery preserves stable identity and IRE
  reconciliation.

## Security and authorization

ServiceNow roles are separated at minimum into:

- Topo administrator: application configuration and worker pools;
- Topo operator: schedules, run-now, cancellation, and retry;
- Topo credential administrator: credential records and bindings;
- Topo viewer/auditor: read-only status and audit; and
- Topo worker: only the fixed scoped REST resources required by workers.

An OAuth/workload identity is assigned per worker deployment or sufficiently
narrow worker pool, not shared across an entire customer without operational
need. Short-lived access tokens are preferred. Token restrictions and API
access policies are exact rather than global.

A ServiceNow compromise cannot override the worker's compiled operation set,
local allowlist, TLS verification, concurrency ceiling, response limits, or
deadline maximum. Conversely, a compromised worker credential cannot
administer schedules, read arbitrary tables, invoke IRE directly, enumerate or
edit credential records, or claim tasks assigned to another pool.

Discovery plugins retain their existing protocol boundaries: fixed SSH
commands, fixed WinRM tuples, fixed SNMP OIDs, read-only VMware properties,
verified TLS and host identity, bounded reads/results, controlled concurrency,
and no arbitrary operation supplied by a task.

## Failure model

No worker-side spool is a deliberate trade-off:

- ServiceNow unavailable before claim: the worker waits and no work starts.
- ServiceNow unavailable during discovery: the operation is cancelled when its
  lease cannot be safely renewed, subject to a short bounded grace policy.
- Worker crashes during discovery: the lease expires and another attempt runs.
- Result accepted but acknowledgement lost: re-uploading the same chunk is an
  idempotent acknowledgement, not a duplicate record.
- Discovery completes but no result was accepted: memory is lost, the lease
  expires, and the entire task is repeated.
- IRE outcome ambiguous: do not blindly replay apply; retain bounded evidence
  and mark the task for controlled investigation/reconciliation.
- ServiceNow restarts: durable task/lease/result state remains authoritative;
  expired leases are recovered by the scheduler/reaper.

This architecture favors simple disposable workers and durable centralized
state over preserving unacknowledged work during a ServiceNow outage.

## Observability

The application exposes, without secret or raw payload content:

- healthy/stale workers by pool, site, version, and capability;
- ready/leased/running/expired/failed task counts;
- claim latency, discovery duration, chunk ingestion, and IRE duration;
- retry, cancellation, allowlist-denial, credential-resolution, and IRE error
  categories;
- assets and relationships considered, accepted, rejected, and reconciled;
- retained raw-result volume and upcoming deletion counts; and
- immutable identifiers linking schedule, run, task, attempt, result, IRE
  delivery, and audit records.

Diagnostic strings are structured, short, redacted, and bounded. Observation
attributes, target responses, and secret-provider errors are never promoted to
metric labels.

## Implementation slices

### Slice A: smallest control-plane vertical slice

Use only `local.v1` and the already validated computer/adapter/ownership IRE
mapping:

1. Add application tables, roles, ACLs, schedule/run/task creation, worker
   registration, atomic claim/lease, result ingestion, IRE processing, run
   summary, and retention.
2. Add stateless `topo worker run` with no spool or local state.
3. Exercise one manual run and one schedule from ServiceNow through a real
   worker to IRE and an entirely terminal run.
4. Crash the worker before result upload and prove lease expiry/re-execution.
5. Run two workers against one task and prove one live lease plus idempotent
   recovery.

### Slice B: worker-pool scale and partitioning

Add deterministic target partitions, load-aware claims, cancellation, lease
renewal, concurrency tests, retention-volume tests, and simulator gates for
1K/10K/100K assets. No new discovery protocol is required to prove scheduling
and storage scale.

### Slice C: credentials

Implement and test ServiceNow-managed Password2-backed bindings and external
Vault-backed bindings. Use one existing authenticated protocol such as Linux
SSH, with target scopes constrained by the local allowlist. Record real
ServiceNow encryption/ACL/broker evidence separately from simulator evidence.

### Slice D: credentialless LAN discovery

Add an opt-in compiled-in operation with bounded ARP/NDP and a fixed reviewed
network detection policy. Treat reachability and classification hints as
evidence, not authoritative computer identity. Define provisional asset
semantics, freshness/expiry, and a reviewed ServiceNow class/mapping before any
new CI is applied.

### Later protocol slices

Add SSH, WinRM, SNMPv3, VMware, cloud, and Kubernetes managed-mode operations
one at a time by reusing existing plugins. Each operation requires schema,
allowlist, credential, cancellation, fault-isolation, repeat-identity, IRE
mapping, and real-protocol evidence appropriate to that integration.

## Slice A acceptance gates

- ServiceNow is the only durable task/result store.
- Two or more fresh worker processes can share a pool without local identity or
  state directories.
- A ServiceNow schedule and **Run now** action both create a run and bounded
  task.
- Exactly one current lease is granted in a high-concurrency real-instance
  claim test; expired work is recoverable by a new attempt.
- The worker accepts only `local.v1`, enforces local policy, and rejects
  arbitrary or unknown operations visibly.
- Result chunk ingestion is bounded, authenticated, checksummed, and
  idempotent.
- The application maps only computer, adapter, and ownership data; IRE
  preflight gates every apply.
- An identical repeat reconciles without duplicate CIs or relationships.
- Successful raw chunks expire only after a durable IRE outcome; failed chunks
  follow the bounded failure-retention policy.
- Worker credentials cannot call the Table API, IRE, application administration,
  schedule, or credential-record resources.
- Secrets and sensitive results do not appear in logs, errors, audit, labels,
  observations, tasks, or IRE payloads.
- Exact Go 1.25.13 format, focused/full race tests, vet, Linux/macOS build,
  Windows amd64 vet/build, and the pinned security-review gate pass.
- Simulator evidence and real ServiceNow evidence are recorded separately.

## Deliberate non-goals

- No `ecc_queue`, MID registration, native Discovery selector, native Discovery
  Schedule, native Discovery Status, probe, pattern, or sensor compatibility.
- No arbitrary command, script, WQL, OID, URL, class, field, relationship, or
  executable payload supplied by ServiceNow.
- No worker-side durable state or offline delivery guarantee.
- No direct CMDB table writes.
- No claim that Password2 is equivalent to an external vault.
- No indefinite raw-result retention.
- No new IRE class or relationship without focused real-instance evidence.
- No production signing, public package-channel promotion, or M2.5 independent
  retest work as part of this architecture slice.

## Evidence still required

Before managed mode is called supported, obtain real ServiceNow evidence for:

- scoped-app installation, upgrade, uninstall, roles, ACLs, and generic Table
  API denial;
- atomic multi-worker claim behavior and lease-expiry recovery;
- the documented scoped-app IRE call and preflight/apply behavior;
- worker OAuth token restrictions and exact API access policies;
- Password2 creation, encryption, supported scoped access, broker release,
  backup/clone behavior, and denial paths;
- external Vault binding with a real short-lived/rotated credential;
- raw attachment/table volume and retention cleanup;
- a scheduled and manual `local.v1` run that reconcile without duplicates; and
- worker crash, ServiceNow outage, ambiguous IRE response, cancellation, and
  retry behavior.

Simulator tests are required for deterministic CI but are never a substitute
for these real-instance findings.

## Relationship to earlier ServiceNow work

- `topo publish servicenow` and its real IRE evidence remain the mapping and
  publication foundation.
- The experimental Relay demonstrates outbound polling, fixed profiles, and
  result reporting, but its locally configured profiles and encrypted local
  spool conflict with this architecture. Reuse reviewed components where
  appropriate; do not promote its state model.
- The ECC experiment remains historical protocol evidence only. No ECC topic
  translator is used here.
- Destination-neutral observations remain the boundary between discovery and
  ServiceNow mapping.
