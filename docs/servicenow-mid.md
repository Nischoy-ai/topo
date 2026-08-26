# Native ServiceNow ECC-compatible MID transport

`topo mid run` is Topo's required ServiceNow control-plane direction. It uses
ServiceNow's native ECC Queue and direct SOAP table service; it does not require
a Topo scoped application, update set, custom table, Scripted REST API,
Business Rule, UI action, or scheduled script on the instance.

The precise current claim is **ECC-compatible MID transport**, not a certified
or fully interoperable replacement for every stock MID Server operation. The
public SOAP and ECC contracts define transport and selection fields, but every
ECC `topic` and payload is an operation-specific contract. Topo enables a topic
only after it has been observed in a sanitized official MID reference run and
mapped to a compiled-in, reviewed Topo operation.

No third-party MID approval program is treated as a prerequisite. Progress is
gated by observed interoperability and security evidence, not by an assumed
certification requirement; use the stronger “native MID Server” product claim
only if later evidence supports it.

## Native architecture

ServiceNow remains the intended control panel:

```text
Native Discovery Schedule / MID selector
                    |
                    v
       ecc_queue output / ready record
                    |
       HTTPS POST /ecc_queue.do?SOAP
                    |
             topo mid run
                    |
      fixed topic dispatcher and local policy
                    |
       ecc_queue input / ready response
                    |
      native sensor / Discovery Status / IRE
```

The native instance records involved in the final architecture are:

- `ecc_agent`: registered MID identity, host/version/status, and validation
  state;
- `ecc_agent_capability` and `ecc_agent_property`, plus native supported
  application and IP-range relationships: selection and configuration;
- `ecc_queue`: work and results, correlated by `agent`, `topic`, `source`,
  `queue`, `state`, `response_to`, and `agent_correlator`.

Topo never writes a CMDB CI table directly. When a later stock discovery
translator produces destination-neutral Topo observations, CI publication
continues through ServiceNow IRE with stable source identity, as documented in
[ServiceNow publishing](servicenow.md).

## SOAP and ECC boundary

The client uses only ServiceNow's documented direct document/literal SOAP
service at the fixed endpoint `/ecc_queue.do?SOAP`:

- `getRecords` polls at most the configured bound (default one, maximum 16)
  with the exact filter
  `agent=mid.server.<name>^queue=output^state=ready`, ordered by creation and
  `sys_id`;
- `update` transitions a selected output record to `processing`, then to
  `processed` only after a response exists;
- `insert` creates one correlated `input`/`ready` result with the original
  `response_to`, `topic`, `agent`, `source`, `parameters`, and
  `agent_correlator`.

Every request is cancellable and limited to 30 seconds. Responses are limited
to 2 MiB, XML nesting to 64 elements, XML tokens to 200,000, payloads to 1 MiB,
parameters to 256 KiB, and error text to 4 KiB. XML directives are rejected.
SOAP faults are bounded and returned as structured errors. The HTTP client
refuses redirects even when a caller supplies a custom transport, so a Basic
credential cannot be forwarded to another origin.

The configured instance value must be an absolute HTTPS origin. URL userinfo,
paths, queries, and fragments are rejected; the CLI appends the one fixed SOAP
path itself. There is no JSON Table API fallback.

## Claim, crash, and duplicate behavior

Before the first server-side state transition, Topo appends the output
`sys_id` and a SHA-256 digest of its operation-bearing fields to an owner-only
local journal and synchronizes it. An OS file lock, scoped to the configured
MID name and state directory, makes a second local process fail at startup.
The OS releases that lock after a crash, while the journal remains.

On restart Topo re-reads the journaled ECC record. A `ready` record is claimed;
a `processing` record is resumed; a `processed` record only needs its completed
journal removed. The result is journaled before `insert`. Before inserting,
Topo queries for an existing `input` record with the same `response_to`; this
closes the crash window where ServiceNow accepted the insert but the response
was lost locally. More than one existing response is not hidden: Topo leaves
the output/journal visible for operator investigation.

ServiceNow's documented direct SOAP `update` operation updates by `sys_id`; it
does not expose a compare-and-swap condition. The local lock therefore prevents
two processes sharing the same host/state directory from executing one item,
but the public transport alone cannot prove atomic exclusion between two hosts
misconfigured with the same MID name. Native configuration must keep MID names
unique. This slice also executes no target-bearing operation, so that remaining
cross-host claim question cannot cause duplicate SSH/PowerShell/SNMP or other
side effects. A real official-MID capture must establish the stronger native
claim behavior before any such translator is enabled.

## Supported topics

| ECC topic | Current behavior | Evidence |
| --- | --- | --- |
| `Heartbeat` | Creates one bounded correlated success result and echoes the bounded ECC `parameters` field as opaque correlation data. It does not force `ecc_agent.status=Up`. | SOAP/state/correlation behavior is simulator-tested. The exact stock Heartbeat XML and ServiceNow Up/Down interpretation remain unverified against a real instance. |
| `Command`, `SSHCommand`, PowerShell, JavaScript, Groovy | Always creates a correlated `topo_unsupported_topic` error; never executes or interprets name/payload content. | Deterministic denial tests. |
| Every other topic | Same default-deny result. | Deterministic denial tests. |

The generated Heartbeat result follows the public ECC description: an
`input` response correlated to the output record, with a `<results>` root, one
`<result>`, and one `<parameters>` element. Its exact compatibility with the
stock Heartbeat sensor is not claimed until it is compared with a sanitized
official MID response on the target ServiceNow release.

Topo advertises no `ALL`, generic orchestration, SSH, PowerShell, or discovery
capability in this slice. `ecc_agent_capability` is selection metadata, never
an authorization boundary.

## Native instance configuration

Use only ordinary ServiceNow administration:

1. Enable/license the ServiceNow Discovery/ITOM features the deployment needs.
2. Create a dedicated non-interactive user for this MID identity and assign the
   built-in `mid_server` role. Do not grant `admin`, a Topo-specific role, or a
   custom application role. Confirm that the release's native ACLs permit the
   required `ecc_queue` SOAP query/insert/update operations.
3. Establish the standard `ecc_agent` record for the exact configured name,
   then validate it through **MID Server > Servers > Validate**. The precise
   bootstrap messages that create/populate this record have not yet been
   captured, so this first slice does not fabricate them or repeatedly write
   host/version/status fields.
4. Assign only the native supported applications, exact capabilities, and IP
   ranges Topo has actually implemented. Do not select `ALL`.
5. Configure native Discovery credentials and schedules only after the exact
   corresponding stock ECC transaction has a reviewed Topo translator. The
   first planned transaction is one sanitized Linux SSH Discovery Schedule run
   captured from a temporary official MID reference installation.
6. Observe work and results through the standard MID Server list, Discovery
   Schedule MID selector, Discovery Status, and ECC Queue.

Official starting references:

- [MID Server ECC Queue](https://www.servicenow.com/docs/r/servicenow-platform/mid-server/ecc-queue-mid-server.html)
- [SOAP web service](https://www.servicenow.com/docs/r/api-reference/web-services/c_SOAPWebService.html)
- [MID Server selection](https://www.servicenow.com/docs/r/servicenow-platform/mid-server/c_MIDServerSelector.html)
- [Validate the MID Server](https://www.servicenow.com/docs/r/servicenow-platform/mid-server/t_ValidateAMIDServer.html)
- [MID Server heartbeat](https://www.servicenow.com/docs/r/servicenow-platform/mid-server/r_MIDServerHeartbeat.html)

## Run Topo on the MID host

The username is non-secret configuration. The password is always resolved
through Topo's shared `env:`, `file:`, `vault:`, or `k8s:` credential-reference
contract and never accepted as a CLI value:

```sh
topo mid run \
  -servicenow-instance https://example.service-now.com \
  -name topo-pilot \
  -username topo.mid \
  -password-ref file:/etc/topo/servicenow-mid-password \
  -state-dir /var/lib/topo-mid \
  -poll-interval 40s
```

The resulting ECC agent identity is exactly `mid.server.topo-pilot`. The state
directory must be absolute, owner-controlled, and shared by every attempted
local process for that identity. Topo creates/tightens it to POSIX mode `0700`
and the lock/journal files to `0600`; on Windows, apply an equivalent NTFS ACL
for the Topo service identity.

The password is loaded once at startup. Replace the referenced secret and
restart after rotation. Do not place passwords, OAuth tokens, spool keys, SSH
keys, or native Discovery credentials in ECC payloads, properties, logs,
labels, command lines, or observation attributes.

## Local target authorization

This Heartbeat-only slice has no target-capable operation, so an ECC `source`,
native IP-range assignment, or capability cannot make Topo connect to anything.
Before the first Linux SSH translator is enabled, the Topo host must have a
separate owner-controlled target allowlist. Every requested host or IP range
must intersect that local allowlist; the effective target set is the
intersection, never the ServiceNow range alone. The translator must reject an
empty or ambiguous intersection and continue to enforce the existing SSH host
key, command, deadline, output, and credential-reference boundaries.

The same rule applies independently to future WinRM, SNMP, VMware, cloud, and
Kubernetes translators. A capability record says what ServiceNow may select;
it does not grant Topo network or operation authority.

## Evidence status

Deterministic CI uses `internal/mid/eccsim`, an HTTPS handler for the exact
direct SOAP operations above. Tests prove Basic authentication, fixed path and
query, exact agent isolation, redirect refusal, response/XML bounds, SOAP
faults, ready-to-processing-to-processed transitions, crash recovery without
duplicate response insertion, local duplicate-process exclusion, Heartbeat
dispatch, and visible denial of executable/unknown topics.

The simulator is not a ServiceNow instance and does not prove any of these
real-system points yet:

- automatic `ecc_agent` registration and required bootstrap fields;
- standard MID list appearance and validation completion;
- the exact stock Heartbeat request/result XML for the target release;
- ServiceNow's Up/Down transitions and events from Topo responses;
- credential exchange or decryption contracts;
- attachments, pagination beyond direct SOAP record paging, or multipage
  pattern results;
- Discovery Status/sensor expectations for any stock discovery operation.

Record each point only from a sanitized real instance/reference run. Keep real
evidence separate from simulator evidence.

## Predecessor scoped-app Relay

The custom scoped-application transport from PR #47 remains in the repository
as an experimental predecessor while native ECC behavior is being proven. It
is not the required final product architecture, and new deployments should not
interpret its custom tables or Scripted REST resources as prerequisites for
`topo mid run`. See [predecessor scoped-app Relay](servicenow-relay.md) for its
separate contract and evidence history.
