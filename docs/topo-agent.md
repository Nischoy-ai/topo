# Topo Agent

Topo Agent is an outbound-only endpoint collector for systems where inbound
remote-management access or distributed remote credentials are undesirable.
It runs on the endpoint itself, discovers that one host on an interval using
the same non-privileged local plugin `topo discover local` uses, and pushes
each observation to a Topo Hub controller's ingestion API. It never listens
for inbound connections, accepts jobs, or executes remote-controlled
commands.

## Running the agent

```sh
./bin/topo agent run \
  -controller-url https://topo-hub.internal:8443 \
  -api-key-ref env:TOPO_AGENT_API_KEY \
  -spool-dir /var/lib/topo-agent/spool \
  -spool-key-ref env:TOPO_AGENT_SPOOL_KEY \
  -interval 15m
```

| Flag | Default | Purpose |
| --- | --- | --- |
| `-controller-url` | required (or `TOPO_AGENT_CONTROLLER_URL`) | Controller base URL, `http://` or `https://`. |
| `-api-key-ref` | optional `env:TOPO_AGENT_API_KEY` | Credential reference for the controller's bearer API key; see [credential references](credential-references.md). |
| `-spool-dir` | required | Absolute path to the encrypted offline-buffer directory. Created with owner-only permissions if it does not exist. |
| `-spool-key-ref` | required `env:TOPO_AGENT_SPOOL_KEY` | Credential reference for the 64-hex-character (32-byte) AES-256 spool encryption key. Generate one with `openssl rand -hex 32`. |
| `-spool-max-bytes` | `67108864` (64 MiB) | Maximum total bytes retained in the offline buffer. |
| `-interval` | `15m` | How often the agent discovers and attempts delivery. |
| `-site` | `default` | Site ID recorded on each observation. |
| `-collector` | local hostname | Collector ID recorded on each observation. |

The agent runs in the foreground and shuts down gracefully on `SIGINT` or
`SIGTERM`, matching `topo serve`. Run it under a process supervisor (systemd,
a container runtime, Windows Service Manager via a wrapper) to survive
reboots; Topo does not yet install or manage that supervision itself — see
[Current limitations](#current-limitations).

## Delivery and offline buffering

On each tick, the agent first retries anything already spooled, oldest
first, then performs one fresh discovery pass and attempts immediate
delivery:

- A successful delivery removes the spool entry (if it came from the spool)
  or requires no further action.
- A retryable failure (network error, or the controller returning a 5xx
  status) buffers the observation and stops draining the spool for this
  tick, preserving delivery order for the next attempt.
- A non-retryable failure (the controller returning a 4xx status — the
  payload itself is invalid) drops that observation rather than retrying it
  forever, and is logged as an error.
- If the spool cannot accept a new entry because it has reached
  `-spool-max-bytes`, the observation is dropped and logged rather than
  growing the spool without limit.

Spool entries are AES-256-GCM encrypted with the key from `-spool-key-ref`,
one file per observation, written atomically (temp file plus rename) with
`0600` permissions in a `0700` directory. A tampered or corrupted entry fails
AEAD authentication on read and is dropped with a logged error rather than
being delivered as if it were valid.

## Current limitations

- **No collector enrollment or mTLS.** The agent authenticates with the same
  static bearer API key `topo serve` accepts today; per-device enrollment,
  short-lived certificates, and rotation/revocation are a later, separately
  scoped roadmap item.
- **No native controller TLS termination.** The controller does not
  terminate TLS itself yet; place a TLS-terminating reverse proxy in front
  of it for any deployment beyond local evaluation, the same guidance that
  already applies to `topo serve`.
- **No OS service integration yet.** `topo agent run` is a foreground
  process; systemd unit files, Windows service wrapping, and install/
  uninstall tooling are the next slice of this milestone.
- **No job delivery.** The agent only self-reports on its own schedule; it
  cannot be remotely instructed to run different discovery.
- **Linux and Windows only** in scope for this milestone; no macOS agent.
