# Job delivery

Job delivery lets an operator ask a specific collector to do something
sooner than its next scheduled discovery pass, without the controller
ever making an inbound connection to the collector. This is slice 5, the
final slice, of the "collector enrollment, outbound mTLS, rotation,
heartbeats, and jobs" milestone; see [project plan](project-plan.md) for
the full staged plan. Like [heartbeats](heartbeats.md), job delivery
requires no additional infrastructure to use — it authenticates with
whatever credential a collector already presents, and is always
available, not opt-in behind a flag.

## How it works

Topo Agent is deliberately outbound-only: it never accepts inbound
connections, so the controller cannot push work to it. Instead, a
collector polls for work on its own initiative, riding the same
`-heartbeat-interval` cadence it already uses for liveness heartbeats
(default one minute) — there is no separate `-job-poll-interval` flag,
since both are cheap, frequent check-ins distinct from the heavier
discovery/delivery `-interval`.

1. An operator queues a job for a specific collector with
   `POST /v1/jobs`.
2. On its next check-in, the collector polls `GET /v1/jobs`, which
   returns every job queued for it and marks each one dispatched so a
   later poll does not redeliver it.
3. The collector runs each job and reports the outcome with
   `POST /v1/jobs/{id}/result`.
4. An operator can check a job's status at any time with
   `GET /v1/jobs/{id}`, which never has the side effect of dispatching it
   (unlike the collector's own poll).

```sh
# On the controller (works the same with or without -mtls):
./bin/topo serve -api-key-ref env:TOPO_API_KEY

# On the collector — no new flags: job polling rides -heartbeat-interval:
./bin/topo agent run \
  -controller-url https://topo-hub.internal:8443 \
  -api-key-ref env:TOPO_AGENT_API_KEY \
  -spool-dir /var/lib/topo-agent/spool -spool-key-ref env:TOPO_AGENT_SPOOL_KEY \
  -interval 15m -heartbeat-interval 1m

# From an operator's machine, ask that collector to discover right now:
curl -s -H "Authorization: Bearer $TOPO_API_KEY" -H 'Content-Type: application/json' \
  -d '{"collector_id":"my-collector","type":"discover"}' \
  https://topo-hub.internal:8443/v1/jobs
# {"job_id":"...","collector_id":"my-collector","type":"discover","created_at":"..."}

curl -s -H "Authorization: Bearer $TOPO_API_KEY" \
  https://topo-hub.internal:8443/v1/jobs/<job_id>
# {"job_id":"...","status":"succeeded","created_at":"...","completed_at":"..."}
```

There is exactly one job type today: `discover`, which triggers the same
discovery-and-deliver pass a normal `-interval` tick already runs — it is
the only capability `topo agent run` actually has. A job requesting any
other type is rejected at creation with 400, not accepted and silently
ignored later.

## Design choices worth knowing

- **Job polling and reporting are identity-bound the same way heartbeats
  and certificate rotation are.** A verified mTLS peer certificate's
  subject always overrides whatever `collector_id` the caller claims in a
  query parameter (`GET /v1/jobs`) or request body field
  (`POST /v1/jobs/{id}/result`), so a collector can only ever poll for
  and report its own jobs, never another collector's. A bearer-key-only
  request has no such stronger signal and uses the claimed value as-is,
  the same limitation heartbeats already have.
- **A job is delivered at most once.** `GET /v1/jobs` marks a job
  dispatched the moment it is returned; there is no redelivery if the
  collector crashes before reporting a result. A job an operator still
  cares about after that must be resubmitted — there is no automatic
  retry, matching this project's existing preference for simple,
  explicit behavior over a queue with redelivery semantics that would
  need their own edge cases worked out.
- **`POST /v1/jobs` — queuing a job for a collector — uses the same
  `s.auth()` middleware as every other admin-style action in this
  project (`POST /v1/enrollment-tokens` included).** The shared bearer
  key or any verified collector certificate is accepted; there is no
  separate admin-only credential distinguishing "can queue jobs for
  anyone" from "is a collector." This matches existing precedent rather
  than introducing a new trust tier in this slice.
- **A `discover` job's reported success reflects whether discovery itself
  succeeded, not whether the resulting observation was delivered
  synchronously.** Delivery already has its own robust, independent
  retry-via-spool path regardless of how discovery was triggered, so job
  tracking does not duplicate it.
- **Job state is in-memory only, like the enrollment token store and
  heartbeat store.** It does not survive a controller restart; a job
  queued but not yet polled before a restart is gone and must be
  resubmitted.

## Current limitations

- **No way to list every job, or every job for a given collector.**
  `GET /v1/jobs/{id}` looks up one job by ID; there is no admin browsing
  endpoint. Keep track of the `job_id` `POST /v1/jobs` returns.
- **No job cancellation.** A queued job cannot be withdrawn before a
  collector polls it.
- **No job history beyond the single current record per job.** Once
  reported, a job's record stays exactly as it was left; there is no
  audit trail of state transitions.
- **This completes the "collector enrollment, outbound mTLS, rotation,
  heartbeats, and jobs" milestone.** See [project plan](project-plan.md)
  for what comes after.
