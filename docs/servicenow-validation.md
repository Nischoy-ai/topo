# ServiceNow package validation — maintainer evidence

This document records test scope and release evidence. Customer installation
steps are in [Install from XML](servicenow-update-set.md#customer-installation).

## Current acceptance status

| Check | Result |
| --- | --- |
| Native combined XML import and commit over existing app/indexes | Passed; 441 app updates plus 32 index updates |
| All 32 indexes created from an absent-index baseline | Passed using the unchanged native index companion |
| Required key uniqueness | All 17 duplicate attempts rejected; redundant composite explained below |
| Exact combined-file repeat | Passed native reimport, preview and batch commit; all 32 definitions retained |
| Fresh combined installation | Passed from absent scope/tables; version 0.4.6 and all 32 index definitions verified |
| Previous-version upgrade using combined XML | Awaiting a separate clean 0.4.5 baseline |
| Linux optional setup helper | Passed APT and RPM on amd64 and arm64, hosted disposable containers |
| Worker scale and retention | Passed 1K/10K/100K simulation and 100K retention drain |

Linux evidence: [run 37809561514](https://github.com/Nischoy-ai/topo/actions/runs/37809561514),
commit `bb7022f`. All four helper jobs verified the installed release bytes,
local discovery, dormant services, removal and operator-file preservation.
These are Linux container installation tests, not a booted-service test.
Simulation timings are not measurements of live ServiceNow throughput.

## Fresh combined installation (2026-10-08)

After an owner-confirmed reset, native lists showed no Topo scope, no Topo
application tables, and no retrieved update sets. The exact published combined
XML (SHA-256 `487c29cbd7b0837d4017ac2d32254302efd80cece949395f839ba8c42bfebdaf`)
was imported through the native XML form. Batch preview reported 441 app
inserts, zero updates/deletes/collisions, and the 32-index child Previewed in
Batch. Both sets subsequently reached Committed.

The installed application reports version 0.4.6. All 12 tables exist. Native
physical-index lists, with full forms for two truncated composite columns,
contain all 32 required ordered-column definitions (54 total physical indexes,
including platform/reference indexes). No SDK install, separate index import,
or manual index creation was used. This proves the combined package creates
the app and required index columns from an absent application baseline.
Uniqueness probes were not repeated in this run; the separate 17-key database
rejection evidence above remains distinct. Previous-version upgrade and data
preservation require a separate 0.4.5 baseline.

## Combined-file repeat (2026-10-08)

The exact published XML (SHA-256
`487c29cbd7b0837d4017ac2d32254302efd80cece949395f839ba8c42bfebdaf`)
was uploaded again through the native import form. Preview showed 441 base
updates, zero inserts/deletes/collisions and the index child Previewed in Batch.
Native batch commit completed with both sets Committed. Twelve physical-index
lists and full forms for the two truncated composite definitions verified all
32 required ordered-column definitions and 66 total indexes, unchanged in count.
No index deletion was performed. This proves repeat import/commit and retained
column coverage; it does not claim a new uniqueness-enforcement test or
customer-record content comparison on this repeat.

## Redundant composite uniqueness

`worker_pool.u_pool_id` is unique. Therefore two rows cannot share a
`(u_service_user, u_pool_id)` pair: they would also share the same pool ID,
which the stronger constraint rejects. The duplicate-key tests prove this
application invariant. Independently isolating the redundant composite index
would require removing the stronger constraint and adds no application
uniqueness guarantee. The package retains both source-defined indexes; physical
column coverage and native export inspection cover their definitions. No claim
is made that a duplicate probe uniquely identified the redundant physical index.

## Historical index evidence

The records below retain their original test scope; later results supersede
earlier outstanding checks as indicated in the current acceptance table.

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
It is a local candidate. Upload, preview and first commit passed on packaging-instance
with 32 records and zero collisions; ordered physical coverage remained 32/32.
An identical reupload and preview also passed. After explicit owner approval, the second commit also succeeded. All 66
physical index rows remained identical, preserving all 32 required definitions.
Import onto existing source-installed indexes cannot prove fresh physical index
creation; clean absent-index and repeat/upgrade evidence is still required.

On 2026-10-05, a bounded absent-index test passed on acceptance-instance. The owner
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
This proves recreation and enforcement for one unique definition. The later all-index test below supersedes this single-index test.

## All-index absent-state XML acceptance (2026-10-07)

The owner dropped all 32 required definitions through native Database Indexes
forms on XML-installed acceptance-instance. Complete lists filtered by each of the twelve
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
updates. Native import, preview and batch commit passed on packaging-instance over
existing app/indexes, with zero base collisions and both sets Committed.
All 32 ordered-column definitions were observed afterward across the twelve
physical-index lists (66 total indexes); truncated composites were opened in
full forms. This does not re-prove uniqueness or fresh creation. Fresh combined
installation, repeated exact batch and subsequent-version upgrade remain open.
Preview 1 omits indexes; preview 2 contains all 32 definitions.

## Maintainer capture and recovery

### Administrator procedure

1. Use a non-production acceptance instance first. Stop workers and deactivate
   Topo schedules. Preserve existing configuration/history and an appropriate
   recovery point before changing a populated installation.
2. For export evidence, first select a dedicated clean Global update set.
   The native creator on the tested Australia instance captures a `sys_index`
   record after successful creation. Never export the Global Default set: it
   can contain unrelated configuration and authentication records. A captured
   record still requires an export/import test before claiming XML delivery.
3. Open **System Definition → Tables & Columns**, select the exact table
   below, and use **Index creator**. This is the path exercised on acceptance-instance.
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

