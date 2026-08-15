# Topo Lab

For protocol-specific behavior, see [Linux SSH discovery](ssh-discovery.md) and [Windows WinRM discovery](winrm-discovery.md).

Topo Lab simulates large IT estates without creating hundreds of virtual machines. It is deterministic: the same scenario and seed produce the same host identities, attributes, relationships, and injected faults.

Topo Lab offers an explicit HTTP lab protocol for fast generic estate tests, an SSH frontend that performs genuine SSH handshakes and session-channel execution, and a WinRM frontend that performs WS-Management SOAP Enumerate/Pull exchanges against the same persona engine. The protocol acceptance suites validate Topo's client and operation routing while deterministic responses avoid provisioning VMs.

## Quick start

Build Topo and start a clean 500-host estate:

```sh
make build
./bin/topo lab serve -scenario examples/lab/clean-500.json
```

From another terminal, discover the estate twice and require complete asset coverage:

```sh
./bin/topo lab run \
  -scenario examples/lab/clean-500.json \
  -url http://127.0.0.1:9090 \
  -concurrency 64 \
  -repeat 2 \
  -min-coverage 100
```

Generate a new scenario or its canonical expected graph:

```sh
./bin/topo lab generate -hosts 1000 -windows-percent 35 -seed 2026 -out estate.json
./bin/topo lab expected -scenario estate.json > expected-observation.json
```

## Scenario contract

Scenarios use strict JSON. Unknown fields, unknown personas, invalid percentages, empty estates, and estates above the 100,000-host safety limit are rejected.

```json
{
  "version": "v1alpha1",
  "seed": 42,
  "site_id": "lab",
  "hosts": [
    {"persona": "ubuntu-24.04", "count": 700},
    {"persona": "windows-2022", "count": 300}
  ],
  "faults": {
    "auth_failure_percent": 2,
    "timeout_percent": 1,
    "permission_denied_percent": 3,
    "malformed_percent": 0.5,
    "disappear_after_first_scan_percent": 2,
    "latency_ms": 5,
    "jitter_ms": 20
  }
}
```

Built-in personas are `ubuntu-22.04`, `ubuntu-24.04`, `rocky-9`, `windows-2019`, `windows-2022`, and `windows-2025`. Each host receives deterministic machine and serial identifiers, hostname, address, MAC address, compute sizing, packages, and services.

## WinRM frontend

Start the loopback-only WS-Management frontend and produce Windows target URLs:

```sh
./bin/topo lab winrm-serve -scenario examples/lab/clean-500.json
./bin/topo lab winrm-targets -scenario examples/lab/clean-500.json
```

The endpoint uses the fixed username and password `topo-lab`. It accepts Basic authentication only because the server is restricted to loopback and intended solely for deterministic testing. The frontend handles real SOAP envelopes, enumeration contexts, and audited CIM routing. It rejects arbitrary actions, resource URIs, WQL, command bodies, and invalid contexts. Authentication failure, timeout, permission denial, malformed XML, latency/jitter, and disappear-after-first-scan faults use the scenario's existing deterministic fault assignment.

Fault percentages are mutually exclusive and may total at most 100%. `permission_denied` returns partial inventory; `disappear` succeeds once and returns HTTP 410 on later scans. Timeouts respect request cancellation.

## Ground truth and evaluation

Every scenario produces a canonical `ObservationEnvelope` containing two assets per host—a host and its primary interface—and one relationship. `topo lab run` compares discovered stable asset identities with that graph and reports coverage, missing assets, and unexpected assets.

The automated test suite performs two scans of 500 mixed hosts, saves both observations through the controller repository, and asserts that the resolved asset count remains 1,000. This catches duplicate creation across repeat scans.

## Boundaries

Simulation is the primary scale and failure-testing mechanism, but it cannot prove compatibility with real shell quoting, PowerShell/CIM behavior, locales, authentication policies, permissions, or OS updates. Each production discovery plugin will therefore retain a small real-system compatibility matrix alongside large simulated tests. The current WinRM slice does not yet include sanitized real-system fixtures, NTLM/Negotiate, or PowerShell-based registry inventory.
