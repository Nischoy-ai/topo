# Developer quickstart

This guide is for building and exercising Topo from source. Customers starting
ServiceNow-controlled Linux discovery should use the [three-step
README](../README.md) and the [ServiceNow Linux pilot
quickstart](pilot-quickstart.md).

## Build and run locally

Topo requires Go 1.26 or later. Release and security evidence uses exact Go
1.26.8.

```sh
make test
make build
./bin/topo discover local
./bin/topo discover -format servicenow-preview local
```

`discover local` inventories the current computer and its interfaces. It is
not a credential-free LAN scan: an IP address is not a durable device identity,
and an ARP-cache row is not sufficient evidence for a computer CI.

## Direct ServiceNow IRE preview and apply

The standalone publisher is separate from the ServiceNow-controlled worker.
It previews locally by default and performs a non-committing IRE
`queryEnhanced` preflight before an explicit apply:

```sh
./bin/topo discover local > observation.jsonl
./bin/topo publish servicenow \
  -input observation.jsonl \
  -instance https://example.service-now.com > ire-preview.json
./bin/topo publish servicenow \
  -input observation.jsonl \
  -instance https://example.service-now.com \
  -token-ref file:/absolute/path/to/servicenow-token \
  -apply
```

This path installs no scoped application and does not emulate a MID Server,
probe, pattern, or sensor. See [ServiceNow IRE publishing](servicenow.md).

## Component guides

- [ServiceNow Linux pilot](pilot-quickstart.md)
- [ServiceNow-managed stateless worker](servicenow-worker.md)
- [Topo Lab and deterministic simulation](topo-lab.md)
- [Linux SSH discovery](ssh-discovery.md)
- [Windows WinRM discovery](winrm-discovery.md)
- [SNMPv3 discovery](snmp.md)
- [VMware discovery](vmware.md)
- [Kubernetes discovery](kubernetes.md)
- [AWS Organizations discovery](aws.md)
- [Azure tenant discovery](azure.md)
- [Controller storage, backup, and restore](storage.md)
- [Topo Agent](topo-agent.md)
- [Collector enrollment and mTLS](enrollment.md)
- [Credential references](credential-references.md)
- [Release artifacts](releases.md)
- [Package-manager distribution](distribution.md)

The [roadmap](../ROADMAP.md) distinguishes implemented behavior from remaining
evidence and planned work. Security-sensitive changes must follow
[SECURITY.md](../SECURITY.md) and the repository's test gates.
