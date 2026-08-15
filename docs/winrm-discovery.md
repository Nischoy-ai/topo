# Windows WinRM discovery

Topo's first Windows discovery slice collects stable host and interface identity through fixed WS-Management CIM operations. It is an in-progress part of the Windows WinRM alpha, not yet an enterprise-ready WinRM implementation.

## Audited operation contract

The plugin compiles each SOAP action, CIM resource URI, and WQL filter into the binary. A discovery request supplies endpoint URLs only; it cannot supply an action, resource, query, PowerShell script, or command.

| Operation | CIM class | Required | Normalized data |
| --- | --- | --- | --- |
| `computer_system` | `Win32_ComputerSystem` | Yes | hostname, domain/workgroup, manufacturer, model, logical CPU count, memory |
| `computer_system_product` | `Win32_ComputerSystemProduct` | Yes | machine UUID |
| `bios` | `Win32_BIOS` | Yes | BIOS serial |
| `operating_system` | `Win32_OperatingSystem` | Yes | edition/caption, version, build, architecture |
| `network` | `Win32_NetworkAdapterConfiguration` | No | interface index, description, MAC, addresses, prefixes |

The network query is a fixed exact WQL string limited to IP-enabled adapters. Topo Lab matches the exact contract again at the server boundary. Required-operation failure discards the affected target's inventory; optional network failure emits `winrm_partial` and retains the host.

## Topo Lab usage

Build Topo, then start the Lab frontend on its default loopback address:

```sh
make build
./bin/topo lab winrm-serve -scenario examples/lab/clean-500.json
```

In another terminal, create a target file and run discovery:

```sh
./bin/topo lab winrm-targets \
  -scenario examples/lab/clean-500.json > winrm-targets.txt
TOPO_WINRM_PASSWORD=topo-lab ./bin/topo discover winrm \
  -targets winrm-targets.txt \
  -site lab \
  -lab-basic
```

The target file contains one endpoint URL per line. Blank lines and lines beginning with `#` are ignored. The password is read only through the environment variable named by `-password-env`; there is no password value flag.

`-lab-basic` is deliberately required. In this mode every target must resolve syntactically to `localhost` or a loopback IP, and the Lab server itself refuses a non-loopback listen address. Do not proxy this endpoint to another network.

## Security and transport behavior

- Non-Lab targets must use `https://`; the default client retains normal certificate-chain and hostname verification.
- Basic authentication is built in only for explicit loopback Lab mode. Production callers can inject an authenticated HTTP client, but the CLI does not yet provide NTLM/Negotiate.
- Per-operation contexts bound the entire Enumerate/Pull sequence. Enumeration pages, object counts, and response bytes are capped.
- Target concurrency is bounded and cancellation propagates through HTTP requests.
- URLs containing user information, queries, or fragments are rejected so credentials and operation text cannot enter targets.
- Request options whose names indicate passwords, credentials, tokens, or secrets are rejected.
- Structured errors include the target and audited operation name, never credentials or arbitrary remote text.

## Current limitations and next slice

This slice does not yet collect volumes, installed software, patches, or services. Software inventory will read the supported uninstall registry locations and will not use `Win32_Product`, which can trigger MSI consistency checks. Built-in NTLM/Negotiate, Kerberos/certificate follow-up decisions, sanitized Windows Server fixtures, the mixed 500-Linux/500-Windows acceptance test, and real-host compatibility validation also remain open. Do not use this slice for an enterprise pilot until those gates are complete.
