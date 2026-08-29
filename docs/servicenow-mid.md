# Experimental ServiceNow ECC-compatible MID transport

`topo mid run` is a retained interoperability experiment. It uses ServiceNow's
native ECC Queue and direct SOAP table service, but it is not Topo's supported
ServiceNow ingestion path, a customer installation requirement, or a drop-in
replacement for the official MID Server. Topo-owned discovery followed by the
documented IRE API is the product architecture; see
[ServiceNow publishing](servicenow.md).

The precise current claim is **ECC-compatible MID transport**, not a certified
or fully interoperable replacement for every stock MID Server operation. The
public SOAP and ECC contracts define transport and selection fields, but every
ECC `topic` and payload is an operation-specific contract. No additional topic
will be enabled without a new explicit product decision and a documented,
supported interoperability contract.

No third-party MID approval program is asserted as a prerequisite. The blocker
is the technical contract: ECC is a probe/sensor transport, not a general CMDB
ingestion API, and stock Discovery results are meaningful only to matching
instance-side processors. Do not use the stronger “native MID Server” product
claim.

## Why this is not the product ingestion architecture

ServiceNow's public ECC and SOAP descriptions define message transport and
selection fields. They do not define one destination-neutral discovery result
that becomes a CI. Each probe topic and payload is consumed by an
operation-specific Business Rule or sensor. An unmatched `input` record can
remain unprocessed or fail with no matching sensor; merely inserting it into
`ecc_queue` does not publish anything to CMDB.

Stock Discovery also sends dynamic operation content. ServiceNow documents, for
example, that an `SSHCommand` message's `name` is the actual command. Executing
that content would violate Topo's compiled-in-operation invariant. Recreating
the official MID runtime and every version-specific probe, sensor, pattern,
credential, attachment, and pagination contract is not a supported Topo goal.

The experiment's bounded transport shape remains:

```text
Experimental ECC output / ready record
                    |
                    v
       HTTPS POST /ecc_queue.do?SOAP
                    |
             topo mid run
                    |
      fixed topic dispatcher and local policy
                    |
       ecc_queue input / ready response
                    |
      matching native sensor, if one exists
```

The native instance records exercised by the experiment are:

- `ecc_agent`: registered MID identity, host/version/status, and validation
  state;
- `ecc_agent_capability` and `ecc_agent_property`, plus native supported
  application and IP-range relationships: selection and configuration;
- `ecc_queue`: work and results, correlated by `agent`, `topic`, `source`,
  `queue`, `state`, `response_to`, and `agent_correlator`.

Topo never writes a CMDB CI table directly. Production discovery observations
are published to ServiceNow IRE with stable source identity independently of
this experiment, as documented in [ServiceNow publishing](servicenow.md).

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
side effects. No target-bearing translator is planned for this path.

## Implemented experimental topics

| ECC topic | Current behavior | Evidence |
| --- | --- | --- |
| `Heartbeat` | Creates one bounded correlated success result and echoes the bounded ECC `parameters` field as opaque correlation data. It does not force `ecc_agent.status=Up`. | Simulator-only contract. The tested official MID release used `HeartbeatProbe`, not `Heartbeat`; this handler is not stock-compatible evidence. |
| `HeartbeatProbe` | Default-denied as unsupported. | A real official MID request/response was captured on 2026-08-28, but Topo did not implement it after ECC was removed from the product ingestion path. |
| `Command`, `SSHCommand`, PowerShell, JavaScript, Groovy | Always creates a correlated `topo_unsupported_topic` error; never executes or interprets name/payload content. | Deterministic denial tests. |
| Every other topic | Same default-deny result. | Deterministic denial tests. |

The generated simulator-only Heartbeat result follows the public ECC
description: an
`input` response correlated to the output record, with a `<results>` root, one
`<result>`, and one `<parameters>` element. The real reference run established
that it is not the tested release's stock liveness contract because the native
topic was `HeartbeatProbe`.

Topo advertises no `ALL`, generic orchestration, SSH, PowerShell, or discovery
capability in this slice. `ecc_agent_capability` is selection metadata, never
an authorization boundary.

## Experimental instance configuration

Use this only in a disposable development environment to reproduce transport
research:

1. Enable/license the ServiceNow Discovery/ITOM features the deployment needs.
2. Create a dedicated non-interactive user for this MID identity and assign the
   built-in `mid_server` role. Do not grant `admin`, a Topo-specific role, or a
   custom application role. Confirm that the release's native ACLs permit the
   required `ecc_queue` SOAP query/insert/update operations.
3. Establish the standard `ecc_agent` record for the exact configured name,
   then validate it through **MID Server > Servers > Validate**. Automatic
   creation was observed for the official MID, but Topo's own bootstrap
   equivalence remains unverified; the experiment does not repeatedly write
   host/version/status fields.
4. Do not assign applications, capabilities, or IP ranges and do not select
   `ALL`; no target-bearing native operation is supported.
5. Do not point a native Discovery Schedule at Topo. Standard Discovery status
   and sensor completion are not claimed.
6. Observe the experiment only through the MID Server list and ECC Queue.

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
  -state-dir /var/lib/topo/mid \
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

## No target authority

This experiment has no target-capable operation, so an ECC `source`,
native IP-range assignment, or capability cannot make Topo connect to anything.
No Linux SSH, WinRM, SNMP, VMware, cloud, Kubernetes, pattern, or orchestration
translator is planned for this path. If a future documented interoperability
contract causes that decision to be revisited, every requested target must
still intersect an owner-controlled local allowlist; ServiceNow selection
metadata can never grant Topo network or operation authority.

## Evidence status

Deterministic CI uses `internal/mid/eccsim`, an HTTPS handler for the exact
direct SOAP operations above. Tests prove Basic authentication, fixed path and
query, exact agent isolation, redirect refusal, response/XML bounds, SOAP
faults, ready-to-processing-to-processed transitions, crash recovery without
duplicate response insertion, local duplicate-process exclusion, Heartbeat
dispatch, and visible denial of executable/unknown topics.

The simulator is not a ServiceNow instance. A separate sanitized official-MID
reference run on 2026-08-28, using the official Australia Patch 3 Linux
container and a dedicated user with the built-in `mid_server` role, established:

- the official MID automatically created its `ecc_agent` record and appeared
  `Up` in the standard MID Server list;
- native validation completed with `Validated: Yes` while initial application,
  capability, and IP-range configuration was skipped;
- the instance created an `output`/`processed` `HeartbeatProbe`, and the
  official MID returned a correlated `input`/`processed` `HeartbeatProbe` whose
  `response_to` referenced the output record;
- the response used a `<results probe_time="..." result_code="0">` root,
  runtime queue/memory/thread fields, echoed request parameters, and
  `mid_operational_state=Up`; and
- startup/validation also exercised topics including `SystemCommand`,
  `config.file`, `queue.processing`, and `queue.stats`, demonstrating that
  registration and validation are not a one-topic generic result contract.

That reference run did not prove Topo compatibility: Topo recognizes
`Heartbeat`, while the real release sent `HeartbeatProbe`. Topo itself was not
registered or validated as an official-MID-equivalent implementation, and the
official container was not stopped long enough to record a ServiceNow-derived
Down transition. The following remain unverified and are no longer scheduled
as product gates:

- ServiceNow Up/Down transitions derived from Topo responses;
- credential exchange or decryption contracts;
- attachments, pagination beyond direct SOAP record paging, or multipage
  pattern results;
- Discovery Status/sensor expectations for any stock discovery operation.

Record each point only from a sanitized real instance/reference run. Keep real
evidence separate from simulator evidence.

## Predecessor scoped-app Relay

The custom scoped-application transport from PR #47 also remains in the
repository as an experiment. Neither experiment is the required product
architecture, and direct IRE publication requires neither one. See
[experimental scoped-app Relay](servicenow-relay.md) for its separate contract
and evidence history.
