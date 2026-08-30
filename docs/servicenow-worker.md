# ServiceNow-managed stateless Topo worker

## Status

M3 Slice A implements the first bounded vertical slice of the architecture in
[`servicenow-control-plane.md`](servicenow-control-plane.md). The Go worker,
the source-driven ServiceNow Fluent application, and deterministic simulator
evidence are implemented. The Fluent application was installed from source on
the real developer instance under its required company-prefixed scope,
`x_664635_topo`. The separate 2026-08-30 real-instance evidence below validates
the bounded runtime path; installed metadata and simulator output are not used
as substitutes for that evidence.

Slice A supports exactly one operation: `local.v1`. It discovers the machine
running the worker, returns a destination-neutral Topo observation, and lets
the Nischoy Topo scoped application validate, map, preflight, and apply that
observation through IRE. It does not contain discovery credentials or remote
targets; those belong to later slices.

## Components

The reviewable, installable scoped-application source is under
`integrations/servicenow/topo-control-plane/`:

- `src/fluent/*.now.ts` is the authoritative ServiceNow Fluent definition of
  the tables, indexes, roles, ACLs, application menu, Script Includes,
  six-route Scripted REST API, scheduled scripts, IRE cross-scope privilege,
  immutable-profile rule, and **Run now** UI action;
- `now.config.json`, `package.json`, and `package-lock.json` make the
  application reproducibly buildable with exactly ServiceNow SDK 4.9.0;
- `application.json` is a test-enforced review contract summarizing that
  deployable Fluent surface; it is not an installer;
- `TopoControlPlane.js` owns worker registration, heartbeat, run/task creation,
  conditional claims, leases, result ingestion, terminal summaries, lease
  recovery, and retention;
- `TopoObservationMapper.js` validates the destination-neutral observation and
  maps only `host` to `cmdb_ci_computer`, `network_interface` to
  `cmdb_ci_network_adapter`, and `host_has_interface` to `Owns::Owned by`;
- `TopoIREProcessor.js` reads the bounded result attachment, repeats checksum
  validation, invokes scoped `identifyCIEnhanced` before
  `createOrUpdateCIEnhanced`, rejects every reported warning/error, and records
  a non-replayable ambiguous outcome if an apply response is missing or
  malformed; and
- six small REST wrappers expose only registration, heartbeat, claim, renewal,
  result ingestion, and completion.

The older Relay and MID experiments retain their original
`x_nischoy_topo` metadata. The real developer instance rejected that prefix
for a newly created application because its assigned company code is `664635`;
Slice A therefore uses `x_664635_topo` as its installed API and table contract
without rewriting either experiment.

The worker implementation is `internal/worker`, with the CLI entry point
`topo worker run`. The `internal/worker/controlsim` server is a deterministic,
in-memory contract fixture for CI. It is not a ServiceNow emulator and makes no
claim about scoped Glide APIs, ServiceNow transactions, ACL enforcement, or
IRE behavior.

## Scoped data model

The Nischoy application is the sole durable operational store. Slice A creates
these audited scoped records:

| Table | Slice A purpose |
| --- | --- |
| `x_664635_topo_worker_pool` | Site, authenticated deployment user, concurrency, lease, and task-duration policy. |
| `x_664635_topo_worker` | Ephemeral boot identity, version, fixed capability, policy digest, load, and heartbeat. |
| `x_664635_topo_profile` | Immutable versioned `local.v1` profile bound to one pool. |
| `x_664635_topo_schedule` | Recurrence and next-run time for a profile revision. |
| `x_664635_topo_run` | Manual/scheduled execution state and bounded terminal counts/error. |
| `x_664635_topo_task` | One local partition, attempt, digest-only lease, deadline, state, and bounded error. |
| `x_664635_topo_result` | Unique chunk metadata, checksum, bounded attachment reference, processing outcome, and expiry. |
| `x_664635_topo_ire_delivery` | Unique attempt delivery, preflight/apply state, counts, and bounded diagnostics. |

The important unique keys are `(profile_id, revision)`,
`(task, attempt_id, chunk_number)`, and `(task, attempt_id)` for IRE delivery.
Claim selection is indexed by `(worker_pool, state, sys_created_on)` and expired
leases by `(state, lease_expires)`.

The application roles are:

- `x_664635_topo.admin`: pool/application configuration and cleanup authority;
- `x_664635_topo.operator`: profile/schedule configuration and **Run now**;
- `x_664635_topo.viewer`: read-only operational visibility; and
- `x_664635_topo.worker`: the six Scripted REST resources only.

The worker role receives no generic table, CMDB, IRE, reporting, schedule, or
application-administration grant. The worker OAuth/API access policy must allow
only the six methods beneath `/api/x_664635_topo/v1/tasks`. A pool record binds one
ServiceNow integration user to the pool and site; every resource resolves
`gs.getUserID()` through that binding. Do not reuse the direct IRE publisher's
OAuth client unless a separate review proves its exact policy—Slice A expects
a distinct worker identity and the worker itself never calls IRE.

## Run, claim, and recovery behavior

**Run now** and the minute schedule evaluator both create one run plus one
bounded `local.v1` task. Slice A suppresses another active run for the same
profile revision. A task deadline is fixed when it is created.

Claiming uses a conditional `GlideRecord.updateMultiple()` whose query includes
the candidate `sys_id` and `u_state=ready`. Only the process whose fresh
attempt ID survives that compare-and-swap receives the random lease token.
The application stores only its SHA-256 digest. A 32-competitor real-instance
race produced one winner and one attempt, as recorded below.

Delivery is at-least-once:

1. A worker registers a random in-memory boot ID and polls outbound over HTTPS.
2. The application returns one fixed, declarative `local.v1` task and a live
   lease.
3. The worker discovers locally in bounded memory and uploads one checksummed
   JSON observation string.
4. Repeating the same `(task, attempt, chunk 0)` and checksum acknowledges the
   existing result; different content for that key is rejected.
5. Completion performs application-side schema/mapping validation, IRE
   preflight, and then one apply.
6. If a worker crashes, the application moves the expired lease back to
   `ready`; the next claimant receives a new attempt ID and token. Late results
   from the old attempt fail lease validation.

The worker never stores a task, result, token, schedule, retry decision, or
observation on disk. If delivery acknowledgement is lost, it retains nothing;
ServiceNow's lease expiry is the retry mechanism. Worker process identity never
enters `source_native_key`; the pool-stable collector ID used in the Topo
envelope is not a CMDB identity either.

## Running a worker

Provision a dedicated ServiceNow integration identity first, bind it to one
active worker-pool record, grant only `x_664635_topo.worker`, and restrict its
OAuth token to the six Scripted REST resources. Supply the resulting token via
the shared credential-reference contract; never put its value on the command
line.

```sh
export SERVICENOW_INSTANCE_URL=https://instance.service-now.com

topo worker run \
  -token-ref file:/run/secrets/topo-servicenow-worker-token \
  -worker-pool site-a-local \
  -site site-a \
  -allow-local
```

`-servicenow-instance` must be one absolute HTTPS origin with no userinfo,
path, query, or fragment. Redirects are refused. `-worker-pool`, `-site`,
`-allow-local`, `-poll-interval`, and `-max-task-duration` are read-only local
policy. There is intentionally no state/spool/database/journal flag and no
inbound listener.

`-allow-local` is explicit because even the one compiled-in operation requires
deployment authorization. ServiceNow can select `local.v1`, but it cannot
expand the worker's local authority or supply a target, command, script, query,
OID, URL, class, field, relationship, or executable payload.

## IRE and retention

Raw observation JSON is stored as one bounded `.json` attachment on the result
record. Completion reads it through scoped `GlideSysAttachment`, verifies its
checksum again, and maps only the three real-instance-validated constructs.
The application never opens a `GlideRecord` on a CMDB CI or relationship table.
It calls the documented scoped `sn_cmdb.IdentificationEngine` interface with
the fixed source `Nischoy Topo`.

Successful raw result records and attachments default to 24-hour retention.
Rejected, failed, superseded, or ambiguous attempts default to seven days.
Maintenance deletes an attachment first, verifies it is gone, and only then
deletes the scoped result record. Runs, bounded summaries, IRE delivery
outcomes, and reconciled CMDB state remain. An interrupted or ambiguous apply
is marked for operator investigation and is never replayed automatically.

## Verification

Focused local gates are:

```sh
(
  cd integrations/servicenow/topo-control-plane
  npm ci --ignore-scripts
  npm run build
)
for file in integrations/servicenow/topo-control-plane/scripts/*.js; do
  node --check "$file"
done
env GOTOOLCHAIN=go1.25.13 go test -race ./internal/worker/... ./cmd/topo
```

Application creation and updates use `now-sdk install` from that directory.
Do not recreate the Fluent-owned metadata by clicking through Studio forms,
running a background script, importing hand-written update-set XML, or writing
metadata through the Table API.

The simulator suite separately proves:

- manual and scheduled execution reach a terminal summarized run;
- 32 concurrent workers produce exactly one live claim;
- lease expiry after a simulated crash creates a fresh attempt/token;
- result chunks are checksum-, lease-, and attempt-bound and idempotent;
- repeat stable observations produce simulated `NO_CHANGE` operations; and
- raw cleanup preserves run summaries.

### Real ServiceNow evidence — 2026-08-30

This evidence is from the Australia-release developer instance
`dev441060.service-now.com`, not `controlsim`:

- Fluent SDK install/update created application
  `d4e2151fdcbc7d97f8c155d1ba873e46`; the installed metadata has eight scoped
  tables, four roles, 23 ACLs, six versioned authenticated POST resources,
  three package-private Script Includes, two schedules, and only the reviewed
  IRE cross-scope execute privilege. Application creation and updates used
  Fluent, not Studio clicks or hand-written update-set XML.
- The dedicated internal-integration identity has only
  `x_664635_topo.worker` as its application role. Six exact Global REST auth
  scopes and API access policies cover only
  `/api/x_664635_topo/v1/tasks/...`; the same short-lived client-credentials
  token received HTTP 401 from an unrelated Table API. Credentials remained
  in owner-only files and were never printed.
- Manual **Run now** produced run `4e27ea09930b8790ec251aebb9373c60`.
  It completed with 22 items and 21 relationships. Scheduled runs
  `e2f726c9930b8790ec251aebb9373cf7` and
  `4538220d930b8790ec251aebb9373c43` independently completed with the same
  22/21 summary; the one-minute proof schedule was then disabled.
- Thirty-two simultaneously registered competitors claimed task
  `5f8ae681934b8790ec251aebb9373c59`: exactly one response contained the task,
  the durable attempt count was one, and the winning lease completed. A
  separate two-process race also produced one attempt on task
  `8b682a09930b8790ec251aebb9373c63`.
- A crash fixture claimed task `3cd92201934b8790ec251aebb9373c5b`
  and exited without renew, result, or completion. A fresh worker boot claimed
  it at the 30-second expiry; attempt two completed with a 22/21 applied IRE
  delivery.
- On task `b1396605930b8790ec251aebb9373cd9`, the first result upload returned
  HTTP 201 with `duplicate:false`; the identical retry returned HTTP 200 with
  `duplicate:true`; exactly one result row existed, and completion succeeded.
- Repeated manual, scheduled, race, and recovery deliveries kept the same
  source identities. Each real IRE preflight and apply was clean; later
  diagnostics reported 22 `UPDATE` item operations and 21 `NO_CHANGE`
  relationship operations, rather than duplicate relationships or new source
  identities.
- For retention, the successful idempotency result's 24-hour deadline was
  backdated and the Fluent-installed maintenance job was executed once. The
  raw result row and attachment both disappeared, while its completed task,
  completed 22/21 run summary, and applied IRE-delivery record remained.

The real run also exposed two native wire details now covered by focused tests:
Scripted REST success bodies use the strict `{ "result": ... }` envelope, and
ServiceNow serializes an integral Glide value as `1.0`. The client accepts only
that bounded envelope and integral numeric form; unknown envelope or task
fields remain rejected.

## Slice A exclusions

Slice A has no credential-binding or target-scope table, Password2/Vault
resolution, remote discovery protocol, target partitioning, worker-side spool,
offline guarantee, stock Discovery integration, ECC record, MID behavior,
native Discovery Schedule/Status record, probe, pattern, or sensor. The older
Relay and MID artifacts remain intact and experimental.
