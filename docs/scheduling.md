# Server-side recurring discovery scheduling

Scheduling lets an operator ask the controller to keep a specific
collector discovering on a recurring cadence, independent of whatever
`-interval` that collector's own `topo agent run` happens to be started
with. This is slice 3, the final slice, of the "persistent observation/audit
storage and scheduling" milestone; see [project plan](project-plan.md) for
the full staged plan. It builds directly on [job delivery](jobs.md): a
schedule is a standing instruction to queue a `discover` job for a
collector every so often, using the exact same `POST /v1/jobs` /
`GET /v1/jobs` machinery already in place.

## How it works

1. An operator creates a recurring schedule for a collector with
   `POST /v1/schedules`.
2. Topo Agent is deliberately outbound-only, so there is no background
   ticker on the controller pushing work out. Instead, the schedule becomes
   an actual job the same way any other job does: lazily, the next time
   that collector polls `GET /v1/jobs`. If the schedule is due (its
   `next_run_at` has passed), the controller queues a `discover` job for it
   before returning the poll response — from the collector's point of
   view, indistinguishable from a job an operator queued manually with
   `POST /v1/jobs`.
3. The schedule's `next_run_at` advances to `now + interval_seconds` at
   that point, and the cycle repeats.

```sh
# On the controller:
./bin/topo serve -api-key-ref env:TOPO_API_KEY -db-driver sqlite -db-dsn /var/lib/topo/topo.db

# On the collector — no new flags; scheduled jobs ride the same poll as
# manually queued ones, which already rides -heartbeat-interval:
./bin/topo agent run \
  -controller-url https://topo-hub.internal:8443 \
  -api-key-ref env:TOPO_AGENT_API_KEY \
  -spool-dir /var/lib/topo-agent/spool -spool-key-ref env:TOPO_AGENT_SPOOL_KEY \
  -interval 15m -heartbeat-interval 1m

# From an operator's machine, ask that collector to discover every hour:
curl -s -H "Authorization: Bearer $TOPO_API_KEY" -H 'Content-Type: application/json' \
  -d '{"collector_id":"my-collector","interval_seconds":3600}' \
  https://topo-hub.internal:8443/v1/schedules
# {"collector_id":"my-collector","job_type":"discover","interval_seconds":3600,"next_run_at":"...","created_at":"...","updated_at":"..."}

curl -s -H "Authorization: Bearer $TOPO_API_KEY" https://topo-hub.internal:8443/v1/schedules
# {"count":1,"items":[{"collector_id":"my-collector", ...}]}

# Stop scheduling discovery for this collector:
curl -s -X DELETE -H "Authorization: Bearer $TOPO_API_KEY" \
  https://topo-hub.internal:8443/v1/schedules/my-collector
```

`type` is optional in the request body and defaults to `discover`, the
only job type that exists today (same as `POST /v1/jobs`); a schedule
requesting any other type is rejected at creation with 400.
`interval_seconds` must be between 60 (one minute) and 604800 (one week) —
below the minimum, a misconfigured schedule could hammer a collector on
every poll; above the maximum, "recurring" stops being a meaningful
distinction from a one-off `POST /v1/jobs`.

## Design choices worth knowing

- **There is exactly one schedule per collector.** `POST /v1/schedules`
  is an upsert keyed by `collector_id`, matching this project's existing
  idempotent-upsert precedent (`SaveObservation`, asset/relationship
  resolution). Creating a schedule for a collector that already has one
  replaces it outright; there is no way to run two independent recurring
  schedules against the same collector.
- **Every `POST /v1/schedules` call — create or update — sets
  `next_run_at` to now.** The effect is always "apply this schedule
  starting now," so narrowing an interval takes effect on the very next
  poll rather than waiting out whatever was left of the old cadence.
  `created_at` is preserved across an update, though; only `next_run_at`
  and `updated_at` reset.
- **A schedule does not pile up jobs.** If a job of the schedule's type is
  still outstanding for that collector (queued but not yet resulted) when
  the schedule comes due again, the controller does not queue a second one
  — it leaves `next_run_at` where it is and tries again on the collector's
  next poll. A collector that was offline for several intervals catches up
  with exactly one job, not a backlog.
- **No catch-up multiplier.** Related to the above: `next_run_at` always
  advances to `now + interval_seconds` when a job is actually queued, never
  to `old next_run_at + interval_seconds`. A collector offline for six
  hours against a 15-minute schedule gets one job on reconnect, not 24
  queued at once.
- **A schedule for a collector that never polls never produces a job.**
  This is not a bug to work around — Topo Agent is deliberately
  outbound-only, so there is no way to reach an unreachable collector
  regardless of what the controller's job-delivery mechanism looks like.
- **Schedule changes are audited.** `POST /v1/schedules` records
  `schedule_created` or `schedule_updated`, and `DELETE
  /v1/schedules/{collector_id}` records `schedule_deleted`, in the same
  hash-chained log as enrollment token issuance, collector enrollment,
  certificate rotation, and job creation. See
  [Persistent storage and the audit log](storage.md#audit-log).
- **Schedules are persisted the same way discovery data and the audit log
  are.** Under `-db-driver sqlite`, a schedule survives a controller
  restart; under the default `-db-driver memory`, it does not — this
  differs from the older enrollment-token, heartbeat, and job state, which
  are always in-memory only regardless of `-db-driver`. A recurring
  schedule is a standing operator policy, not a short-lived or
  self-healing runtime record like a heartbeat or a single job, so
  silently losing it on every restart would be a real, easy-to-miss
  operational surprise — unlike, say, a lost enrollment token, which an
  operator just reissues.
- **All schedule endpoints are operator actions.** `POST /v1/schedules`,
  `GET /v1/schedules`, and `DELETE /v1/schedules/{collector_id}` require the
  configured bearer key; a verified collector certificate alone receives
  `403 Forbidden`.

## Current limitations

- **No per-collector schedule history.** Like job state, a schedule's
  record is exactly its current configuration; there is no log of past
  interval changes beyond what the audit log's `schedule_updated` entries
  incidentally capture.
- **One job type, one schedule per collector.** There is no way to
  schedule two different kinds of work, or the same kind of work at two
  different cadences, against one collector.
- **This completes the "persistent observation/audit storage and
  scheduling" milestone.** See [project plan](project-plan.md) for what
  comes after.
