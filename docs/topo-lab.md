# Topo Lab

For the SSH protocol frontend and the production Linux SSH plugin, see [Linux SSH discovery](ssh-discovery.md).

Topo Lab simulates large IT estates without creating hundreds of virtual machines. It is deterministic: the same scenario and seed produce the same host identities, attributes, relationships, and injected faults.

Topo Lab currently uses an explicit HTTP lab protocol. This exercises network requests, bounded concurrency, cancellation, timeouts, decoding, partial results, identity stability, and repeated scans. It does **not** claim to validate SSH or WinRM compatibility. Those protocol frontends will reuse the same persona engine as their production discovery plugins are implemented.

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

Fault percentages are mutually exclusive and may total at most 100%. `permission_denied` returns partial inventory; `disappear` succeeds once and returns HTTP 410 on later scans. Timeouts respect request cancellation.

## Ground truth and evaluation

Every scenario produces a canonical `ObservationEnvelope` containing two assets per host—a host and its primary interface—and one relationship. `topo lab run` compares discovered stable asset identities with that graph and reports coverage, missing assets, and unexpected assets.

The automated test suite performs two scans of 500 mixed hosts, saves both observations through the controller repository, and asserts that the resolved asset count remains 1,000. This catches duplicate creation across repeat scans.

## Boundaries

Simulation is the primary scale and failure-testing mechanism, but it cannot prove compatibility with real shell quoting, PowerShell/CIM behavior, locales, authentication policies, permissions, or OS updates. Each production discovery plugin will therefore retain a small real-system compatibility matrix alongside large simulated tests.
