# Windows WinRM discovery

Topo's current Windows discovery slices collect stable host and interface identity plus bounded volume, service, and patch inventory through fixed WS-Management CIM operations. This remains an in-progress part of the Windows WinRM alpha, not yet an enterprise-ready WinRM implementation.

## Audited operation contract

The plugin compiles each SOAP action, CIM resource URI, and WQL filter into the binary. A discovery request supplies endpoint URLs only; it cannot supply an action, resource, query, PowerShell script, or command.

| Operation | CIM class | Required | Normalized data |
| --- | --- | --- | --- |
| `computer_system` | `Win32_ComputerSystem` | Yes | hostname, domain/workgroup, manufacturer, model, logical CPU count, memory |
| `computer_system_product` | `Win32_ComputerSystemProduct` | Yes | machine UUID |
| `bios` | `Win32_BIOS` | Yes | BIOS serial |
| `operating_system` | `Win32_OperatingSystem` | Yes | edition/caption, version, build, architecture |
| `network` | `Win32_NetworkAdapterConfiguration` | No | interface index, description, MAC, addresses, prefixes |
| `volumes` | `Win32_LogicalDisk` | No | fixed disks, label, file system, size, free bytes |
| `services` | `Win32_Service` | No | name, display name, state, start mode, service account |
| `patches` | `Win32_QuickFixEngineering` | No | hotfix ID, description, raw installed-on value |

The network query is a fixed exact WQL string limited to IP-enabled adapters, and the volume query is limited to fixed disks. Service and patch projections are also fixed in code. Topo Lab matches every exact contract tuple again at the server boundary. Required-operation failure discards the affected target's inventory; an optional operation permission or parse failure emits `winrm_partial`, omits that category, and retains the rest of the host.

Volumes, services, and patches are currently normalized as structured host attributes. Collections are sorted by device ID, service name, or hotfix ID so repeated observations remain deterministic. `InstalledOn` is retained as source text because Windows formatting can be locale-dependent; compatibility fixtures remain required before relying on that field for date comparisons.

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

## NTLMv2 pilot usage

Production targets must expose WinRM over HTTPS with a certificate trusted by the relay. Put one `https://host:5986/wsman` endpoint on each line of the target file, then provide the username and password through environment-backed inputs:

```sh
TOPO_WINRM_USERNAME='EXAMPLE\topo-reader' \
TOPO_WINRM_PASSWORD='replace-with-secret-input' \
./bin/topo discover winrm \
  -targets winrm-targets.txt \
  -site pilot \
  -auth ntlm
```

For a local Windows account, use `SERVERNAME\username`; UPN form such as `username@example.test` is also accepted. The password has no CLI value flag. `-password-env` can name a different environment variable supplied by the deployment secret mechanism.

This mode implements NTLMv2 when the server advertises either the `NTLM` HTTP challenge or a `Negotiate` challenge containing NTLM. It is not a Kerberos/SPNEGO client. Domain environments that disable NTLM must wait for the Kerberos follow-up rather than weakening server policy.

Topo uses the narrowly scoped [Azure NTLMSSP implementation](https://github.com/Azure/go-ntlmssp) for NTLMv2 message construction and owns the HTTP handshake so a Basic-only challenge is never answered. The dependency does not provide transport encryption. [Microsoft's WinRM security guidance](https://learn.microsoft.com/en-us/powershell/scripting/security/remoting/winrm-security) notes that NTLM cannot establish the target server's identity, so Topo requires verified TLS and provides no TrustedHosts-style bypass.

## Security and transport behavior

- Non-Lab targets must use `https://`; the default client retains normal certificate-chain and hostname verification.
- Basic authentication is built in only for explicit loopback Lab mode. Production `-auth ntlm` never sends Basic credentials or falls back when a server offers only Basic.
- NTLM authentication is connection-oriented. The built-in transport disables HTTP/2, keeps each challenge exchange on one TLS connection, and caps authentication response headers and decoded challenge tokens at 64 KiB.
- Per-operation contexts bound the entire Enumerate/Pull sequence. Enumeration pages, object counts, and response bytes are capped.
- Target concurrency is bounded and cancellation propagates through HTTP requests.
- URLs containing user information, queries, or fragments are rejected so credentials and operation text cannot enter targets.
- Request options whose names indicate passwords, credentials, tokens, or secrets are rejected.
- Structured errors include the target and audited operation name, never credentials or arbitrary remote text.
- The fixed CIM operations do not use `Win32_Product` and cannot invoke methods or modify remote state.

## Current limitations and next slice

This slice does not yet collect installed software. Software inventory will read the supported uninstall registry locations and will not use `Win32_Product`, which can trigger MSI consistency checks. Kerberos and certificate authentication, sanitized Windows Server fixtures, the mixed 500-Linux/500-Windows acceptance test, and broader real-host compatibility validation also remain open. Treat NTLMv2 as a narrowly scoped pilot transport, not completion of the Windows milestone.
