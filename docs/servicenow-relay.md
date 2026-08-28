# Experimental scoped-app ServiceNow-controlled Topo Relay

This document records the experimental control-plane prototype implemented in
PR #47. It is retained as control-plane and encrypted-spool evidence, but it is
not the required ServiceNow architecture. Installing Topo for direct IRE
publication does not require this scoped application, its custom tables,
Scripted REST resources, Business Rule, UI action, or scheduled script. The
separate ECC experiment is documented in
[Experimental ServiceNow ECC-compatible MID transport](servicenow-mid.md).

The experimental Relay runs inside the network, makes outbound HTTPS requests to
custom ServiceNow resources, claims a job created from a custom form or
schedule, performs a compiled-in discovery operation, publishes the
observation through IRE, and records terminal job status in a custom table.

```text
ServiceNow profile / schedule / job
                 |
                 | outbound poll from the network
                 v
        topo relay run
                 |
        local profile lookup
                 |
       SSH or local discovery
                 |
       ServiceNow IRE + job result
```

This experiment is not a drop-in replacement in the stock ServiceNow
Discovery Schedule MID Server selector. Its custom Topo tables and **Start
discovery** UI action are optional experimental controls, not installation
requirements for the product. Topo does not promise native `ecc_agent`,
Discovery Schedule, Discovery Status, or stock sensor compatibility.

## Security boundary

A ServiceNow job contains exactly three authority-bearing values: a job ID,
the fixed type `discover`, and a profile ID. It cannot supply targets, IP
ranges, usernames, credential references, plugin options, OIDs, WQL,
PowerShell, shell commands, or executable content. The owner-controlled Relay
configuration maps that profile ID to a compiled-in plugin, an absolute local
targets file, server-identity policy, bounded concurrency/deadlines, and secret
references. Editing a ServiceNow job therefore cannot expand what the Relay
host administrator already authorized.

One ServiceNow integration user is bound to one Relay record through
`u_service_user`. Both API resources query by the authenticated
`gs.getUserID()` as well as `relay_id`; a token belonging to one Relay cannot
claim or complete another Relay's jobs by changing the JSON body. Run only one
process for a given Relay ID in this MVP. ServiceNow's scheduled-script engine
is the single producer for recurring jobs, and it refuses to create another
job while the profile already has one queued or running.

Completed observations/results are AES-256-GCM encrypted in a bounded local
spool before the first IRE attempt. The Relay retries IRE three times across
poll cycles and retains the entry until ServiceNow acknowledges the terminal
job result. A stopped process can resume those deliveries with the same spool
directory and key. Tampering fails integrity verification rather than
returning modified discovery data.

## Supported profiles

The first Relay slice supports:

- `local`: inventory the Relay host with no target or credential settings.
- `ssh-linux`: discover an explicit list of `username@host:port` targets using
  the existing fixed SSH command set. A managed `known_hosts` file and either a
  password or private-key credential reference are required. There is no
  insecure-host-key option in Relay mode.

WinRM, SNMP, VMware, Kubernetes, AWS, and Azure already have standalone Topo
discovery plugins but are not yet Relay profile types. Adding each requires a
focused profile/configuration slice; ServiceNow never gets to pass the plugin's
raw configuration.

## Historical experimental application setup

The source definitions are retained under
`integrations/servicenow/topo-relay/`. `application.json` is the authoritative
field/resource manifest and the `scripts/` directory contains the exact server
scripts. They are reviewable source artifacts, not yet a signed ServiceNow
Store application or a generated update-set XML file. They are not needed for
direct IRE publication. To reproduce PR #47's experiment in a disposable
sub-production instance only:

1. Create a scoped application named **Nischoy Topo** with scope
   `x_nischoy_topo`.
2. Create roles `x_nischoy_topo.relay` and
   `x_nischoy_topo.operator`. The dedicated integration user gets only the
   Relay role plus the minimum roles your instance requires to call the IRE
   enhanced API; interactive operators get the operator role, not the Relay
   user's OAuth credential.
3. Create the four tables and fields exactly as listed in
   `application.json`. Add the listed unique indexes. Give Relay-role users no
   direct table UI/API CRUD access; the two Scripted REST resources are their
   narrow interface. Give operator-role users the intended form/list access to
   Relay, Profile, Schedule, and Job records.
4. Create a Scripted REST API named **Topo Relay API**, API ID `v1`. ServiceNow
   documents Scripted REST resources as the place to define an HTTP method,
   relative resource path, processing script, and per-resource security. Add
   `POST /relay/poll` from `scripts/poll.js` and
   `POST /relay/result` from `scripts/result.js`, requiring
   `x_nischoy_topo.relay` on both. See ServiceNow's official
   [Scripted REST API resource procedure](https://www.servicenow.com/docs/r/api-reference/rest-api-explorer/t_CreateAScriptedRESTAPIResource.html).
5. Add a Scheduled Script Execution that runs every minute using
   `scripts/enqueue_due_schedules.js`. Scheduled jobs are ServiceNow's native
   mechanism for recurring server-side work; see the official
   [Scheduled Jobs documentation](https://www.servicenow.com/docs/r/platform-administration/time-configuration/c_ScheduledJobs.html).
6. Add a form UI action named **Start discovery** to
   `x_nischoy_topo_profile`, server-side only, using
   `scripts/start_discovery.js` and requiring the operator role.
7. Create a non-interactive user for this Relay, grant its narrow roles, issue
   an OAuth bearer token, and create an active Topo Relay record whose
   `u_service_user`, `u_relay_id`, and `u_site_id` match the local config.
8. Register `Nischoy Topo` as a valid `cmdb_ci.discovery_source` choice and
   verify identification/reconciliation rules for every CI class the selected
   profile can emit. See [ServiceNow publishing](servicenow.md).

After this has been installed and verified in the developer instance, export
the scoped application through ServiceNow source control or an update set.
That real exported artifact—not hand-authored metadata XML—should become the
repeatable installation input for later instances.

## Configure the experimental Relay host

Start from `examples/servicenow/relay-config.json`. The config path, targets
file, `known_hosts`, credential files, and spool path must be absolute. The
config stores references such as `file:/...` or `vault:...`, never secret
values.

Example SSH targets file:

```text
# /etc/topo/targets/linux-pilot.txt
topo-reader@server-01.example.net:22
topo-reader@server-02.example.net:22
```

Generate a Relay spool key and store the ServiceNow token in owner-readable
files (a managed Vault or Kubernetes Secret reference is preferred on a
server):

```sh
sudo install -d -m 0700 /etc/topo /var/lib/topo-relay/spool
openssl rand -hex 32 | sudo tee /etc/topo/relay-spool.key >/dev/null
sudo chmod 0600 /etc/topo/relay-spool.key /etc/topo/servicenow-token
```

Run the Relay:

```sh
topo relay run \
  -servicenow-instance https://example.service-now.com \
  -token-ref file:/etc/topo/servicenow-token \
  -config /etc/topo/relay.json \
  -spool-dir /var/lib/topo-relay/spool \
  -spool-key-ref file:/etc/topo/relay-spool.key \
  -poll-interval 1m
```

The token is resolved at startup and never appears in the process arguments or
logs. Restart the Relay after rotating or renewing it. Automatic OAuth token
refresh is a follow-up; use an access token whose lifetime matches the pilot or
restart with a newly written token file before expiry.

## Start and inspect experimental discovery

Create one ServiceNow Topo Profile for each local profile ID, assigned to the
matching Relay record. The ServiceNow `u_plugin` is display/audit metadata; the
Relay trusts only its local profile definition.

- Open a profile and choose **Start discovery** for an immediate run.
- Create an active Topo Schedule with a bounded interval and next-run time for
  recurring execution.
- Watch the Topo Job record move through `queued`, `running`, and
  `completed`/`failed`. It records observation ID, asset/relationship/error
  counts, timestamps, and a bounded error message.
- View the actual discovered CIs and relationships in CMDB, filtered by
  `discovery_source = Nischoy Topo`. Topo writes them only through
  `/api/now/identifyreconcile/enhanced`, never directly through the Table API.

## Evidence status

Automated tests exercise this experiment's outbound HTTP contract, bearer header and
fixed paths, redirect refusal, response bounds, job-schema injection rejection,
encrypted-spool retry, local discovery, and a complete real-SSH Topo Lab flow
through a simulated ServiceNow poll/IRE/result server. The scoped application
scripts and table definitions have not yet been imported into a real
ServiceNow instance. That experiment is no longer on the required validation
path. Product evidence should exercise Topo's documented IRE publication,
stable source identity, class/relationship mappings, and repeated discovery
against a real instance. Do not claim this experiment as a supported
production MID replacement or required ServiceNow integration.
