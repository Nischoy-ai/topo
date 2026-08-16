# Nischoy Topo

Nischoy Topo is an open-source, destination-neutral discovery data plane for hybrid IT. It collects evidence about infrastructure, normalizes assets and relationships into a stable schema, and publishes them to ServiceNow or another CMDB without making the destination the discovery engine.

Topo is an independent public product repository under the Nischoy organization. It does not depend on Nischoy's private website or commercial source repositories.

This repository is the first working vertical slice of the project plan. It currently includes local and Linux SSH host discovery; Windows WinRM discovery for audited CIM identity, hardware, OS, network, volume, service, and patch collection plus machine-wide uninstall-registry software inventory; HTTPS-only NTLMv2 authentication for Windows pilots; an authenticated controller ingestion API; in-memory identity resolution; JSON Lines and HTTPS webhook publishers; and ServiceNow IRE payload generation. WinRM compatibility fixtures and mixed-estate acceptance, Kerberos and certificate authentication, SNMP, VMware, cloud, Kubernetes, persistent PostgreSQL storage, enrollment/mTLS, and fleet scheduling remain subsequent work rather than being represented as complete.

It also includes **Topo Lab**, a deterministic estate simulator for exercising discovery concurrency, failures, identity resolution, and CMDB mappings without provisioning hundreds of real machines.

## Quick start

Requires Go 1.23 or later.

```sh
make test
make build
./bin/topo discover local
./bin/topo discover -format servicenow-preview local
```

Run a clean, repeatable 500-host simulation:

```sh
./bin/topo lab serve -scenario examples/lab/clean-500.json
# In another terminal:
./bin/topo lab run -scenario examples/lab/clean-500.json -repeat 2 -min-coverage 100
```

See [Topo Lab](docs/topo-lab.md) for personas, fault injection, expected graphs, and limitations.

Exercise 500 Linux targets through real SSH handshakes and sessions without provisioning VMs:

```sh
./bin/topo lab ssh-serve -scenario examples/lab/clean-500.json
# In another terminal:
./bin/topo lab ssh-targets -scenario examples/lab/clean-500.json > targets.txt
TOPO_SSH_PASSWORD=topo-lab ./bin/topo discover ssh \
  -targets targets.txt -site lab -insecure-host-key > observation.jsonl
```

The insecure host-key option is intentionally restricted to an explicit flag for Topo Lab. Real targets should use `-known-hosts`. See [Linux SSH discovery](docs/ssh-discovery.md).

Exercise Windows personas through real WS-Management SOAP exchanges on an isolated loopback endpoint:

```sh
./bin/topo lab winrm-serve -scenario examples/lab/clean-500.json
# In another terminal:
./bin/topo lab winrm-targets -scenario examples/lab/clean-500.json > winrm-targets.txt
TOPO_WINRM_PASSWORD=topo-lab ./bin/topo discover winrm \
  -targets winrm-targets.txt -site lab -lab-basic > windows-observation.jsonl
```

Basic authentication and HTTP are accepted only with the explicit Lab flag and loopback targets. Production NTLMv2 targets require HTTPS, verified certificates, and `-auth ntlm`; Kerberos is not yet implemented. See [Windows WinRM discovery](docs/winrm-discovery.md).

Start the controller with authentication enabled:

```sh
TOPO_API_KEY='replace-with-a-long-random-value' ./bin/topo serve
curl http://localhost:8080/healthz
```

Submit an observation produced by the CLI:

```sh
./bin/topo discover local > observation.jsonl
curl -H 'Authorization: Bearer replace-with-a-long-random-value' \
  -H 'Content-Type: application/json' \
  --data-binary @observation.jsonl http://localhost:8080/v1/observations
```

## Architecture

The canonical `ObservationEnvelope` separates immutable source observations from resolved assets. Each asset has a source-native identity, optional strong identifiers, attributes, and evidence. Relationships refer to native identities within the observation. IP addresses remain mutable attributes and do not determine identity.

The public extension points are small Go interfaces:

- `discovery.Plugin`: capability description, configuration validation, connectivity check, and discovery.
- `publisher.Publisher`: destination validation, preview, and batch publication.
- `store.Repository`: immutable observation storage and resolved-asset reads.

The JSON Schema and Protobuf definitions under `api/` are the cross-process contract. They are currently `v1alpha1`; breaking changes are allowed until `v1` but must increment the schema version.

The product family uses these names as capabilities are delivered:

- **Topo Relay** — agentless discovery collector deployed in a network segment.
- **Topo Agent** — outbound-only endpoint discovery agent.
- **Topo Hub** — self-hosted controller and local asset view.
- **Topo Connect** — ServiceNow and other CMDB publishers.
- **Topo Graph** — the future full CMDB product.

## ServiceNow behavior

Nischoy Topo maps assets to ServiceNow CI classes and supplies `sys_object_source_info` for stable source identity. Publishing uses `/api/now/identifyreconcile/enhanced`; it does not write CMDB tables directly. Preview mode produces the complete proposed payload without network access. A production deployment must configure an `Nischoy Topo` discovery source, reconciliation rules, and explicit canonical-attribute mappings in ServiceNow before enabling writes; Nischoy Topo does not invent custom `cmdb_ci` fields for its internal identifiers.

## Security posture

- The controller can require a bearer API key and caps request bodies at 10 MiB.
- Destination URLs must use HTTPS; client timeouts and bounded response reads are mandatory.
- The local plugin needs no privileged account and executes no shell commands.
- The SSH plugin executes a fixed audited command set, requires host-key verification by default, bounds command output, and applies connection and command deadlines.
- The WinRM plugin executes fixed CIM resource/query pairs for required host identity and optional network, volume, service, and patch inventory plus one compiled-in PowerShell command for machine-wide uninstall-registry software inventory; it requires HTTPS outside loopback-only Lab mode, verifies server certificates, performs NTLMv2 without Basic fallback, bounds SOAP and command output, and applies operation deadlines and concurrency limits.
- The container runs as a non-root user with a read-only filesystem and no Linux capabilities.
- Secrets are read from the environment and never serialized into observations.

The current API-key transport is suitable for local evaluation only. Do not expose the controller to an untrusted network until collector enrollment, mTLS, certificate rotation, audit logging, and a persistent secret provider are implemented. See [SECURITY.md](SECURITY.md).

## Project status

Nischoy Topo is pre-alpha. The implementation order and acceptance gates are in [ROADMAP.md](ROADMAP.md). Contributions should follow [CONTRIBUTING.md](CONTRIBUTING.md). Licensed under Apache 2.0.

For durable project context across coding sessions, start with [the project plan and current handoff](docs/project-plan.md). Coding agents must also follow [AGENTS.md](AGENTS.md); those files are maintained as repository state so progress does not depend on chat history.
