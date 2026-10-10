# ServiceNow package validation

This document records test scope and release evidence for the published package. Customer installation
steps are in [Install from XML](servicenow-update-set.md#customer-installation).

## Current acceptance status

This table applies to the published 473-update XML identified below. The
revised installation-metadata candidate has separate evidence at the end of
this document.

| Check | Result |
| --- | --- |
| Native combined XML import and commit over existing app/indexes | Passed; 441 app updates plus 32 index updates |
| All 32 indexes created from an absent-index baseline | Passed using the unchanged native index companion |
| Required key uniqueness | All 17 duplicate attempts rejected; redundant composite explained below |
| Exact combined-file repeat | Passed native reimport, preview and batch commit; all 32 definitions retained |
| Fresh combined installation | Passed from absent scope/tables; version 0.4.6 and all 32 index definitions verified |
| Previous-version upgrade using combined XML | Passed 0.4.5 → 0.4.6; three configuration IDs and 21 field values preserved, all 32 index definitions verified |
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
rejection evidence above remains distinct. The previous-version upgrade used
the separate 0.4.5 baseline described below.

## Combined-file upgrade (2026-10-08)

An independent reset baseline had no Topo scope, tables or retrieved update
sets. The saved native 0.4.5 XML (SHA-256
`dad292dc3c7395c6d8d27e7da068a1318fd854776aca8f2f9577280c5e6f7981`)
was imported, previewed and committed: 441 inserts, zero updates/deletes or
collisions. Version 0.4.5 and all 12 tables were verified before creating an
isolated inactive pool, local profile and future schedule.

The exact published combined 0.4.6 XML (SHA-256
`487c29cbd7b0837d4017ac2d32254302efd80cece949395f839ba8c42bfebdaf`)
then passed native batch preview and commit. Preview reported 441 app updates,
zero inserts/deletes/collisions, and the index child Previewed in Batch. Both
sets reached Committed. The installed version is 0.4.6, all 12 tables remain,
and native lists plus full forms for the two truncated composite definitions
verify all 32 required ordered-column definitions (54 total physical indexes).
No SDK install, separate companion import or manual index creation was used.

Before/after snapshots matched all three configuration record IDs and all 21
selected field values exactly, including references, inactive flags and the
future schedule time. Only those three temporary records were then removed;
bounded queries confirmed none remained. This establishes preservation of the
tested configuration fields. Protected credentials and operational history
were outside this fixture comparison. The earlier key-enforcement tests retain
their separate scope; duplicate probes were not repeated for this upgrade.

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

## Database uniqueness and worker behavior

The native companion XML created all 32 indexes from a verified absent-index
baseline. All 17 baseline key inserts succeeded and duplicate keys were rejected
by the database. Distinct-key variants and multiple empty lease slots were
accepted, subject to the redundant pool constraint described above. All 43
inserted fixture rows were removed. These tests used minimal database fixtures
with workflow disabled and no real SSH secrets.

Restricted-worker API tests used four tasks and four workers in an isolated
pool. Eight concurrent claims by one worker had one winner; twelve additional
claims from three workers had one winner under a pool limit of two. Chunk
upload/retry returned 201/200 with the duplicate flag. Failed/cancelled completion
was idempotent and a released lease slot was reused. Fixture rows and the test
attachment were removed; IRE was not invoked in this concurrency test.

## Previous application-only upgrade

The application-only 0.4.5 → 0.4.6 XML upgrade passed on Australia Patch 3.
Preview reported 441 updates, zero inserts/deletes and no problems. Version,
tables, roles, ACLs, routes and the mapper script were verified after commit.
An inactive pool, local profile and future schedule preserved their three
record IDs and all 23 captured field values. This covers those configuration
records; Password2 credentials and run-history preservation were not exercised.
The combined-file upgrade above is a separate acceptance run.

## Real Linux workflow

The XML-installed 0.4.6 app and published v0.1.0-beta.1 worker completed two
manual scans and one automatically scheduled scan of an approved Linux target.
Each run completed one task in one attempt with three assets, two relationships
and zero collection errors. Broker authorization and IRE preflight/apply passed.

| Execution | IRE operations | CMDB result |
| --- | --- | --- |
| First manual | Five INSERT | One computer, two adapters, two ownership relationships |
| Manual repeat | Three UPDATE, two NO_CHANGE | Same three CI IDs and two relationship IDs |
| Automatic schedule | Three UPDATE, two NO_CHANGE | Same three CI IDs and two relationship IDs |

Read-only snapshots verified stable mapped fields and identities. The schedule
was disabled and the temporary worker stopped after testing. The restricted
OAuth identity reached all seven custom routes and received 401 from an unrelated
Table API. These are focused functional/OAuth checks, separate from the complete
role and protected-value security matrix.

## Verification scope

Compare physical indexes by table and ordered columns; platform-generated names
and reference indexes may differ between installations. Physical column coverage
does not independently establish uniqueness. The package also contains the 17
source-defined uniqueness flags, and the database tests above cover their data
invariants. Retain XML checksums and native preview/commit records for each
customer installation. Follow [installation and recovery](servicenow-update-set.md)
for deployment and upgrade steps.

## Revised installation metadata (2026-10-09)

The native export with SHA-256
`d85b21f883bf5f2b599eaf570547c4d02a682f59dc5a1eaff8b4a6fa4d1374e9`
contains 474 updates: 441 application definitions, the unchanged 32 index
payloads, and one Global Fix Script, **Nischoy Topo — Register Discovery
Source**. The Global child is named **Nischoy Topo — Database Indexes**.
Application scope, ID and version are unchanged. The short description now
describes the managed discovery workflow. Twelve native licensing records
retain every setting, including `license_model=none`; their record identities
were remapped by table name during the earlier installation.

Native import, batch preview and commit passed over the existing application.
An exact-byte repeat also passed. Both sets reached Committed, and neither
preview had errors or warnings. This is an installation-metadata and repeat
test, separate from the published XML's fresh-install and upgrade tests.

The setup script created one active English discovery source from an absent
choice baseline. Repeated runs preserved its ID, fields and update timestamp.
Native snapshots preserved the 20 pre-existing discovery-source choices,
including IDs and displayed fields. Contract tests separately cover customer
labels, translations, dependent/domain choices, inactive entries, duplicates
and insert failure; those fixtures are simulated. A scoped, non-committing
`identifyCIEnhanced` request accepted the registered source without errors or
warnings. It required no added CMDB table-read privilege.

For XML installation, the administrator must run this packaged Fix Script
after committing the batch, with **Unloadable** cleared. Automatic execution
on XML commit is not claimed. The package excludes the native whole-field
discovery-source choice capture, which would include unrelated choices.

This revised XML has not been published or signed. Its checks do not replace
the published package's digest-specific evidence or establish namespace-account
custody. The current customer guide continues to describe the published XML.
