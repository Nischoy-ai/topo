# One-time administrator preparation

Complete this checklist after importing the Topo application and before
connecting the discovery worker. This is separate from the operator’s three
actions: import XML, connect the server, then choose **Run now**.
For an existing prepared installation, verify these settings rather than
creating duplicate identities or records.

## Access and approved target

Arrange these with your ServiceNow administrator and target owner:

- An authorized development instance with custom-table/application entitlement
  and CMDB/IRE available. Keep schedules inactive and workers stopped during setup.
- A Linux worker with outbound HTTPS to ServiceNow and SSH/22 access to one
  approved Linux IPv4 address. No inbound worker listener is needed.
- A non-admin SSH account/password and a host key verified through an independent
  trusted source. `ssh-keyscan` alone does not verify identity.
- Separate app administrator, operator and credential custodian access, plus a
  dedicated worker integration user and restricted OAuth client.

Examples use `192.0.2.10/32`, `pilot-linux` and `pilot-site`. Replace the example
address, instance and paths with approved values. Never paste passwords or tokens
into commands, labels or logs.

## ServiceNow identities and records

Use the [identity and seven-resource OAuth checklist](pilot-quickstart.md#2-create-the-least-privilege-servicenow-identities).
Humans receive their respective `admin`, `operator`, `viewer` or
`credential_admin` roles under `x_664635_topo`. The machine user receives only
`x_664635_topo.worker`; its OAuth policy allows only the seven listed POST
resources, with no wildcards, generic Table API, IRE or credential-table access.
Transfer its token directly into a restricted worker file through your secure
OAuth workflow. The credential custodian enters the target password in
**Remote Credentials**, using the protected Password2 field.

In **Nischoy Topo**, create these active records in order. Give each a descriptive
**Name**; fields not listed retain their defaults:

| Record | First-run values |
| --- | --- |
| **Worker Pools** | Pool `pilot-linux`, site `pilot-site`, dedicated worker user; lease seconds `120`, maximum task seconds `300`, maximum leases `2`. |
| **Target Scopes** | ID `pilot-linux-targets`, revision `1`, same pool/site; approved target `192.0.2.10/32`, IPv4 partition prefix `32`. |
| **Remote Credentials** | ID `pilot-linux-password`, non-admin SSH username and protected password. |
| **Credential Bindings** | ID `pilot-linux-binding`, revision `1`, protocol **SSH password** (`ssh_password`); credential above and the revision-`1` scope record, allowed profile `pilot-linux`, profile revision `1`. |
| **Discovery Profiles** | ID `pilot-linux`, revision `1`, operation `ssh_linux.v1`, schema `v1alpha1`; matching pool, scope and binding. |

IDs/revisions form the authorization contract. For later changes, use new
revisions and matching bindings; see [scope and binding rules](mixed-inventory-setup.md#3-create-one-profile-per-protocol).

## Handoff to the discovery-server operator

Provide the instance HTTPS origin, pool and site IDs, approved target allowlist
and independently verified SSH host keys. Deliver the restricted worker OAuth
token directly to an owner-only file on the server through your secure process;
never place its value in chat, commands, labels or logs. The operator needs
file paths and a protected token reference, not the target password. Keep
schedules inactive until a manual run and repeat have been inspected.

Continue with [installing, checking and starting the worker](first-success.md#2-install-check-and-start-the-worker).
