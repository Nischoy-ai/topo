# ServiceNow index setup for Topo 0.4.6

Status (2026-10-07): the local native companion XML recreated all 32 required
indexes from a verified absent-index baseline on dev317694. Post-import tests
accepted 17 baseline keys and rejected all 17 duplicate keys; the redundant
service-user/pool-ID constraint has the isolation limit described below.
The standalone companion remains unpublished; preview 2 includes its index
updates in the combined batch. Public preview 1 still omits these indexes.

## Install the combined XML

Development preview 2 contains one native batch export,
`nischoy-topo-0.4.6-combined.xml`. Follow the
[installation guide](servicenow-update-set.md#customer-installation): upload once,
preview and commit the batch from the Nischoy Topo base set. Confirm the app
and index child both reach Committed. Do not separately upload the companion.

The child is a Global update set named **Nischoy Topo 0.4.6 Index Repair**,
containing only 32 `sys_index` updates for the twelve Topo tables (17 unique,
15 non-unique). Global scope here does not mean it contains general instance
configuration. The separate companion remains a historical test artifact;
the absent-index proof below used that companion, not the new combined file.

Use the package's matching manifest and checksums. Review every preview problem
before committing. Stop for unexpected tables, missing dependencies, duplicate
data or unrelated changes. Never weaken uniqueness or delete customer records
to force installation. The 32 newer-local-update conflicts in our absent-index
test resulted from deliberately dropping the indexes; accepting those known
test deletions is not a blanket customer collision-resolution procedure.

Customers **do not drop existing indexes** before installing. Keep workers
stopped and schedules inactive until the batch commit succeeds and the administrator
checks all definitions below, including uniqueness. Physical names can change
on import; compare table and ordered columns. The Database Indexes list's
**Is Droppable** flag does not indicate uniqueness.

On the tested Australia instance, open a table's **Database Indexes** related
list. For an exact browser inventory, use `v_index_creator_list.do` with
`sysparm_query=table%3D<TABLE_SYS_ID>`, using that table's actual `sys_db_object`
ID. A name-prefix or `logical_table_name` query returning no rows is not proof
of absence. Use the full index form when a column list is truncated.

The native index creator below is a maintainer capture/recovery procedure;
normal installation uses the companion XML and does not require 32 manual
Create Index clicks. No private index API is used.

## Administrator procedure

1. Use a non-production acceptance instance first. Stop workers and deactivate
   Topo schedules. Preserve existing configuration/history and an appropriate
   recovery point before changing a populated installation.
2. For export evidence, first select a dedicated clean Global update set.
   The native creator on the tested Australia instance captures a `sys_index`
   record after successful creation. Never export the Global Default set: it
   can contain unrelated configuration and authentication records. A captured
   record still requires an export/import test before claiming XML delivery.
3. Open **System Definition → Tables & Columns**, select the exact table
   below, and use **Index creator**. This is the path exercised on dev317694.
4. Select the specified columns in exactly the listed order. Select **Unique
   Index** only where the checklist says **Yes**, and use **btree**.
5. Create the index and wait for completion before continuing. If creation
   reports duplicates or any other error, stop and investigate; do not delete
   customer records or substitute a non-unique index to make installation pass.
6. Record completion for each definition. Existing physical indexes may have
   different generated names: compare table, ordered columns and uniqueness.
   Do not create an extra copy merely because the generated name differs.
7. Verify all 32 column definitions, then separately verify all 17 uniqueness
   contracts. A column-coverage report alone cannot prove uniqueness.

The procedure uses ServiceNow's documented
[table index creator](https://www.servicenow.com/docs/r/platform-administration/table-administration-and-data-management/t_CreateCustomIndex.html).
No background-script index API or automatic XML fix script has been validated.

## Required definitions

Every table name below has the prefix `x_664635_topo_`. The definition name is
from the source; ServiceNow may assign a different physical index name.
The checklist was derived from a clean SDK 4.9.0 build of the unchanged 0.4.6
`tables.now.ts` declarations. Keep it synchronized with the build's
`servicenow-index-requirements` artifact when the schema changes.

| Table suffix | Source definition | Columns in order | Unique |
| --- | --- | --- | --- |
| `credential_access` | `credential_access_event_unique` | `u_event_id` | Yes |
| `credential_access` | `credential_access_task_time` | `u_task`, `u_accessed_at` | No |
| `credential_binding` | `credential_binding_profile_scope` | `u_profile_id`, `u_profile_revision`, `u_target_scope`, `u_active` | No |
| `credential_binding` | `credential_binding_revision_unique` | `u_binding_id`, `u_revision` | Yes |
| `ire_delivery` | `ire_key_unique` | `u_idempotency_key` | Yes |
| `ire_delivery` | `ire_run_state` | `u_run`, `u_state` | No |
| `ire_delivery` | `ire_task_attempt_unique` | `u_task`, `u_attempt_id` | Yes |
| `profile` | `profile_pool_active` | `u_worker_pool`, `u_active` | No |
| `profile` | `profile_revision_unique` | `u_profile_id`, `u_revision` | Yes |
| `result` | `result_chunk_unique` | `u_task`, `u_attempt_id`, `u_chunk_number` | Yes |
| `result` | `result_retention` | `u_processing_state`, `u_delete_after` | No |
| `run` | `run_id_unique` | `u_run_id` | Yes |
| `run` | `run_profile_state` | `u_profile`, `u_state` | No |
| `run` | `run_schedule_state` | `u_schedule`, `u_state` | No |
| `schedule` | `schedule_due` | `u_active`, `u_next_run` | No |
| `schedule` | `schedule_id_unique` | `u_schedule_id` | Yes |
| `ssh_credential` | `ssh_credential_id_unique` | `u_credential_id` | Yes |
| `target_scope` | `target_scope_pool_active` | `u_worker_pool`, `u_active` | No |
| `target_scope` | `target_scope_revision_unique` | `u_scope_id`, `u_revision` | Yes |
| `task` | `task_cancel` | `u_cancel_requested`, `u_state` | No |
| `task` | `task_claim` | `u_worker_pool`, `u_state`, `u_partition_ordinal`, `sys_created_on` | No |
| `task` | `task_id_unique` | `u_task_id` | Yes |
| `task` | `task_lease_expiry` | `u_state`, `u_lease_expires` | No |
| `task` | `task_pool_lease_slot_unique` | `u_pool_lease_slot` | Yes |
| `task` | `task_run_partition` | `u_run`, `u_partition_key` | No |
| `task` | `task_run_state` | `u_run`, `u_state` | No |
| `task` | `task_worker_lease_slot_unique` | `u_worker_lease_slot` | Yes |
| `worker` | `worker_boot_unique` | `u_pool`, `u_boot_id` | Yes |
| `worker` | `worker_id_unique` | `u_worker_id` | Yes |
| `worker` | `worker_pool_active` | `u_pool`, `u_active`, `u_last_heartbeat` | No |
| `worker_pool` | `pool_id_unique` | `u_pool_id` | Yes |
| `worker_pool` | `pool_user_unique` | `u_service_user`, `u_pool_id` | Yes |

## Acceptance evidence required

On the disposable test instance, use isolated, explicitly identified fixtures.
Never use real customer credentials or discovery targets for these checks.

- Verify every required ordered-column definition exists after installation.
- For each unique definition, prove one valid insert succeeds and a second
  record with the same complete key is rejected by the database. Also prove
  distinct keys are accepted. For composite keys, varying one component must
  allow another record. Clean up only the dedicated test fixtures.
- For pool/worker lease slots, verify released/empty slots permit the multiple
  idle tasks the application expects, while occupied duplicate slots fail.
- Race worker claims against the same pool and task fixtures. Verify a task
  has only one successful claim and configured pool/worker limits are respected.
- Retry result chunks and completion requests. Verify the same logical result
  is retained once and the documented retry response is returned.
- Recheck after repeating the setup and after an XML upgrade. Existing indexes
  and operational records must remain intact; do not source-install over the
  XML acceptance fixture to make the test pass.

Repairing an existing test instance does not prove a self-contained customer
package. Fresh XML plus this setup, repeated setup, upgrades and a practical
customer installation check still need separate evidence.

## Repair evidence (2026-10-04)

All 17 baseline key inserts succeeded and duplicate keys were rejected;
distinct-key variants succeeded except the redundant service-user/pool-ID
combination, where pool ID alone already enforces uniqueness. This does not
independently isolate the redundant composite index. Multiple empty lease slots
were accepted. Tests used minimal database fixtures with workflow disabled;
all 43 inserted rows were cleaned up, with no real SSH secrets involved.

Live restricted-worker API tests used four tasks and four workers in an isolated
pool: 8 concurrent claims by one worker had one winner; 12 further claims from
three workers had one winner under the pool limit of 2. Chunk upload/retry
returned 201/200 with the duplicate flag, failed/cancelled completion was
idempotent, and a released lease slot was reused. One result row remained before
cleanup. Test rows and the attachment were removed; IRE was not invoked.

A native companion XML export now contains exactly 32 `sys_index` updates,
matching the required ordered columns and uniqueness flags. Its SHA-256 is
`83de78f055477320c136138ce05782c9dff4d4e79c8501b701ffab74ba260b82`.
It is a local candidate. Upload, preview and first commit passed on dev394887
with 32 records and zero collisions; ordered physical coverage remained 32/32.
An identical reupload and preview also passed. After explicit owner approval, the second commit also succeeded. All 66
physical index rows remained identical, preserving all 32 required definitions.
Import onto existing source-installed indexes cannot prove fresh physical index
creation; clean absent-index and repeat/upgrade evidence is still required.

On 2026-10-05, a bounded absent-index test passed on dev317694. The owner
dropped task `(u_cancel_requested,u_state)` through the supported Database
Indexes form. The native success message and 11-row physical list confirmed
absence. The unchanged companion XML preview reported one newer-local-update
collision for this deliberate deletion. Accepting its remote definition and
committing succeeded in four seconds. The physical list returned to 12 rows,
including the exact two-column index. This proves XML recreation of this one
non-unique index; it does not establish all-index or unique-index fresh creation.

On 2026-10-07, the corresponding Task ID unique-index test passed. Native Drop
and the 11-row physical list established that `u_task_id` was absent. Reimport
of the unchanged XML reported the expected local-deletion conflict only;
accepting the remote definition and committing succeeded in one second.
The physical list returned to 12 indexes. A scoped, isolated database test
accepted a baseline Task ID, rejected its duplicate with an explicit database
unique-key violation for `u_task_id`, and accepted a distinct Task ID. Both
inserted rows were removed with zero cleanup failures. The test used minimal
cancelled task records with workflow disabled, not a discovery execution.
This proves recreation and enforcement for one unique definition. Fresh
creation of all 32 definitions together remains unverified.

## All-index absent-state XML acceptance (2026-10-07)

The owner dropped all 32 required definitions through native Database Indexes
forms on XML-installed dev317694. Complete lists filtered by each of the twelve
exact table IDs verified that none remained. There were 22 retained physical
primary/reference indexes. No application records were deleted for this reset.

The unchanged companion (SHA-256 above) was uploaded, previewed and committed.
Preview reported exactly 32 newer-local-update conflicts corresponding to the
deliberate drops. Those remote definitions were accepted; commit reported
**Succeeded in 5 Seconds**. Complete post-commit lists and full forms for two
truncated composite keys verified all 32 ordered definitions, 54 physical
indexes total, and preservation of the 22 retained definitions. Several physical
names changed (for example `u_task_2` became `u_task`); names are not the contract.

A new scoped, workflow-disabled fixture test accepted all 17 baseline keys,
rejected every duplicate and emitted explicit database unique-key violations.
Distinct-key variants succeeded except changing only the service user while
retaining the same pool ID: the stronger pool-ID constraint correctly rejected
that variant. This does not independently isolate the redundant composite
constraint. Multiple tasks with empty lease slots succeeded. All 43 inserted
rows were removed with zero cleanup failures. Execution history:
`48a97121c3bf8790b49fbefdd4013140`. No discovery or IRE write was invoked.

This establishes creation of all required indexes together from absence on an
existing XML-installed Australia instance, plus the described key enforcement.
It is not a fresh full-instance installation or a new app-version upgrade with
the companion present. Prior repeated-import and race/retry findings remain
separate evidence; they were not rerun during this test. Broader package gates
and production readiness remain open. Public preview 1 has not changed.

## Combined native batch (2026-10-07)

The unchanged native combined export contains 441 app updates and 32 index
updates. Native import, preview and batch commit passed on dev394887 over
existing app/indexes, with zero base collisions and both sets Committed.
All 32 ordered-column definitions were observed afterward across the twelve
physical-index lists (66 total indexes); truncated composites were opened in
full forms. This does not re-prove uniqueness or fresh creation. Fresh combined
installation, repeated exact batch and subsequent-version upgrade remain open.
The public preview still omits indexes.
