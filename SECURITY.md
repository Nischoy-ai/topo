# Security policy

Nischoy Topo is pre-alpha and has no supported production release yet. Report vulnerabilities privately to the maintainers; do not open a public issue containing exploit details or credentials.

## Trust boundaries

Collectors and agents process data from untrusted infrastructure. Destination APIs and discovery targets must be treated as hostile. Plugins must validate all configuration, use bounded reads and deadlines, avoid locally constructed or user-supplied shell text, redact secrets, and return structured errors. A plugin must never accept arbitrary commands from the controller.

The controller's bearer-key authentication is an evaluation bootstrap, not the final enterprise trust model. Before production readiness, Nischoy Topo requires per-device enrollment, short-lived mTLS certificates, rotation/revocation, encrypted persistent secrets, immutable audit events, signed artifacts and plugin manifests, SBOM generation, and external penetration testing.

## Deployment guidance

- Bind evaluation controllers to localhost or a private management interface.
- Use a long random API key and TLS-terminating reverse proxy.
- Use dedicated read-only discovery identities and restrict targets by allowlist.
- Verify SSH host keys with a managed `known_hosts` file. `-insecure-host-key` exists only for isolated Topo Lab evaluation.
- Require HTTPS with normal certificate and hostname verification for non-Lab WinRM targets. Production NTLMv2 never falls back to Basic authentication. Basic authentication and HTTP are restricted to the explicit loopback-only Topo Lab mode.
- Never place credentials in job options, labels, logs, or observation attributes.
- Review ServiceNow IRE preview output before enabling destination writes.

## SSH discovery

The Linux SSH plugin never accepts a command from a controller job. Its commands are compiled into the binary and matched exactly by the Topo Lab SSH frontend. Passwords are read through a named environment variable and private keys through a file; neither is accepted as a command-line value or emitted in observations. Each command has a deadline and a bounded output buffer. Package and service permission failures produce partial inventory, while failures of identity or hardware commands reject that target's inventory.

## WinRM discovery

The Windows plugin's operation set consists of compiled-in WS-Management action, CIM resource URI, and WQL tuples plus one compiled-in PowerShell command for software inventory. Targets and jobs cannot provide SOAP actions, resource URIs, queries, PowerShell, or command text. The Topo Lab frontend independently matches the same exact tuples and command argument vector, rejects mismatched SOAP body operations, filters, selectors, shell options, enumeration contexts, and command IDs, and refuses arbitrary executables. Optional volume, service, and patch collection uses fixed, read-only CIM queries. Software collection reads only the 64-bit and 32-bit machine-wide uninstall registry views; it does not use `Win32_Product`, collect uninstall command strings, or inspect per-user hives.

Non-Lab targets must use HTTPS; Go's standard TLS hostname and certificate verification remains enabled. Production `ntlm` mode implements NTLMv2 over server `NTLM` or `Negotiate` challenges, disables HTTP/2 to retain connection affinity, caps authentication headers and tokens, and never answers a Basic-only challenge. It does not implement Kerberos/SPNEGO. The CLI reads the password from a named environment variable rather than a value flag. Lab Basic remains explicitly limited to loopback Topo Lab endpoints.

Each CIM or software operation has a deadline, responses and cumulative command output are bounded, enumeration pages, receive messages, objects, and software records are capped, and target concurrency is controlled. Remote shell and command identifiers are length- and control-character-checked before reuse, and created shells are deleted after command completion or failure while the operation context remains active. Required identity/hardware failures reject that target; optional network, volume, service, patch, or software permission and parse failures retain a partial host and identify the affected operation. Real-system compatibility fixtures and mixed-estate acceptance are not complete and must be added before claiming the Windows milestone complete.
