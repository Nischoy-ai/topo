# Collector heartbeats

Heartbeats let the controller tell a collector is alive between discovery
scans, without waiting for the next full observation delivery. This is
slice 4 of the "collector enrollment, outbound mTLS, rotation, heartbeats,
and jobs" milestone; see [project plan](project-plan.md) for the full
staged plan. Unlike enrollment, outbound mTLS, and certificate rotation
([Collector enrollment](enrollment.md)), heartbeats require no additional
infrastructure to use: they authenticate with whatever credential a
collector already presents — the bearer API key or a verified mTLS client
certificate — and are always available, not opt-in behind a flag.

## How it works

1. `topo agent run` sends `POST /v1/heartbeats` on its own independent
   cadence (`-heartbeat-interval`, default one minute), separate from the
   `-interval` discovery/delivery cadence. A heartbeat carries only a
   schema version, collector ID, and site ID — no discovery payload.
2. The controller records each collector's most recent heartbeat in
   memory, keyed by collector ID.
3. `GET /v1/collectors` lists every collector the controller has ever
   received a heartbeat from, each with its last heartbeat time and
   whether it counts as still alive.

```sh
# On the controller (heartbeats work the same with or without -mtls):
./bin/topo serve -api-key-ref env:TOPO_API_KEY

# On the collector:
./bin/topo agent run \
  -controller-url https://topo-hub.internal:8443 \
  -api-key-ref env:TOPO_AGENT_API_KEY \
  -spool-dir /var/lib/topo-agent/spool -spool-key-ref env:TOPO_AGENT_SPOOL_KEY \
  -interval 15m -heartbeat-interval 1m

# From an operator's machine:
curl -s -H "Authorization: Bearer $TOPO_API_KEY" \
  https://topo-hub.internal:8443/v1/collectors
# {"count":1,"items":[{"collector_id":"...","site_id":"...","last_heartbeat":"...","alive":true}]}
```

| Flag | Command | Purpose |
| --- | --- | --- |
| `-heartbeat-interval` | `topo agent run` | Interval between liveness heartbeats, independent of `-interval`. Default one minute; `0` disables heartbeats entirely. |

## Why a separate cadence

`topo agent run` already delivers an observation on every `-interval`
tick, which itself proves the collector was alive at that moment. But
`-interval` is often long (the default is 15 minutes, and some
deployments run it much longer), so relying on observation delivery alone
means the controller can't distinguish "briefly unreachable" from "down"
without waiting up to a full interval. A heartbeat is cheap enough to send
far more often than a full discovery pass, so `-heartbeat-interval`
defaults to a much shorter one minute, independent of `-interval` — the
two run on entirely separate tickers inside `agent run`, and neither
blocks the other.

## Design choices worth knowing

- **A heartbeat over mTLS is authenticated the same way as
  [certificate rotation](enrollment.md#renewing-a-certificate): the
  collector ID comes from the verified peer certificate, not from
  anything the client claims in the request body.** A request over mTLS
  that claims a different `collector_id` in its body is recorded under
  the certificate's real identity anyway, so a collector can never
  heartbeat, and therefore appear alive, as a different collector. A
  heartbeat authenticated by the bearer API key has no such stronger
  identity signal available, so it is recorded under whatever
  `collector_id` the request body states.
- **Heartbeats are best-effort, not durable, and never retried or
  spooled.** Unlike a failed observation delivery — which the agent
  buffers to its encrypted offline spool and retries indefinitely, because
  losing an observation would be a real gap in discovery data — a failed
  heartbeat is simply logged and dropped. A stale heartbeat has no lasting
  value once the next one supersedes it a minute later, so there is
  nothing worth buffering.
- **Heartbeat state is in-memory only, like the enrollment token store and
  every other piece of controller state today.** It does not survive a
  controller restart; every collector is reported as never having sent a
  heartbeat until it sends its next one.
- **Liveness is a fixed three-minute staleness window
  (`controller.DefaultHeartbeatStaleAfter`), not something set per
  collector.** The controller has no reliable way to know an individual
  collector's actual configured `-heartbeat-interval`, so it uses one
  constant threshold — three times the agent's own default interval — for
  every collector.

## Current limitations

- **No historical heartbeat log.** `GET /v1/collectors` reports only each
  collector's single most recent heartbeat, not a timeline of past ones.
- **No alerting.** Nothing yet notifies an operator when a collector goes
  stale; `GET /v1/collectors` must be polled.
- **No job delivery yet.** That is the next, final slice of this
  milestone.
