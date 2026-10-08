# Topo documentation

Start with [installation in three steps](../README.md#start-servicenow-discovery-in-three-steps)
for ServiceNow-managed discovery. Use the guides below for configuration,
verification and component details.

## Install and operate

| Task | Guide |
| --- | --- |
| Install the ServiceNow application and indexes | [XML installation and recovery](servicenow-update-set.md) · [Index checklist](servicenow-index-setup.md) |
| Configure identities, SSH credentials, targets and scans | [ServiceNow setup](pilot-quickstart.md) |
| Install the Linux or macOS worker | [Package-manager installation](distribution.md) |
| Understand leases, retry, cancellation and retention | [Managed worker](servicenow-worker.md) |
| Publish observations directly through IRE | [ServiceNow IRE publisher](servicenow.md) |

## Security and release evidence

| Question | Record |
| --- | --- |
| What credential and authorization controls apply? | [Security policy](../SECURITY.md) · [Credential references](credential-references.md) |
| How do I report a vulnerability privately? | [Confidential reporting](../SECURITY.md#report-a-vulnerability) |
| What has been reviewed and independently retested? | [Security review record](security-review.md) |
| What installation and upgrade checks passed? | [ServiceNow package validation](servicenow-validation.md) |
| How do I verify worker artifacts and channel signatures? | [Release verification](releases.md) · [Public-channel evidence](distribution.md#first-beta-operational-evidence) |

Validation records distinguish real-system runs, simulation, source review and
independent retest. Checksums, signatures and attestations have different trust
properties; follow the instructions for the artifact you are installing.

## Components and development

- [Developer quickstart](development.md), [architecture](architecture.md),
  [control-plane design](servicenow-control-plane.md) and [contributing](../CONTRIBUTING.md).
- Host/network discovery: [SSH](ssh-discovery.md), [WinRM](winrm-discovery.md),
  [SNMP](snmp.md) and [VMware](vmware.md).
- Cloud structure: [Kubernetes](kubernetes.md), [AWS Organizations](aws.md) and
  [Azure tenant subscriptions](azure.md).
- [Source resolution](source-resolution.md), [Topo Lab](topo-lab.md),
  [storage/backup](storage.md), [enrollment](enrollment.md) and [Topo Agent](topo-agent.md).
- [Heartbeats](heartbeats.md), [jobs](jobs.md), [scheduling](scheduling.md) and
  [package artifacts](packages.md).
- [Product roadmap](../ROADMAP.md).

## Experimental transports

[Relay](servicenow-relay.md) and [ECC/MID](servicenow-mid.md) are developer
experiments with their own interoperability boundaries. Customer installation
uses the managed worker and supported IRE APIs described above.
