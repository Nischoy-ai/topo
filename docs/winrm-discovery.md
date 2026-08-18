# Windows WinRM discovery

Topo's current Windows discovery slices collect stable host and interface identity plus bounded volume, service, patch, and machine-wide installed-software inventory through fixed WS-Management operations. This remains an in-progress part of the Windows WinRM alpha, not yet an enterprise-ready WinRM implementation.

## Audited operation contract

The plugin compiles each SOAP action, CIM resource URI, WQL filter, and the sole PowerShell executable/argument vector into the binary. A discovery request supplies endpoint URLs only; it cannot supply an action, resource, query, PowerShell script, or command.

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
| `software` | 64-bit and 32-bit HKLM uninstall registry views | No | display name/version, publisher, raw install date, architecture, registry key |

The network query is a fixed exact WQL string limited to IP-enabled adapters, and the volume query is limited to fixed disks. Service and patch projections are also fixed in code. Topo Lab matches every exact contract tuple again at the server boundary. Required-operation failure discards the affected target's inventory; an optional operation permission or parse failure emits `winrm_partial`, omits that category, and retains the rest of the host.

The software operation follows [Microsoft's documented uninstall-registry inventory approach](https://learn.microsoft.com/en-us/powershell/scripting/samples/working-with-software-installations) and never queries `Win32_Product`. It uses the [documented WinRS shell lifecycle](https://learn.microsoft.com/en-us/openspecs/windows_protocols/ms-wsmv/a78e42df-66dc-4466-800d-086b42233d2d): create a text command shell, run only `powershell.exe -NoLogo -NoProfile -NonInteractive -EncodedCommand <reviewed constant>` without `cmd.exe`, receive stdout/stderr, check the exit code, and delete the shell. Topo reads `HKEY_LOCAL_MACHINE\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall` and its `WOW6432Node` counterpart. It deliberately omits uninstall strings and per-user hives.

Volumes, services, patches, and software are normalized as structured host attributes. Collections are sorted by stable source keys so repeated observations remain deterministic. `InstalledOn` and registry `InstallDate` are retained as source text because Windows formatting can be locale-dependent; compatibility fixtures remain required before relying on those fields for date comparisons.

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

The target file contains one endpoint URL per line. Blank lines and lines beginning with `#` are ignored. The password resolves from `env:TOPO_WINRM_PASSWORD` by default. `-password-ref env:NAME` or `-password-ref file:/absolute/path` selects another source; there is no password value flag. The older `-password-env` flag remains a deprecated compatibility alias and cannot be combined with `-password-ref`.

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

For a local Windows account, use `SERVERNAME\username`; UPN form such as `username@example.test` is also accepted. The password has no CLI value flag. Use `-password-ref file:/run/secrets/topo_winrm_password` for a restricted mounted file.

This mode implements NTLMv2 when the server advertises either the `NTLM` HTTP challenge or a `Negotiate` challenge containing NTLM. It is not a Kerberos/SPNEGO client. Domain environments that disable NTLM must wait for the Kerberos follow-up rather than weakening server policy.

Topo uses the narrowly scoped [Azure NTLMSSP implementation](https://github.com/Azure/go-ntlmssp) for NTLMv2 message construction and owns the HTTP handshake so a Basic-only challenge is never answered. The dependency does not provide transport encryption. [Microsoft's WinRM security guidance](https://learn.microsoft.com/en-us/powershell/scripting/security/remoting/winrm-security) notes that NTLM cannot establish the target server's identity, so Topo requires verified TLS and provides no TrustedHosts-style bypass.

## Security and transport behavior

- Non-Lab targets must use `https://`; the default client retains normal certificate-chain and hostname verification.
- Basic authentication is built in only for explicit loopback Lab mode. Production `-auth ntlm` never sends Basic credentials or falls back when a server offers only Basic.
- NTLM authentication is connection-oriented. The built-in transport disables HTTP/2, keeps each challenge exchange on one TLS connection, and caps authentication response headers and decoded challenge tokens at 64 KiB.
- Per-operation contexts bound the entire Enumerate/Pull sequence. Enumeration pages, object counts, and response bytes are capped.
- The software operation bounds the complete shell lifecycle, every SOAP response, cumulative decoded output, receive-message count, and normalized record count. Remote shell and command IDs are validated before reuse.
- Target concurrency is bounded and cancellation propagates through HTTP requests.
- URLs containing user information, queries, or fragments are rejected so credentials and operation text cannot enter targets.
- Request options whose names indicate passwords, credentials, tokens, or secrets are rejected.
- Structured errors include the target and audited operation name, never credentials or arbitrary remote text.
- The fixed operations do not use `Win32_Product`; the only remote command is the compiled-in read-only uninstall-registry script, and command output excludes uninstall strings.

## Current limitations and next slice

This slice collects only machine-wide software entries from the native and WOW6432Node uninstall views. Per-user uninstall hives are not loaded or inspected. The concurrent, repeated 500-Linux/500-Windows protocol acceptance gate passes. Kerberos and certificate authentication, sanitized Windows Server 2022 plus one other supported-release fixture set, and broader real-host compatibility validation remain open. The real-host fixture evidence is explicitly deferred, not completed. Treat NTLMv2 as a narrowly scoped pilot transport, not proof of real-host compatibility.
