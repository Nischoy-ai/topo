# ServiceNow-managed stateless Topo worker

The unpublished 0.4.7 source candidate adds managed Windows computer/interface
inventory and bounded IPv4 subnet selection. Its implementation and direct
real-host evidence are separate from the published workflow below; see
[candidate scope and acceptance](mixed-inventory-pilot.md).

## Supported published workflow

The stateless worker supports two reviewed operations: `local.v1` discovers
its own machine and `ssh_linux.v1` discovers explicitly approved IPv4 Linux
targets over port 22. Workers connect outbound and check a deployment-owned
target allowlist and SSH `known_hosts`. ServiceNow validates normalized
observations and publishes supported CI mappings through IRE.

Topo 0.4.6 Beta is installed from one combined XML containing the
app and its 32 required indexes. Install the worker through the Beta package
channel. Follow the [installation guide](servicenow-update-set.md) and
[setup guide](pilot-quickstart.md) for configuration.

Implemented controls include deterministic partitions, bounded concurrency,
renewable leases, cancellation, raw-result retention, Password2 credential
storage and an attempt-bound broker. Workers cannot read the credential table.
The [deployment security guide](deployment-security.md#where-ssh-passwords-live)
explains password storage, administrator trust, rotation and deployment modes.
The [package validation record](servicenow-validation.md) describes current
installation and Linux workflow evidence. Dated protocol, security and upgrade results are kept in the
[worker evidence record](evidence/servicenow-worker.md).

## Components

The reviewable, installable scoped-application source is under
`integrations/servicenow/topo-control-plane/`:

- `src/fluent/*.now.ts` is the authoritative ServiceNow Fluent definition of
  the tables, indexes, roles, ACLs, application menu, Script Includes,
  seven-route Scripted REST API and scheduled scripts,
  immutable profile/target-scope/credential-binding rules, **Run now**, and **Cancel run** UI actions;
- `now.config.json`, `package.json`, and `package-lock.json` make the
  application reproducibly buildable with exactly ServiceNow SDK 4.9.0;
- `application.json` is a test-enforced review contract summarizing that
  deployable Fluent surface; it is not an installer;
- `TopoControlPlane.js` owns worker registration, heartbeat, run/task creation,
  conditional claims, unique capacity-slot reservations, renewable leases,
  attempt-bound Password2 resolution and access audit, cancellation, result
  ingestion, terminal summaries, lease recovery, and retention;
- `TopoObservationMapper.js` validates the destination-neutral observation and
  maps only `host` to `cmdb_ci_computer`, `network_interface` to
  `cmdb_ci_network_adapter`, and `host_has_interface` to `Owns::Owned by`;
- `TopoIREProcessor.js` reads the bounded result attachment, repeats checksum
  validation, invokes scoped `identifyCIEnhanced` before
  `createOrUpdateCIEnhanced`, rejects every reported warning/error, and records
  a non-replayable ambiguous outcome if an apply response is missing or
  malformed; and
- seven small REST wrappers expose only registration, heartbeat, claim,
  renewal, attempt-bound credential resolution, result ingestion, and
  completion.

The managed application uses `x_664635_topo` for its API and table contract.
The older Relay and MID experiments retain their separate `x_nischoy_topo`
metadata; installation does not migrate or rewrite them.

The worker implementation is `internal/worker`, with CLI entry points
`topo worker check` and `topo worker run`. `check` performs only registration
and one zero-lease heartbeat; it never enters the claim loop. The
`internal/worker/controlsim` server is a deterministic,
in-memory contract fixture for CI. It is not a ServiceNow emulator and makes no
claim about scoped Glide APIs, ServiceNow transactions, ACL enforcement, or
IRE behavior.

## Scoped data model

The Nischoy application is the sole durable operational store. Fluent `0.4.6`
defines these scoped records:

| Table | Purpose |
| --- | --- |
| `x_664635_topo_worker_pool` | Site, authenticated deployment user, concurrency, lease, and task-duration policy. |
| `x_664635_topo_worker` | Ephemeral boot identity, version, fixed capability, policy digest, advertised capacity, authoritative current load, and heartbeat. |
| `x_664635_topo_target_scope` | Immutable revision of bounded canonical IPv4 selection/exclusion policy and its deterministic partition-plan digest. It is not used by production `local.v1`. |
| `x_664635_topo_ssh_credential` | Credential-admin-only SSH username plus non-audited, non-replicated Password2 secret. Generic web-service access is disabled. |
| `x_664635_topo_credential_binding` | Immutable revision binding one `ssh_password` credential to one profile revision and target scope. Contains no plaintext secret. |
| `x_664635_topo_credential_access` | Secret-free allowed/denied attempt-bound broker events. |
| `x_664635_topo_profile` | Immutable versioned `local.v1` or `ssh_linux.v1` profile bound to one pool. |
| `x_664635_topo_schedule` | Recurrence and next-run time for a profile revision. |
| `x_664635_topo_run` | Manual/scheduled execution, cancellation state, and bounded terminal counts/error. |
| `x_664635_topo_task` | One immutable partition descriptor, attempt, digest-only lease, unique pool/worker capacity slots, deadline, cancellation state, and bounded error. |
| `x_664635_topo_result` | Unique chunk metadata, checksum, bounded attachment reference, processing outcome, and expiry. |
| `x_664635_topo_ire_delivery` | Unique attempt delivery, preflight/apply state, counts, and bounded diagnostics. |

The important unique keys are `(profile_id, revision)`, `(scope_id, revision)`,
`(task, attempt_id, chunk_number)`, and `(task, attempt_id)` for IRE delivery.
Active task rows reserve one globally unique pool lease slot and one globally
unique worker lease slot. Claim selection is indexed by
`(worker_pool, state, partition_ordinal, sys_created_on)` and expired leases by
`(state, lease_expires)`.

The application roles are:

- `x_664635_topo.admin`: pool/application configuration and cleanup authority;
- `x_664635_topo.credential_admin`: protected SSH credential and binding authority;
- `x_664635_topo.operator`: profile/schedule configuration and **Run now**;
- `x_664635_topo.viewer`: read-only operational visibility; and
- `x_664635_topo.worker`: the seven Scripted REST resources only.

The worker role receives no generic table, CMDB, IRE, reporting, schedule, or
application-administration grant. The worker OAuth/API access policy must allow
only the seven methods beneath `/api/x_664635_topo/v1/tasks`. A pool record binds one
ServiceNow integration user to the pool and site; every resource resolves
`gs.getUserID()` through that binding. Do not reuse the direct IRE publisher's
OAuth client. Use a distinct worker identity; the worker itself never calls IRE.

## Run, claim, and recovery behavior

**Run now** and the minute schedule evaluator both create one durable run. A
`local.v1` run has one targetless task. An `ssh_linux.v1` run has one task per
compiled `/32`, with a 1,024-task ceiling and the profile's immutable
credential binding copied onto every task. The app suppresses another active run for the same
profile revision. A task deadline is fixed when it is created.

Claiming uses a conditional `GlideRecord.updateMultiple()` whose query includes
the candidate `sys_id` and `u_state=ready`. Only the process whose fresh
attempt ID survives that compare-and-swap receives the random lease token.
The application stores only its SHA-256 digest. A 32-competitor real-instance
race produced one winner and one attempt, as recorded below.

Each active task has two unique nullable capacity-slot keys. A claim reserves
one slot from the pool ceiling and one from the registered worker ceiling in
the same conditional task transition. Database uniqueness resolves concurrent
slot contenders; terminal completion, cancellation, and expiry release both
slots. The worker independently enforces `-max-concurrency` (1 by default,
maximum 32), reports current in-memory leases, and never trusts the server to
expand that local ceiling.

Delivery is at-least-once:

1. A worker registers a random in-memory boot ID and polls outbound over HTTPS.
2. The application returns one fixed, declarative task and a live lease. An
   SSH task contains exactly one canonical IPv4 `/32` plus an opaque reviewed
   credential-binding ID—never a command, port, URL, or executable payload.
3. Before credential retrieval or dialing, the worker proves that the address
   is inside its read-only local CIDR allowlist. The live attempt then calls
   the fixed credential route; the app revalidates the user, pool, worker,
   boot, task, attempt, lease, operation, profile, scope, and binding before
   decrypting Password2. The response is `no-store`, retained in memory for
   the attempt, and never copied into an observation or error.
4. The worker executes the existing fixed SSH command set on port 22 with
   local `known_hosts` verification and bounded time/output/concurrency, then
   uploads one checksummed JSON observation string.
5. Repeating the same `(task, attempt, chunk 0)` and checksum acknowledges the
   existing result; different content for that key is rejected.
6. Completion performs application-side schema/mapping validation, IRE
   preflight, and then one apply.
7. If a worker crashes, the application moves the expired lease back to
   `ready`; the next claimant receives a new attempt ID and token. Late results
   from the old attempt fail lease validation.
8. Long tasks renew at half of the remaining lease (at most every 30 seconds).
   If renewal cannot succeed by expiry, the operation context is cancelled and
   no worker-local retry state is created.
9. **Cancel run** terminalizes unleased partitions immediately and marks active
   attempts for cooperative cancellation. Heartbeats and renewals carry the
   cancellation hint; late result and successful completion calls are rejected.

The worker never stores a task, result, token, schedule, retry decision, or
observation on disk. If delivery acknowledgement is lost, it retains nothing;
ServiceNow's lease expiry is the retry mechanism. Worker process identity never
enters `source_native_key`; the pool-stable collector ID used in the Topo
envelope is not a CMDB identity either.

## Running a worker

Provision a dedicated ServiceNow integration identity first, bind it to one
active worker-pool record, grant only `x_664635_topo.worker`, and restrict its
OAuth token to the seven Scripted REST resources. Supply the resulting token via
the shared credential-reference contract; never put its value on the command
line.

Validate the same read-only startup policy without claiming work first:

```sh
topo worker check \
  -token-ref file:/run/secrets/topo-servicenow-worker-token \
  -worker-pool site-a-local \
  -site site-a \
  -max-concurrency 4 \
  -allow-local
```

```sh
export SERVICENOW_INSTANCE_URL=https://instance.service-now.com

topo worker run \
  -token-ref file:/run/secrets/topo-servicenow-worker-token \
  -worker-pool site-a-local \
  -site site-a \
  -max-concurrency 4 \
  -allow-local
```

`-servicenow-instance` must be one absolute HTTPS origin with no userinfo,
path, query, or fragment. Redirects are refused. `-worker-pool`, `-site`,
`-allow-local`, `-poll-interval`, `-max-task-duration`, and
`-max-concurrency` are read-only local policy. There is intentionally no
state/spool/database/journal flag and no inbound listener.

`-allow-local` is explicit because even the one compiled-in operation requires
deployment authorization. ServiceNow can select `local.v1`, but it cannot
expand the worker's local authority or supply a target, command, script, query,
OID, URL, class, field, relationship, or executable payload.

For Password2 SSH discovery, create the target scope with partition prefix 32,
the protected credential as a `credential_admin`, a matching immutable
binding, and an `ssh_linux.v1` profile. On the laptop, prepare a canonical CIDR
allowlist and a normal OpenSSH `known_hosts` file as read-only deployment
configuration:

```text
# /etc/topo/ssh-allowlist
192.0.2.0/24
```

```sh
topo worker run \
  -servicenow-instance https://instance.service-now.com \
  -token-ref file:/run/secrets/topo-servicenow-worker-token \
  -worker-pool site-a-ssh \
  -site site-a \
  -allow-ssh-linux \
  -ssh-target-allowlist /etc/topo/ssh-allowlist \
  -ssh-known-hosts /etc/topo/ssh-known_hosts
```

The worker rejects missing/nonregular/oversized files, noncanonical or IPv6
allowlist entries, targets outside the local allowlist, target partitions that
are not exactly one IPv4 `/32`, and any SSH task without a binding. Port 22 and
the SSH operation/commands are compiled in; ServiceNow cannot change them.

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
An SSH observation with no assets and at least one bounded collection error is
recorded as `no_data`; IRE preflight/apply is skipped, the run retains its
collection-error summary, and the successful raw chunk follows normal expiry.

## Managed SSH scope

The managed SSH operation has one Password2-backed SSH credential per immutable binding and one
fixed `ssh_linux.v1` operation. It has no Vault/Kubernetes Secret/private-key
provider, ordered credential list, password spraying, user-selected command,
port, URL, shell, script, host-key bypass, IPv6/hostname target, credentialless
LAN sweep, other managed protocol, worker-side spool, offline guarantee, stock
Discovery integration, ECC record, MID behavior, native Discovery
Schedule/Status record, probe, pattern, or sensor. The older Relay and MID
artifacts remain intact and experimental.

## Validation records

Dated results are recorded in [worker evidence](evidence/servicenow-worker.md)
and [package validation](servicenow-validation.md).
