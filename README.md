# Nischoy Topo

Nischoy Topo is an open-source, destination-neutral discovery data plane for hybrid IT. It collects evidence about infrastructure, normalizes assets and relationships into a stable schema, and publishes them to ServiceNow or another CMDB without making the destination the discovery engine.

Topo is an independent public product repository under the Nischoy organization. It does not depend on Nischoy's private website or commercial source repositories.

This repository is the first working vertical slice of the project plan. It currently includes local and Linux SSH host discovery; Windows WinRM discovery for audited CIM identity, hardware, OS, network, volume, service, and patch collection plus machine-wide uninstall-registry software inventory; HTTPS-only NTLMv2 authentication for Windows pilots; concurrent two-scan acceptance for 500 Linux and 500 Windows targets; bounded `env:`/`file:`/`vault:`/`k8s:` credential references; an authenticated controller ingestion API with an opt-in certificate authority for collector enrollment and opt-in native outbound mTLS; an outbound-only Topo Agent MVP with encrypted offline buffering, a hardened Linux systemd unit, and Windows Service Control Manager integration; in-memory identity resolution; JSON Lines and HTTPS webhook publishers; and a ServiceNow IRE publisher whose outbound payload is validated duplicate-free and idempotent across repeated Topo Lab scans. WinRM real-host compatibility fixtures, Kerberos and certificate authentication, real-Windows verification of the agent's service wrapper, validation of ServiceNow's own reconciliation behavior against a real instance, certificate rotation/heartbeats/job delivery, SNMP, VMware, cloud and Kubernetes discovery, persistent PostgreSQL storage, and fleet scheduling remain subsequent work rather than being represented as complete.

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
# Equivalent explicit reference:
./bin/topo serve -api-key-ref env:TOPO_API_KEY
curl http://localhost:8080/healthz
```

Controller, SSH, WinRM, and Topo Agent credentials share the same
`env:<name>`, `file:<absolute-path>`, `vault:<path>#<field>`, and
`k8s:[<namespace>/]<secret-name>#<field>` reference contract. Values never
appear in CLI arguments. See [Credential references](docs/credential-references.md).

Run the outbound-only Topo Agent against the controller started above, self-reporting on an interval and buffering to an encrypted local spool if the controller is unreachable:

```sh
TOPO_AGENT_SPOOL_KEY=$(openssl rand -hex 32) \
TOPO_AGENT_API_KEY='replace-with-a-long-random-value' \
./bin/topo agent run \
  -controller-url http://localhost:8080 \
  -spool-dir /var/lib/topo-agent/spool \
  -interval 15m
```

See [Topo Agent](docs/topo-agent.md) for the spool encryption, delivery
retry semantics, and current limitations.

Enroll a collector with its own certificate instead of sharing the
controller's bearer API key:

```sh
./bin/topo serve -api-key-ref env:TOPO_API_KEY -ca-dir /var/lib/topo-hub/ca
curl -s -X POST -H "Authorization: Bearer $TOPO_API_KEY" http://localhost:8080/v1/enrollment-tokens
TOPO_AGENT_ENROLLMENT_TOKEN='<token from above>' ./bin/topo agent enroll \
  -controller-url http://localhost:8080 -cert-dir /etc/topo-agent/enrollment
```

See [Collector enrollment](docs/enrollment.md). The issued certificate now
authenticates live traffic too: run the controller with `-mtls` and the
agent with `-mtls-cert-dir` to use it instead of, or alongside, the bearer
API key — see [Running as native mTLS](docs/enrollment.md#running-as-native-mtls).

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

Nischoy Topo maps assets to ServiceNow CI classes and supplies `sys_object_source_info` for stable source identity. Publishing uses `/api/now/identifyreconcile/enhanced`; it does not write CMDB tables directly. Preview mode produces the complete proposed payload without network access. The outbound payload is proven duplicate-free and idempotent across repeated scans (deduplicated within a batch, and validated identical across independent Topo Lab discovery runs); ServiceNow's own identification and reconciliation behavior is not — there is no ServiceNow instance available to validate against. See [ServiceNow publishing](docs/servicenow.md). A production deployment must configure an `Nischoy Topo` discovery source, reconciliation rules, and explicit canonical-attribute mappings in ServiceNow before enabling writes; Nischoy Topo does not invent custom `cmdb_ci` fields for its internal identifiers.

## Security posture

- The controller can require a bearer API key and caps request bodies at 10 MiB.
- Destination URLs must use HTTPS; client timeouts and bounded response reads are mandatory.
- The local plugin needs no privileged account and executes no shell commands.
- The SSH plugin executes a fixed audited command set, requires host-key verification by default, bounds command output, and applies connection and command deadlines.
- The WinRM plugin executes fixed CIM resource/query pairs for required host identity and optional network, volume, service, and patch inventory plus one compiled-in PowerShell command for machine-wide uninstall-registry software inventory; it requires HTTPS outside loopback-only Lab mode, verifies server certificates, performs NTLMv2 without Basic fallback, bounds SOAP and command output, and applies operation deadlines and concurrency limits.
- The container runs as a non-root user with a read-only filesystem and no Linux capabilities.
- Secrets are resolved through bounded `env:`, `file:`, `vault:`, or `k8s:` references and never serialized into observations, CLI arguments, or logs.
- The Topo Agent authenticates with the same bearer API-key contract as any other controller client; its offline spool is AES-256-GCM encrypted at rest with a key from the same credential-reference contract, bounded in total size, and detects tampering rather than returning corrupted data.
- Collector enrollment (opt-in via `-ca-dir`) issues each collector its own short-lived certificate through a single-use, time-bounded token; the collector's private key is generated locally and never transmitted. See [Collector enrollment](docs/enrollment.md).
- Outbound mTLS (opt-in via `-mtls`, requires `-ca-dir`) lets the controller terminate TLS natively and authenticate collectors by their enrolled certificate instead of the bearer API key; a client presenting no certificate at all still reaches `POST /v1/enroll` (authenticated by its one-time token), but every other protected endpoint requires a verified certificate or the bearer key. See [Running as native mTLS](docs/enrollment.md#running-as-native-mtls).

The current API-key transport, and TLS termination without `-mtls`, are suitable for local evaluation only. Do not expose the controller to an untrusted network until certificate rotation, audit logging, and a persistent secret provider are implemented; without `-mtls`, production deployments need a TLS-terminating reverse proxy in front of the controller. See [SECURITY.md](SECURITY.md).

## Project status

Nischoy Topo is pre-alpha. The implementation order and acceptance gates are in [ROADMAP.md](ROADMAP.md). Contributions should follow [CONTRIBUTING.md](CONTRIBUTING.md). Licensed under Apache 2.0.

For durable project context across coding sessions, start with [the project plan and current handoff](docs/project-plan.md). Coding agents must also follow [AGENTS.md](AGENTS.md); those files are maintained as repository state so progress does not depend on chat history.
