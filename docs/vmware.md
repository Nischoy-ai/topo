# VMware vCenter discovery

Topo's VMware discovery slice collects virtual machine and host inventory from vCenter (or a standalone ESXi host) over the vSphere API, using read-only enumeration only. This is the second slice of the SNMP/VMware discovery milestone; it never issues a power, configuration, or lifecycle operation against a managed object.

## What is collected

A discovery request supplies vCenter/ESXi endpoint targets only; it cannot supply a managed object reference, property filter, or any write operation. For each target, the plugin creates a read-only inventory view scoped to `HostSystem` and `VirtualMachine` objects and retrieves a fixed property set.

| Object | Properties | Normalized data |
| --- | --- | --- |
| `HostSystem` | `name`, `summary`, `config.network` | hardware UUID (identity), hostname, vendor/model/CPU model/CPU MHz/memory, physical NIC name + MAC address |
| `VirtualMachine` | `name`, `summary`, `config.hardware.device` | instance UUID (identity), name, guest OS, CPU/memory, power state, running host, virtual NIC name (`eth-<device key>`) + MAC address |

Asset identity is never an IP address or vCenter inventory path (both change independently of the underlying object): a host's identity is its hardware UUID, and a VM's identity is its VC-managed instance UUID, falling back to its BIOS UUID only when no vCenter has assigned one (a standalone ESXi host with no vCenter). A host or VM missing every identity field is skipped for that one object rather than failing the whole target — unlike Topo's other discovery plugins, one vCenter target here yields many assets, so one malformed object should not discard an otherwise healthy inventory. A `vm_runs_on_host` relationship links each VM to the host it is currently running on when that host was itself discovered; `host_has_interface` and `vm_has_interface` link each host and VM to its interface assets.

Host and VM listings are each bounded to 100,000 objects, matching the bounded-read requirement every Topo plugin follows. Listing hosts is required — a failure fails the whole target with a retryable `vmware_operation` error. Listing VMs is optional: a failure emits a retryable `vmware_partial` error and returns host-only inventory for that target, the same required/optional split the SSH, WinRM, and SNMP plugins use for their own core-vs-supplementary operations.

## Authentication and transport

Production targets must use HTTPS with normal certificate verification — there is no insecure fallback outside Topo Lab. Username and password are supplied through Topo's shared, bounded credential-reference contract (`env:`, `file:`, `vault:`, `k8s:`), never as CLI values, and are always passed to `Login` separately from the target URL: a target string containing embedded credentials (`https://user:pass@host/sdk`) is rejected outright, the same rule WinRM and SNMP already enforce for their own targets.

```sh
TOPO_VMWARE_PASSWORD_REF=vault:secret/vmware#password \
./bin/topo discover vmware \
  -targets vcenter-targets.txt \
  -site pilot \
  -username 'topo-reader@vsphere.local' \
  -password-ref vault:secret/vmware#password
```

A read-only vCenter role covering `HostSystem` and `VirtualMachine` inventory is sufficient; no configuration, power, or lifecycle privileges are used or required. See [credential references](credential-references.md) for the full provider list.

## Dependency

The plugin uses [`github.com/vmware/govmomi`](https://github.com/vmware/govmomi), the official vSphere Go SDK, pinned to `v0.52.0` — the last release declaring `go 1.23.0` compatibility with this project's pinned toolchain; newer releases require `go 1.24` or `go 1.25` and were deliberately not used to avoid silently bumping the language version CI is pinned to. This is the project's fourth external dependency, after `golang.org/x/crypto` (SSH), `github.com/Azure/go-ntlmssp` (WinRM NTLM), and `github.com/gosnmp/gosnmp` (SNMP) — each added for the same reason: hand-rolling a well-trodden, security-sensitive wire protocol from scratch is exactly what this project's "prefer standard-library components and narrowly scoped dependencies" principle exists to weigh against, not to forbid outright.

## Testing against vcsim

Unlike SNMP — where gosnmp is client-only and Topo Lab had to hand-roll an SNMPv3 agent from scratch — govmomi ships its own vCenter simulator, `vcsim`, built specifically for testing code like this plugin. Topo Lab therefore does not need (and does not have) a hand-rolled VMware fixture; the acceptance test in `pkg/discovery/vmware/integration_test.go` runs the plugin directly against `govmomi/simulator`, over the same real HTTPS SOAP wire protocol a production vCenter speaks — including a self-signed TLS certificate (vcsim defaults to plaintext HTTP; the test explicitly enables TLS to match production more closely) and real credential enforcement (vcsim accepts any non-empty username/password by default unless a specific login is configured, which the test does deliberately so a wrong-password case is a real, meaningful failure).

To explore the plugin manually against a local vcsim instance without writing Go, use govmomi's simulator package directly in a short script, or run a vcsim binary built from the `github.com/vmware/govmomi` module at the pinned version — see [vcsim's own documentation](https://github.com/vmware/govmomi/tree/main/vcsim) for the current recommended way to build and run it standalone, since its packaging has moved between govmomi releases.

## Security and transport behavior

- Production targets must use HTTPS with normal certificate and hostname verification; there is no fallback to HTTP or skipped certificate verification outside Topo Lab.
- Request options whose names indicate passwords, secrets, tokens, or credentials are rejected.
- Target URLs must not contain embedded credentials, a query string, or a fragment.
- The username is bounded and checked for control characters; the password is bounded and never accepted as a CLI value, only through credential references.
- Host and VM listings are bounded to 100,000 objects per target.
- Target concurrency is bounded and cancellation propagates through the underlying vSphere API calls.
- Structured errors include the target and failing operation, never credentials.
- Only read-only vSphere API calls are made: enumerating a container view and retrieving object properties. No power, configuration, snapshot, or lifecycle operation is ever issued.

## Current limitations and next work

This slice covers `HostSystem` and `VirtualMachine` inventory only — no datastores, networks, resource pools, folders, or vApps, and no VMware Tools-reported guest IP addresses (guest network state requires Tools running, which is not guaranteed; virtual NIC MAC addresses come from the VM's own hardware configuration instead, which is always available). Real vCenter/ESXi verification beyond vcsim has not been performed; treat this the same way WinRM real-host fixtures and SNMP `authPriv` are treated — implemented and tested against a faithful simulator, not yet proven against a live system. This completes the SNMP/VMware discovery milestone's two planned slices; see `docs/project-plan.md` for what comes next.
