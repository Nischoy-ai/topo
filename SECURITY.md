# Security policy

Nischoy Topo is pre-alpha and has no supported production release yet. Report vulnerabilities privately to the maintainers; do not open a public issue containing exploit details or credentials.

## Trust boundaries

Collectors and agents process data from untrusted infrastructure. Destination APIs and discovery targets must be treated as hostile. Plugins must validate all configuration, use bounded reads and deadlines, avoid invoking a shell, redact secrets, and return structured errors. A plugin must never accept arbitrary commands from the controller.

The controller's bearer-key authentication is an evaluation bootstrap, not the final enterprise trust model. Before production readiness, Nischoy Topo requires per-device enrollment, short-lived mTLS certificates, rotation/revocation, encrypted persistent secrets, immutable audit events, signed artifacts and plugin manifests, SBOM generation, and external penetration testing.

## Deployment guidance

- Bind evaluation controllers to localhost or a private management interface.
- Use a long random API key and TLS-terminating reverse proxy.
- Use dedicated read-only discovery identities and restrict targets by allowlist.
- Never place credentials in job options, labels, logs, or observation attributes.
- Review ServiceNow IRE preview output before enabling destination writes.

