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

Run interactively, the agent runs in the foreground and shuts down
gracefully on `SIGINT` or `SIGTERM`, matching `topo serve`. For a deployment
that should survive reboots and restart on failure, install it as a systemd
service on Linux or a Windows service on Windows — see the two sections
below. The exact same `topo agent run` invocation works both ways: under
systemd it is just a supervised foreground process; on Windows, the same
command line auto-detects that the Service Control Manager started it and
switches into service mode internally.

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

## Running as a Linux systemd service

A unit file template and environment file template are in
[`packaging/systemd`](../packaging/systemd). systemd itself supervises the
process (`Restart=on-failure`), so no Go code is involved in installing it —
copy the templates and enable the unit:

```sh
sudo useradd --system --no-create-home --shell /usr/sbin/nologin topo-agent

sudo install -d -m 0750 -o topo-agent -g topo-agent /etc/topo-agent
sudo install -m 0640 -o root -g topo-agent packaging/systemd/topo-agent.env.example /etc/topo-agent/topo-agent.env
sudo "$EDITOR" /etc/topo-agent/topo-agent.env   # set TOPO_AGENT_CONTROLLER_URL

# Populate the credential files the unit references (file: reference, not
# an environment variable, so the values never appear in `systemctl show`
# or process environment dumps):
openssl rand -hex 32 | sudo tee /etc/topo-agent/spool-key >/dev/null
echo -n 'replace-with-a-long-random-value' | sudo tee /etc/topo-agent/api-key >/dev/null
sudo chown root:topo-agent /etc/topo-agent/spool-key /etc/topo-agent/api-key
sudo chmod 0640 /etc/topo-agent/spool-key /etc/topo-agent/api-key

make build   # produces ./bin/topo
sudo install -m 0755 -o root -g root bin/topo /usr/local/bin/topo
sudo install -m 0644 packaging/systemd/topo-agent.service /etc/systemd/system/topo-agent.service
sudo systemctl daemon-reload
sudo systemctl enable --now topo-agent
```

Check status and logs with `systemctl status topo-agent` and
`journalctl -u topo-agent -f`. The unit's `StateDirectory=topo-agent` gives
the service a writable `/var/lib/topo-agent` (used for `-spool-dir`) without
any other path being writable (`ProtectSystem=strict`), and it runs with an
empty capability set. Uninstall with:

```sh
sudo systemctl disable --now topo-agent
sudo rm /etc/systemd/system/topo-agent.service
sudo systemctl daemon-reload
sudo rm -rf /etc/topo-agent /var/lib/topo-agent
```

## Running as a Windows service

`topo agent run` detects when the Service Control Manager started it
(`svc.IsWindowsService()`) and switches into service mode automatically —
the same binary and the same flags work whether launched interactively or
as a service. `topo agent install` registers it:

```powershell
topo.exe agent install `
  -controller-url https://topo-hub.internal:8443 `
  -api-key-ref file:C:\ProgramData\TopoAgent\api-key `
  -spool-dir C:\ProgramData\TopoAgent\spool `
  -spool-key-ref file:C:\ProgramData\TopoAgent\spool-key

sc.exe start TopoAgent
```

Credential references are stored as-is in the service's persisted command
line (visible via `sc.exe qc TopoAgent`); `install` never resolves them, so
a resolved secret value is never written to the registry. Use `file:`
references pointing at files whose ACLs restrict read access to the account
the service runs under (`LocalSystem` by default) rather than `env:`, since
a service's environment is not the same as an interactive user's.

`install` also configures automatic start, three restart-on-failure
attempts spaced 30 seconds apart, and an Event Log source (`TopoAgent`) so
service output is visible in Event Viewer under Windows Logs → Application
even though a service has no attached console. Uninstall with:

```powershell
sc.exe stop TopoAgent
topo.exe agent uninstall
```

Windows service registration is exercised by cross-compilation
(`GOOS=windows go build`/`go vet`) and code review; it has not yet been
exercised end-to-end against a real Windows Service Control Manager. Treat
it as implemented-but-unverified-on-real-Windows until that gate closes,
the same posture this project already takes with WinRM real-host fixtures.

## Current limitations

- **No collector enrollment or mTLS.** The agent authenticates with the same
  static bearer API key `topo serve` accepts today; per-device enrollment,
  short-lived certificates, and rotation/revocation are a later, separately
  scoped roadmap item.
- **No native controller TLS termination.** The controller does not
  terminate TLS itself yet; place a TLS-terminating reverse proxy in front
  of it for any deployment beyond local evaluation, the same guidance that
  already applies to `topo serve`.
- **Windows service registration is unverified on real Windows.** See
  [Running as a Windows service](#running-as-a-windows-service).
- **No job delivery.** The agent only self-reports on its own schedule; it
  cannot be remotely instructed to run different discovery.
- **Linux and Windows only** in scope for this milestone; no macOS agent.
- **One agent instance per host.** The systemd unit and Windows service name
  are both fixed (`topo-agent` / `TopoAgent`); running multiple independently
  configured agents on one host is not supported.
