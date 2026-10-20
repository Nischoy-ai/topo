# Topo documentation

Start with [your first ServiceNow discovery run](first-success.md)
for ServiceNow-managed discovery. Use the guides below for configuration,
verification and component details.

## Install and operate

| Task | Guide |
| --- | --- |
| Download or build the worker and ServiceNow app | [Get Topo](get-topo.md) |
| Install the ServiceNow application and indexes | [XML installation and recovery](servicenow-update-set.md) · [Index checklist](servicenow-index-setup.md) |
| Configure identities, protocol credentials, targets and scans | [Linux/Windows setup](mixed-inventory-setup.md) · [SSH-only setup](pilot-quickstart.md) |
| Install the Linux or macOS worker | [Package-manager installation](distribution.md) |
| Understand leases, retry, cancellation and retention | [Managed worker](servicenow-worker.md) |
| Publish observations directly through IRE | [ServiceNow IRE publisher](servicenow.md) |

## Security and release evidence

Dated test history lives in the [evidence index](evidence/README.md).

| Question | Record |
| --- | --- |
| Where do passwords live and which storage controls apply? | [Deployment security](deployment-security.md) |
| What credential and authorization controls apply? | [Security policy](../SECURITY.md) · [Credential references](credential-references.md) |
| How do I report a vulnerability privately? | [Confidential reporting](../SECURITY.md#report-a-vulnerability) |
| What has been reviewed and independently retested? | [Security review record](security-review.md) |
| What installation and upgrade checks passed? | [ServiceNow package validation](servicenow-validation.md) |
| How do I verify worker artifacts and channel signatures? | [Release verification](releases.md) · [Public-channel evidence](evidence/distribution.md) |

Validation records distinguish real-system runs, simulation, source review and
independent retest. Checksums, signatures and attestations have different trust
properties; follow the instructions for the artifact you are installing.

<a id="managed-beta-scope"></a>

## Managed discovery scope

The published 0.4.7 release covers bounded Linux/Windows computer and interface
inventory. Review the [scope and acceptance record](mixed-inventory-pilot.md)
and [dated evidence](evidence/mixed-inventory.md) before expanding targets.

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
