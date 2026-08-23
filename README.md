# Nischoy Topo

Nischoy Topo is an open-source, destination-neutral discovery data plane for hybrid IT. It collects evidence about infrastructure, normalizes assets and relationships into a stable schema, and publishes them to ServiceNow or another CMDB without making the destination the discovery engine.

Topo is an independent public product repository under the Nischoy organization. It does not depend on Nischoy's private website or commercial source repositories.

This repository is the first working vertical slice of the project plan. It currently includes local and Linux SSH host discovery; Windows WinRM discovery for audited CIM identity, hardware, OS, network, volume, service, and patch collection plus machine-wide uninstall-registry software inventory; HTTPS-only NTLMv2 authentication for Windows pilots; SNMPv3 network device identity and interface discovery (MIB-II `system`/`interfaces`, `authPriv` required in production); VMware vCenter/ESXi virtual machine and host inventory over the vSphere API (read-only only, `authPriv`-equivalent HTTPS-with-verified-certificates required in production); concurrent two-scan acceptance for 500 Linux and 500 Windows targets; bounded `env:`/`file:`/`vault:`/`k8s:` credential references; an authenticated controller ingestion API with an opt-in certificate authority for collector enrollment, opt-in native outbound mTLS, certificate rotation, durable serial-specific revocation/re-enrollment recovery, and always-on collector liveness heartbeats and job delivery; an outbound-only Topo Agent MVP with encrypted offline buffering, a hardened Linux systemd unit, and Windows Service Control Manager integration; identity resolution with a choice of storage backend — in-memory by default, or an opt-in SQLite-backed store (`topo serve -db-driver sqlite -db-dsn <path>`) that survives a controller restart, both behind the same `store.Repository` interface and proven behaviorally identical by a shared conformance test suite; a hash-chained, tamper-evident audit log (`GET /v1/audit`) of enrollment token issuance, collector enrollment, certificate rotation/revocation, job creation, and schedule changes, persisted the same way discovery data is; server-side recurring discovery scheduling (`POST /v1/schedules`) that turns into an actual job the next time the target collector polls for work, with no background ticker, persisted the same way; JSON Lines and HTTPS webhook publishers; and a ServiceNow IRE publisher whose outbound payload is validated duplicate-free and idempotent across repeated Topo Lab scans, and whose reconciliation behavior (submit once creates a CI, resubmit the same source key updates it rather than duplicating it) is verified against a real ServiceNow developer instance. WinRM real-host compatibility fixtures, Kerberos and certificate authentication, real-Windows verification of the agent's service wrapper, broader ServiceNow class/relationship coverage against a real instance beyond the single class validated so far, SNMP `authPriv` and VMware discovery verification against real devices/vCenter (both are implemented and tested against faithful simulators only, `vcsim` for VMware and a hand-rolled agent for SNMP), cloud and Kubernetes discovery, a PostgreSQL storage backend (deliberately deferred until an HA/clustered-controller deployment shape actually needs it — see [docs/storage.md](docs/storage.md)), and fleet scheduling remain subsequent work rather than being represented as complete.

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

Release artifacts include raw archives, DEB, RPM, MSI, Helm, and an offline
bundle. The protected distribution workflow promotes those exact bytes to APT,
RPM, Homebrew, WinGet, and OCI Helm channels after native-signature and
clean-machine gates. The channels are not public until their first real
promotion succeeds; see [package-manager distribution](docs/distribution.md).

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

Exercise simulated network devices over real SNMPv3 message exchanges, one loopback UDP agent per device:

```sh
./bin/topo lab snmp-serve -scenario examples/lab/clean-500.json > snmp-targets.txt
# In another terminal:
./bin/topo discover snmp \
  -targets snmp-targets.txt -site lab -lab > snmp-observation.jsonl
```

`-lab` is restricted to loopback targets and `noAuthNoPriv`. Production targets require `authPriv` with SHA authentication and AES privacy; there is no weaker fallback. See [SNMP network device discovery](docs/snmp.md).

Discover VMware vCenter (or standalone ESXi) virtual machine and host inventory read-only over the vSphere API:

```sh
TOPO_VMWARE_PASSWORD='replace-with-secret-input' ./bin/topo discover vmware \
  -targets vcenter-targets.txt \
  -site pilot \
  -username 'topo-reader@vsphere.local'
```

`vcenter-targets.txt` has one endpoint per line. Production targets require HTTPS with normal certificate verification; `-lab` permits HTTP and skipped certificate verification, restricted to loopback targets — for exploring the plugin against govmomi's `vcsim` simulator rather than a real vCenter, see the testing section of [VMware vCenter discovery](docs/vmware.md), which (unlike SSH/WinRM/SNMP) has no `topo lab vmware-serve` command since `vcsim` already serves that role directly.

Start the controller with authentication enabled:

```sh
TOPO_API_KEY='replace-with-a-long-random-value' ./bin/topo serve
# Equivalent explicit reference:
./bin/topo serve -api-key-ref env:TOPO_API_KEY
curl http://localhost:8080/healthz
```

By default nothing survives a restart. For discovery data (observations, assets, relationships), the hash-chained audit log (`GET /v1/audit`), and recurring discovery schedules (`GET /v1/schedules`) to survive one, add `-db-driver sqlite -db-dsn <path>`:

```sh
TOPO_API_KEY='replace-with-a-long-random-value' ./bin/topo serve \
  -db-driver sqlite -db-dsn /var/lib/topo/topo.db
```

Enrollment tokens, collector heartbeats, and one-off job state remain in-memory only regardless of `-db-driver` — though the audit record that a token was issued, a collector enrolled, or a job created still persists. See [Persistent storage and the audit log](docs/storage.md) and [Server-side recurring discovery scheduling](docs/scheduling.md).

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
Enrollment and rotation print certificate serials. An operator can durably
invalidate an exposed serial with `POST /v1/certificate-revocations`, then
re-enroll the same collector ID with a fresh token and key; see
[Revoking and recovering a certificate](docs/enrollment.md#revoking-and-recovering-a-certificate).

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

Nischoy Topo maps assets to ServiceNow CI classes and supplies `sys_object_source_info` for stable source identity. Publishing uses `/api/now/identifyreconcile/enhanced`; it does not write CMDB tables directly. Preview mode produces the complete proposed payload without network access. The outbound payload is proven duplicate-free and idempotent across repeated scans (deduplicated within a batch, and validated identical across independent Topo Lab discovery runs), and ServiceNow's own identification and reconciliation behavior has been verified against a real developer instance for the `cmdb_ci_computer` class: submitting the same `sys_object_source_info` twice updates the existing CI rather than creating a duplicate. See [ServiceNow publishing](docs/servicenow.md). A production deployment must register the `Nischoy Topo` discovery source as a `discovery_source` choice value (a real ServiceNow requirement found during validation — writes are otherwise rejected), and configure reconciliation rules and explicit canonical-attribute mappings, before enabling writes; Nischoy Topo does not invent custom `cmdb_ci` fields for its internal identifiers.

## Security posture

- The controller can require a bearer API key and caps request bodies at 10 MiB. With a key configured, operator reads and control-plane mutations require that bearer credential; enrolled collector certificates are limited to observation delivery, heartbeats, job polling/results, and certificate rotation. Leaving the key unset preserves the open evaluation mode.
- Destination URLs must use HTTPS; client timeouts and bounded response reads are mandatory.
- The local plugin needs no privileged account and executes no shell commands.
- The SSH plugin executes a fixed audited command set, requires host-key verification by default, bounds command output, and applies connection and command deadlines.
- The WinRM plugin executes fixed CIM resource/query pairs for required host identity and optional network, volume, service, and patch inventory plus one compiled-in PowerShell command for machine-wide uninstall-registry software inventory; it requires HTTPS outside loopback-only Lab mode, verifies server certificates, performs NTLMv2 without Basic fallback, bounds SOAP and command output, and applies operation deadlines and concurrency limits.
- The container runs as a non-root user with a read-only filesystem and no Linux capabilities.
- Secrets are resolved through bounded `env:`, `file:`, `vault:`, or `k8s:` references and never serialized into observations, CLI arguments, or logs.
- The Topo Agent authenticates with the same bearer API-key contract as any other controller client; its offline spool is AES-256-GCM encrypted at rest with a key from the same credential-reference contract, bounded in total size, and detects tampering rather than returning corrupted data.
- Collector enrollment (opt-in via `-ca-dir`) issues each collector its own short-lived certificate through a single-use, time-bounded token; the collector's private key is generated locally and never transmitted. See [Collector enrollment](docs/enrollment.md).
- Outbound mTLS (opt-in via `-mtls`, requires `-ca-dir`) lets the controller terminate TLS natively and authenticate collectors by their enrolled certificate instead of the bearer API key; a client presenting no certificate at all still reaches `POST /v1/enroll` (authenticated by its one-time token). A verified collector certificate authorizes collector data-plane endpoints but not operator endpoints. See [Running as native mTLS](docs/enrollment.md#running-as-native-mtls).
- Certificate rotation (`topo agent rotate`) renews a collector's certificate before its 90-day expiry, authenticated by the current certificate itself over mTLS rather than a new token, with no bearer-API-key fallback and no way to request a certificate for any identity but the one already proven. See [Renewing a certificate](docs/enrollment.md#renewing-a-certificate).
- Certificate revocation (`POST /v1/certificate-revocations`) immutably invalidates one exact serial; SQLite persists the record across restarts, revoked certificates cannot rotate, and compromise recovery re-enrolls the same collector ID with a fresh key. Revocation lookup failures fail closed. See [Revoking and recovering a certificate](docs/enrollment.md#revoking-and-recovering-a-certificate).
- Collector liveness heartbeats are always available, requiring no CA or opt-in flag: `POST /v1/heartbeats` accepts the bearer API key or a verified mTLS certificate, while operator-only `GET /v1/collectors` requires the bearer key. A heartbeat over mTLS is always recorded under the certificate's real identity even if the request body claims a different one. See [Collector heartbeats](docs/heartbeats.md).
- Job delivery is collector-initiated polling, since the agent never accepts inbound connections: operator-only `POST /v1/jobs` and `GET /v1/jobs/{id}` require the bearer key; collector `GET /v1/jobs` and `POST /v1/jobs/{id}/result` accept the bearer key or a verified certificate and are identity-bound. See [Job delivery](docs/jobs.md).
- Server-side recurring discovery scheduling (`POST /v1/schedules`, `GET /v1/schedules`, `DELETE /v1/schedules/{collector_id}`) lets an operator set a recurring `discover` cadence for a collector centrally rather than relying solely on that collector's own `-interval`; a due schedule becomes an actual job the next time the collector polls `GET /v1/jobs` — no background ticker, reusing job delivery's existing collector-initiated-polling mechanism. See [Server-side recurring discovery scheduling](docs/scheduling.md).
- SQLite operational recovery uses verified, non-overwriting `topo storage backup` and `topo storage restore` commands. Forward migrations from every supported schema retain persisted data and all pending versions roll back together on failure; downgrade recovery restores a pre-upgrade backup to a new path. See [backup, restore, and upgrade procedures](docs/storage.md#backup-and-restore).
- Semantic-tag releases build deterministic Linux, macOS, and Windows archives twice from different source paths, publish SHA-256 manifests and an SPDX SBOM, and bind the artifacts to keyless Sigstore signatures plus signed GitHub build/SBOM attestations. All release actions are pinned by commit and evidence is verified before publication. See [release artifacts and verification](docs/releases.md).
- Native package assembly consumes those verified binaries without recompiling them: DEB/RPM for Linux amd64/arm64, Authenticode-gated MSI for Windows amd64/arm64, a hardened Helm chart, and a deterministic offline bundle. Host packages install a dormant service definition but never embed credentials or start an unconfigured agent. See [package artifacts and lifecycle](docs/packages.md).

The current API-key transport, and TLS termination without `-mtls`, are suitable for local evaluation only. Do not expose the controller to an untrusted network until a persistent secret provider and encryption at rest are implemented; without `-mtls`, production deployments need a TLS-terminating reverse proxy in front of the controller. See [SECURITY.md](SECURITY.md).

## Project status

Nischoy Topo is pre-alpha. The implementation order and acceptance gates are in [ROADMAP.md](ROADMAP.md). Contributions should follow [CONTRIBUTING.md](CONTRIBUTING.md). Licensed under Apache 2.0.

For durable project context across coding sessions, start with [the project plan and current handoff](docs/project-plan.md). Coding agents must also follow [AGENTS.md](AGENTS.md); those files are maintained as repository state so progress does not depend on chat history.
