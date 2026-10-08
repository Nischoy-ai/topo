# Managed worker validation evidence

Historical evidence captured from source commit
`5439ef5b441e1145172190db94aa24ba4d809599`. Versions, dates and test
scopes below identify the tested artifacts; they are not installation steps.

## Verification

For the current published XML and worker package, see
[package validation](../servicenow-validation.md) and
[distribution evidence](distribution.md#first-beta-operational-evidence).
The dated records below describe their specific source versions, fixtures and
security checks; they are evidence records rather than customer installation
steps. Use the [setup guide](../pilot-quickstart.md) to install and configure a worker.

Focused local gates are:

```sh
(
  cd integrations/servicenow/topo-control-plane
  npm ci --ignore-scripts
  npm test
  npm run build
)
for file in integrations/servicenow/topo-control-plane/scripts/*.js; do
  node --check "$file"
done
env GOTOOLCHAIN=go1.26.8 go test -race ./internal/worker/... ./cmd/topo
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

Slice B adds deterministic evidence for:

- canonical IPv4/IPv6 Go partition plans with stable SHA-256 keys, exclusions,
  non-overlap, ordinals/counts, and a 100,000-partition ceiling; the current
  Fluent control-panel compiler intentionally accepts IPv4 only because no
  target-bearing production operation exists yet;
- pool backpressure of five live leases across two workers capped locally at
  four, plus an eight-partition run drained after the first worker disappeared
  with four leases and a fresh worker completed four attempt-two recoveries;
- successful renewal of a 350 ms operation beyond an 80 ms initial lease,
  cancellation at expiry when renewals fail, and recovery by a fresh boot;
- ready and active multi-partition cancellation, including rejection of late
  result and success calls and a terminal cancellation acknowledgement;
- identical 1K, 10K, and 100K simulated estates across 1, 10, and 100
  partitions. On the 2026-08-30 development run they completed and repeated in
  approximately 1.026 s, 1.083 s, and 1.802 s. The 100K case retained exactly
  100,000 supported computer/adapter items and 50,000 ownership relationships;
  every repeat item and relationship operation was simulated `NO_CHANGE`; and
- 100,000 eligible successful raw results drained in batches of at most 257,
  leaving bounded tombstones with zero raw payload bytes. This measures the
  algorithm and test process, not ServiceNow attachment throughput or an SLA.

### Slice C1 simulator and source evidence — 2026-08-30

This evidence is local and deterministic; it is not evidence about ServiceNow
Password2 encryption, scoped ACL enforcement, the real Scripted REST runtime,
IRE, or a real SSH server:

- Fluent `0.4.3` builds with SDK 4.9.0 and defines twelve scoped tables, five
  roles, seven fixed authenticated worker routes, and no worker table ACL.
- Node contract tests accept only `local.v1`/`ssh_linux.v1`, validate the
  bounded username and no-store broker source, accept an SSH no-assets result
  only with a collection error, and reject a plugin mismatch.
- Go tests prove canonical read-only IPv4 allowlist loading, bounded
  `known_hosts`, policy-digest sensitivity, exact `/32` task validation,
  allowlist rejection before credential retrieval or dialing, fixed port 22,
  and secret-redacted provider failures.
- The end-to-end controlsim run registers an SSH-only worker, claims one `/32`
  task, obtains exactly one live-attempt credential, returns a bounded
  unreachable-target observation, records one secret-free allowed access,
  skips simulated IRE, and retains a terminal run summary with one collection
  error.
- A separate manual-plus-scheduled simulator pair retrieves its attempt-bound
  credential for each run, produces the same computer/adapter/ownership source
  identities, and reports only simulated `NO_CHANGE` on the repeat. A mixed
  local/SSH worker also completes a local IRE fixture while an unreachable SSH
  task terminates independently as `no_data`.
- Formatting, diff checks, exact Go 1.25.13 full and focused tests, vet, full
  race tests, native and Windows amd64 build/vet, clean Fluent
  install/test/build, and the pinned security-review gate pass;
  `govulncheck` reports zero reachable vulnerabilities.

Real source-driven upgrade preservation, installed metadata, Password2 broker
and ACL evidence, and sanitized Docker SSH/IRE/retention evidence are documented
below. The staged Slice C1 acceptance matrix passed, and the implementation
is merged.

### Real ServiceNow Slice C1 installation evidence — 2026-08-30

This evidence was collected on a real developer instance, separately from `controlsim`.
It proves installation metadata and upgrade preservation only; it does not
prove Password2 runtime behavior or SSH discovery:

- `now-sdk install --auth topo-dev` upgraded the existing application sys_id
  `d4e2151fdcbc7d97f8c155d1ba873e46` to `0.4.0` from the Fluent source and
  produced rollback context `b03a95d1938fc790ec251aebb9373cc7`.
- Read-only SDK queries found exactly twelve `x_664635_topo_*` tables, including
  the protected SSH credential, immutable credential binding, and secret-free
  credential-access log; exactly five application roles; and exactly seven
  authenticated POST worker routes, including `/{id}/credential`.
- The installed Password2 dictionary row
  `3d3ad9d193030b90ec251aebb9373c0d` is mandatory, has field auditing disabled,
  and carries `is_legacy_password2=true,no_data_replicate=true`. The ten
  credential-related ACLs are active: credential CRUD is credential-admin
  only; binding read is viewer while writes are credential-admin only; access
  log read is credential-admin and delete is app-admin. No worker table ACL was
  added.
- Pool `12289acd93478790ec251aebb9373ceb`, local profile
  `ae289acd93478790ec251aebb9373cf0`, disabled proof schedule
  `2a28dacd93478790ec251aebb9373c0f`, and the three known complete Slice A
  22-item/21-relationship run summaries remained present after the upgrade.
- The existing native `topo.worker.execute` auth scope and API-access policy
  each remain exact-resource allowlists for the original six Slice A POST
  routes. They do not yet include `POST /{id}/credential`; no wildcard or
  generic Table API grant was introduced. A fresh worker token must not be used
  for Password2 discovery until the exact seventh scope/policy pair is added
  and verified.
- Both credential and binding tables are empty. No secret, target, broker call,
  SSH connection, IRE transaction, or CMDB write was attempted as part of this
  installation check.

The next section supplements this installation-only evidence with the exact
seventh OAuth route, a disposable Password2 record, and a focused broker run.

### Real ServiceNow Slice C1 Password2 broker evidence — 2026-08-31

This evidence was collected on a real developer instance, separately from `controlsim`.
It deliberately used a documentation-only TEST-NET address and did not execute
SSH, submit discovery data, invoke IRE, or write CMDB:

- Active REST API auth scope `4eee652593c38b90ec251aebb9373c8c`
  and API-access policy `322fe92d93078b90ec251aebb9373cf0` grant the existing
  `Topo Worker OAuth` inbound profile only POST, version 1,
  `/x_664635_topo/v1/tasks/{id}/credential`. Resource, method, version, and
  global wildcards are all disabled; no Table, CMDB, IRE, admin, or other
  route permission was added.
- Disposable credential `topo-slice-c1-disposable` was entered directly into
  the installed Password2 form. Reopening the record rendered no secret value,
  the password field produced no matching `sys_audit` row, and a fresh
  `topo.worker.execute` token still received HTTP 401 from the generic
  `x_664635_topo_ssh_credential` Table API.
- Manual Run Now profile `topo-slice-c1-ssh-test` binds that credential to the
  immutable scope `topo-slice-c1-testnet`, whose single canonical partition is
  `192.0.2.10/32`. Run `14a135a193478b90ec251aebb9373cb6` created exactly one
  fixed `ssh_linux.v1` task, `90a135a193478b90ec251aebb9373cb7`.
- A fresh worker identity registered with only `ssh_linux.v1`, claimed the
  task, and called the broker with the exact worker, boot, task, attempt, and
  lease. The broker returned HTTP 200, the expected username and a non-empty
  password, `Cache-Control: no-store`, and `Pragma: no-cache`; neither the
  response value nor OAuth/lease tokens were printed or retained.
- Repeating the request with a modified attempt ID returned HTTP 403
  `task lease is not owned by this worker attempt`. ServiceNow retained one
  secret-free `Allowed / attempt_bound` access row and one secret-free
  `Denied / lease_not_owned` row. The exact attempt was then completed with a
  bounded structured evidence-only failure, leaving both task and run failed
  and no live lease.

This proves focused Password2 resolution and attempt binding, not SSH or IRE.
The next sections supply the separately labelled SSH/IRE and complete security-
matrix evidence.

### Real ServiceNow Slice C1 Docker SSH/IRE evidence — 2026-08-31

This evidence was collected on a real developer instance, separately from `controlsim`.
The target was a disposable Debian 12 Docker container bound only to laptop
loopback `127.0.0.1:22`; its source, password file, allowlist, and pinned public
host keys lived under `/private/tmp`, outside the repository. No credential,
OAuth token, or lease token was printed or retained as evidence:

- Credential `topo-slice-c1-docker`, immutable scope
  `topo-slice-c1-docker-loopback` (`127.0.0.1/32`), binding
  `topo-slice-c1-docker-binding`, and profile
  `topo-slice-c1-docker-ssh` were created through the installed control-panel
  forms. The worker independently allowed only `127.0.0.1/32`, pinned the
  container's runtime host keys, enabled only `ssh_linux.v1`, and exposed no
  listener or durable state.
- The direct destination-neutral observation contained one synthetic Linux
  host plus eleven interfaces and eleven `host_has_interface` relations. The
  one collection error was the expected bounded `ssh_partial` result for the
  container's absent `systemctl`; it did not prevent supported inventory.
- Manual run `12a0af2993c3cb90ec251aebb9373c79` completed in one attempt with
  12 items, 11 relationships, and one collection error. IRE preflight and
  apply were clean; the first delivery recorded 23 `INSERT` operations.
- Identical manual run `ae11a3ad93c3cb90ec251aebb9373ced` completed with the
  same 12/11 identities. Its clean repeat apply recorded 12 `UPDATE` item
  operations and 11 relationship `NO_CHANGE` operations, rather than new
  relationships or source identities.
- Disabled proof schedule `topo-slice-c1-docker-schedule` produced scheduled
  run `54c163219307cb90ec251aebb9373cbb`, which completed in one attempt with
  the same 12/11 summary. The schedule advanced its next-run time and was then
  disabled.
- The first successful raw chunk was initially `Processed` with its normal
  24-hour deletion deadline. That one disposable deadline was backdated and
  the Fluent-installed maintenance job executed once. Result row
  `aaa06f2d93c3cb90ec251aebb9373c29` and attachment
  `e2a06f2d93c3cb90ec251aebb9373c2a` disappeared; the completed 12/11 run and
  applied IRE-delivery record remained.
- The first real claim also exposed that ServiceNow serializes target
  partition ordinal/count values as integral decimals (`0.0`/`1.0`), just as
  it had done for profile revision. The strict Go decoder now accepts only
  mathematically integral, in-range forms for all three fields and rejects a
  fractional value; focused worker/control-simulator tests pass.

This completes the real sanitized-target, manual/scheduled, repeat-IRE, and
retention gates. It is not throughput evidence or a production network scan.

### Real ServiceNow Slice C1 security acceptance matrix — 2026-09-01

This evidence was collected on a real developer instance, separately from `controlsim`.
The test harness lived under `/private/tmp`, outside the repository; it read
owner-only OAuth material without printing it, retained credentials only in
process memory, and emitted status/shape assertions rather than secret values.
It did not dial a target, submit a result, invoke IRE, or write CMDB:

- Disposable users proved the installed ACL boundary by impersonation.
  Credential administrators could create credentials and bindings and read,
  but not create, access events. Operators and viewers could read bindings but
  not credentials or access events. Workers could read none of the credential,
  binding, access-event, or task records; users with no application role could
  read none of the three credential-related tables. A worker OAuth token
  continued to receive HTTP 401 from the generic
  credential Table API.
- The exact live attempt and an idempotent repeat both returned HTTP 200 with a
  non-empty bounded credential shape plus `Cache-Control: no-store` and
  `Pragma: no-cache`. Wrong pool, worker, boot, task, attempt, and lease-token
  identities returned HTTP 403. Wrong operation, profile, scope, binding, and a
  credential belonging to another binding returned HTTP 409.
- Inactive binding and inactive Password2 credential records returned HTTP
  409. Restoring those records made the exact request succeed again, proving
  that the status-only form mutation did not replace the protected value.
  A cancelled task, expired lease, and terminal attempt each returned HTTP
  409. Every success and denial retained the no-store/no-cache headers.
- The expired task was safely reclaimed as attempt two and terminalized. The
  final task retained `attempt_count=2` and released both pool and worker
  capacity slots. The cancellation denial and lease expiry checks exposed two
  real defects: the broker had not rejected `u_cancel_requested`, and
  `updateMultiple` with empty strings had left stale unique lease slots on
  expiry. Fluent `0.4.3` now denies cancelled tasks before credential
  resolution and atomically clears every lease identity/capacity field with
  scoped-compatible null values. The same matrix passed after installation.
- Credential-access rows recorded only worker, attempt, task/binding where
  appropriate, outcome, and reason. Success used `allowed/attempt_bound`;
  identity and terminal denials used `denied/lease_not_owned`; cancellation
  used `denied/task_cancelled`. No password, bearer token, or lease token was
  present in the inspected fields.
- The binding, credential active state, profile/task fixture, and 30-second
  pool lease policy were restored after the probes. Disposable ACL users and
  evidence records remain only in the throwaway developer instance. One
  pre-fix cancelled task retains explicit retired slot markers documenting the
  stale-slot defect; the post-fix retry record released its slots to null. No
  production configuration was changed.

Together with the separately recorded Docker SSH/IRE/retention run, this
completes every staged Slice C1 acceptance gate. External Vault bindings remain
the deliberate Slice C2 follow-up and are not implied by this evidence.

### Real ServiceNow C1.1 onboarding preflight evidence — 2026-09-02

This evidence was collected on a real developer instance, separately from `controlsim`.
It proves source upgrade and the non-claiming preflight only; it is not a new
discovery, IRE, CMDB, package-channel, or consumer ZIP-install proof:

- `scripts/install-servicenow-app.sh topo-dev` used the SDK's preconfigured
  OAuth alias to install Fluent `0.4.4` over the same application sys_id
  `d4e2151fdcbc7d97f8c155d1ba873e46`. The install created rollback context
  `1c6c299e930f03d0ec251aebb9373ca0` and preserved active pool
  `pdi-local-a` (`12289acd93478790ec251aebb9373ceb`). No credential value was
  accepted by the helper or printed.
- A fresh short-lived token with exact scope `topo.worker.execute` ran
  `topo worker check` with only `local.v1`. ServiceNow returned worker ID
  `aaad6112934f03d0ec251aebb9373cda` and boot ID
  `f7777c6aae5be6b0acc0c5fdfd0972b8`. A read-only SDK query found the matching
  active worker row, the preserved pool reference, `u_current_leases=0`, and
  the stored heartbeat at `2026-09-03 00:06:08` UTC.
- The same query found no task whose lease worker was that registration.
  This corroborates the code-level call audit: `check` registers once,
  heartbeats once, and never calls claim, renewal, credential, result, or
  completion.
- The first attempt exposed a local onboarding defect before any HTTP request:
  newline-terminated token files were rejected. The worker now trims
  surrounding whitespace only at the OAuth token boundary; regression tests
  cover the conventional owner-only token-file form without changing
  byte-exact password or private-key handling.

The release app normalizer also produced an identical validated ZIP from two
independent SDK builds after canonicalizing only SDK-generated BOM UUID/time
and ZIP metadata. The pinned SDK is a build-only development dependency and
the shipped app has no npm runtime dependency tree. Its 4.9.0 development tree
recorded audit reports nine moderate and two high transitive npm advisories; that
build-tool exposure remains explicit and is not described as a clean audit.
The required Go vulnerability gate separately found reachable SSH deadlocks
`GO-2026-6354`/`GO-2026-6355`; the remediation uses exact Go
1.26.8 and `x/crypto` 0.56.0. The full pinned gate then reports zero reachable
vulnerabilities and passes native plus Windows amd64 tests/builds.

### Real ServiceNow Slice B evidence — 2026-08-30

This evidence was collected on a real developer instance, separately from `controlsim`:

- `now-sdk install --auth topo-dev` upgraded the same application sys_id
  `d4e2151fdcbc7d97f8c155d1ba873e46` to `0.3.0` from the Fluent source and
  produced rollback context `56fc68d1938bc790ec251aebb9373c20`. Read-only SDK
  queries found exactly the nine `x_664635_topo_*` tables, including target
  scope `22fc68d1938bc790ec251aebb9373ca0`, all seven new task partition/cancel/
  capacity fields, worker `u_max_leases`, run `u_cancelled_tasks`, four target-
  scope ACLs, the active immutable-target-scope rule, and **Cancel run** action.
- The upgrade preserved pool `12289acd93478790ec251aebb9373ceb`, active profile
  `ae289acd93478790ec251aebb9373cf0`, disabled proof schedule
  `2a28dacd93478790ec251aebb9373c0f`, and the three known Slice A 22-item/
  21-relationship runs. Their run IDs, terminal states, task counts, assets,
  and relationships were unchanged; the additive cancellation count is zero.
- A fresh short-lived worker token registered a max-concurrency-4 worker and
  heartbeated successfully against the preserved `pdi-local-a`/`pdi-local`
  binding. The same token still received HTTP 401 from an unrelated scoped
  Table API. Token/client-secret values remained in owner-only files or
  process memory and were never printed.
- Admin-only evidence fixture target scope
  `535078d9938bc790ec251aebb9373c3e` canonicalized overlapping input to
  `192.0.2.0/24`, retained exclusion `192.0.2.128/26`, compiled three `/26`
  partitions, and stored plan digest
  `fefb36fc898b70986d22d956b858813ee9b2fb4605800761f907bda68822cffd`.
  The scope is inactive and was never attached to a production `local.v1`
  profile.
- Isolated inactive pool `c35074d9938bc790ec251aebb9373c01`
  had exactly two lease slots. Eight concurrent real worker-API claimants over
  four admin-seeded ready tasks produced exactly two winners, two distinct pool
  slots, two distinct worker slots, and two attempt-one tasks. After 1.1
  seconds, renewal extended one 30-second lease. Setting its cancellation flag
  caused the next renew to return `cancelled:true`; a late result and late
  successful completion each returned HTTP 409, while structured cancellation
  completed with HTTP 200. The other live attempt completed as a structured
  fixture failure.
- Run `1f5030d9938bc790ec251aebb9373c0b` now retains three cancelled tasks, one
  failed task, two total attempts, and no occupied pool or worker slot. Its pool
  and profile are inactive. The fixture used admin-only Table API writes solely
  to create multi-task scale state that production `local.v1` cannot create;
  all claims, renewal, cancellation observation, late-call denial, and terminal
  reports used the six worker resources. It performed no result acceptance,
  IRE call, or CMDB write and is not evidence for **Run now** construction.
- An earlier inactive fixture is excluded from claim evidence because ISO-8601
  values seeded through the Table API were truncated to midnight. The app
  correctly failed those already-expired tasks before claim. The successful
  fixture used ServiceNow UTC date-time form (`YYYY-MM-DD HH:mm:ss`).

Still not real Slice B evidence: 1K/10K/100K platform throughput, a 100K raw-
attachment backlog, a long-running production discovery operation, or a
target-bearing discovery operation. Those remain simulator/future-protocol
gates and are not inferred from the focused fixture.

### Real ServiceNow Slice A evidence — 2026-08-30

This evidence was collected on a real Australia-release developer instance,
separately from `controlsim`:

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

It does not prove Slice B behavior by itself; the separately labelled evidence
above does. Neither real section proves Slice B's simulator-only scale and
retention-volume gates.

