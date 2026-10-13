# ServiceNow index checklist for Topo 0.4.7

This checklist contains all 32 required ordered index definitions, including
17 configured unique. The definitions match the compiled candidate source and
the unchanged native companion exactly. Physical coverage was audited after
companion restoration; uniqueness enforcement was not independently exercised.
Follow the [installation guide](servicenow-update-set.md) and verify signed package
checksums before importing the combined XML.

Keep workers stopped and schedules inactive until installation checks finish.
Do not drop existing indexes or create additional copies during normal setup.
If preview reports conflicts or duplicate data, review them before committing.

Open each table's **Database Indexes** related list and compare the ordered
columns below. Generated physical index names can differ; expand truncated
column lists in the full form. **Is Droppable** is not a uniqueness indicator.

## Required definitions

Every table name below has the prefix `x_664635_topo_`. The definition name is
from the source; ServiceNow may assign a different physical index name.
The checklist was derived from a clean SDK 4.9.0 build of the unchanged index
declarations originally exported for 0.4.6; candidate 0.4.7 field additions
do not change these 32 index definitions. Keep it synchronized with the build's
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

## Validation reference

[Historical 0.4.6 validation evidence](https://github.com/Nischoy-ai/topo/blob/533267125420f064c45562607772b3aeba512423/docs/servicenow-validation.md) records the earlier package’s installation,
repeat-import and key-enforcement tests plus administrator recovery procedures.
Those tests do not establish enforcement for this restored 0.4.7 installation.

[Current candidate evidence](https://github.com/Nischoy-ai/topo/blob/c70c5813895fcae20c54ee1fac54e0e44e9e531f/docs/evidence/mixed-inventory.md)
records all 32 ordered physical definitions present after companion restoration.
The 17 unique flags match configuration; enforcement was not independently exercised.
