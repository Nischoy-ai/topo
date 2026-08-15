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
- Require HTTPS with normal certificate and hostname verification for non-Lab WinRM targets. Basic authentication and HTTP are restricted to the explicit loopback-only Topo Lab mode.
- Never place credentials in job options, labels, logs, or observation attributes.
- Review ServiceNow IRE preview output before enabling destination writes.

## SSH discovery

The Linux SSH plugin never accepts a command from a controller job. Its commands are compiled into the binary and matched exactly by the Topo Lab SSH frontend. Passwords are read through a named environment variable and private keys through a file; neither is accepted as a command-line value or emitted in observations. Each command has a deadline and a bounded output buffer. Package and service permission failures produce partial inventory, while failures of identity or hardware commands reject that target's inventory.

## WinRM discovery

The Windows plugin's current operation set consists of compiled-in WS-Management action, CIM resource URI, and WQL tuples. Targets and jobs cannot provide SOAP actions, resource URIs, queries, PowerShell, or command text. The Topo Lab frontend independently matches the same exact tuples and rejects mismatched SOAP body operations, filters, and enumeration contexts.

Non-Lab targets must use HTTPS; Go's standard TLS hostname and certificate verification remains enabled. The CLI's Basic authentication mode is explicitly limited to loopback Topo Lab endpoints, and its password is read from an environment variable rather than a flag. Each CIM operation has a deadline, responses are bounded, enumeration pages and objects are capped, and target concurrency is controlled. Required identity/hardware failures reject that target; optional network permission failures retain a partial host. Built-in NTLM/Negotiate, registry software inventory, and real-system compatibility validation are not complete and must be added before an enterprise pilot.
