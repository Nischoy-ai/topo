# Windows WinRM discovery

Topo collects stable Windows host/interface identity and bounded volume,
service, patch and machine-wide software inventory through fixed WS-Management
operations. Protocol simulation coverage and real-host validation scope are
recorded below.

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

The target file contains one endpoint URL per line. Blank lines and lines beginning with `#` are ignored. The password resolves from `env:TOPO_WINRM_PASSWORD` by default. `-password-ref env:NAME`, `-password-ref file:/absolute/path`, `-password-ref vault:<path>#<field>`, or `-password-ref k8s:[<namespace>/]<secret-name>#<field>` selects another source; there is no password value flag. See [credential references](credential-references.md) for the full provider list. The older `-password-env` flag remains a deprecated compatibility alias and cannot be combined with `-password-ref`.

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

## Supported scope

Software inventory covers only machine-wide software entries from the native and WOW6432Node uninstall views. Per-user uninstall hives are not loaded or inspected. The concurrent, repeated 500-Linux/500-Windows protocol acceptance gate passes. Kerberos and certificate authentication, a reusable sanitized multi-version fixture set, and broader real-host compatibility validation remain open. Focused Windows Server 2022/2025 host/interface candidate results are recorded below; they do not close broader inventory validation. Treat NTLMv2 as a narrowly scoped pilot transport, not proof of real-host compatibility.

## Unpublished mixed-inventory candidate

The 0.4.7 source candidate adds `-host-interfaces-only`, limiting collection to
computer identity and network adapters without a WinRS shell. It corrects WQL
namespace URI and NTLM HTTP/1.1 ALPN behavior. Production-executor tests on
non-admin Windows Server 2022 and 2025 returned two assets and one relationship
per scan, zero errors and stable identities across two scans, with verified TLS.
See [candidate evidence](evidence/mixed-inventory.md). This focused result does
not validate broader standalone inventory, the Windows service wrapper or the
managed ServiceNow installation. The published worker is unchanged.

## Windows target compatibility matrix

This matrix applies to remote computer/interface collection by Linux/macOS
workers. It does not cover Windows worker distribution or agent service
registration. Direct collection is distinct from managed ServiceNow acceptance.

| Target | Verified transport and privilege | Direct candidate collection | Complete managed flow |
| --- | --- | --- | --- |
| Windows Server 2022, Azure lab | HTTPS/5986, independently trusted IP SAN certificate, NTLMv2; non-admin scan account with Remote Management Users and WMI read access | Two scans, 2 assets/1 relationship, zero errors, stable identity; offline IRE preview | Passed on the replacement developer instance: restricted OAuth, attempt-bound Password2, Linux worker collection/result ingestion, IRE apply and repeated reconciliation; 2 assets/1 relation per task |
| Windows Server 2025, Azure lab | Same restricted transport and privilege contract | Two scans, 2 assets/1 relationship, zero errors, stable identity; offline IRE preview | Passed through the same managed flow; 2 assets/1 relation per task |

Direct evidence is attributed to the unchanged collection implementation at
`997b796a11b9636c04030ede08b4984866b00539`; executable attribution and limits are
in [candidate evidence](evidence/mixed-inventory.md). The 2026-10-11 managed runs used worker source
`78ec6e1f57c89ad0c58b52960920a79ecce7a83c` after fixing empty-asset serialization.
The replacement instance had native app 0.4.7 installed before Windows ownership
was handed over. The original-instance CICD blocker was subsequently cleared; native restoration
and companion-index acceptance are recorded separately in the candidate evidence.
Replacement-instance Windows results do not establish those installation results.

| Failure/authority gate | Deterministic coverage | Real managed evidence |
| --- | --- | --- |
| Untrusted certificate, verified server identity, NTLM with no Basic fallback | WinRM transport/configuration tests | Both real targets: empty trust rejected collection; wrong TLS server identity rejected the handshake before NTLM; zero CIs from either negative |
| Wrong credentials, permission denial, timeout, cancellation, bounded responses | Protocol fault-isolation fixtures and worker cancellation tests | Wrong passwords: managed no-data completion on both targets; bounded deadline failures and live ServiceNow cancellation passed. Dedicated non-admin account authenticated with NTLM on both targets but WMI returned AccessDenied; managed no-data completion passed. Lab network blackholes hit the production 10-second deadline; real responses exceeded a test-local 64-byte read budget (the production 1 MiB threshold remains fixture-tested) |
| Protocol, profile/revision and scope binding | Production Password2 broker fixture rejects mismatches before decryption | Live matching binding succeeded. Disposable native task protocol/profile/revision/scope mismatches each returned 409, then matching resolution succeeded after restoration. Native audit recorded binding_mismatch; no-decryption ordering remains production-script fixture attribution |
| Worker identity, boot, task, attempt, token, expiry and renewed-attempt binding | Production broker plus production lease validator fixture; denials precede decryption | Wrong worker/boot/attempt/token returned 403; terminal, cancelled and expired authority denied; expired task recovered with a new attempt; cancelled-expiry slot cleanup has a platform-semantics regression |
| Secret-free broker/provider errors | Decryption exception and credential-provider redaction tests | Live negative observations checked against bearer/lease/password values before submission; broker errors expose HTTP status only; audit inspection retained privately |
| Repeat IRE reconciliation, stable identity after address change | Direct two-scan identities and offline mapping only | Repeated live IRE: UPDATE,UPDATE,NO_CHANGE per target, four unique source-to-CI mappings and two ownership relations. Address-change testing remains pending |

Live and fixture evidence remain distinct. The Windows managed flow and listed
negative gates passed on the replacement instance; native installation and
broader pilot gates remain separate as described in the evidence record.
At that checkpoint the published 0.4.6 managed beta supported Linux targets only.
The matching 0.4.7 worker and native package are now published; follow
[mixed setup](mixed-inventory-setup.md) and [publication evidence](evidence/mixed-inventory.md).
Publication does not broaden the dated compatibility matrix above.
