# Nischoy Topo

Nischoy Topo is an open-source, destination-neutral discovery data plane for hybrid IT. It collects evidence about infrastructure, normalizes assets and relationships into a stable schema, and publishes them to ServiceNow or another CMDB without making the destination the discovery engine.

Topo is an independent public product repository under the Nischoy organization. It does not depend on Nischoy's private website or commercial source repositories.

This repository implements local, Linux SSH, Windows WinRM, SNMPv3, VMware,
Kubernetes Node/Pod, AWS Organizations, and Azure tenant/subscription discovery;
bounded credential references; an authenticated controller with outbound mTLS,
rotation, revocation, heartbeats, and jobs; an outbound-only agent with encrypted
offline buffering; SQLite persistence, backup/restore, source-aware asset
resolution, an audit log, and recurring scheduling; deterministic release and
package automation; JSON Lines/webhook publishing; and a ServiceNow IRE
publisher with real duplicate-CI reconciliation evidence. `topo mid run` adds
the first native ServiceNow ECC-compatible transport slice: strict SOAP ECC
polling, durable claim/restart handling, native Heartbeat-only dispatch, and a
safe correlated denial for every other topic. The PR #47 scoped-app Relay is
retained as an experimental predecessor, not a required installation or the
final architecture. The detailed status and evidence boundaries are maintained
in [ROADMAP.md](ROADMAP.md).

AWS Organizations has partial live-account evidence: real SigV4 connectivity,
multi-account enumeration, the not-enabled error path, and the documented
least-privilege policy are confirmed. Live AWS OU/delegated-admin/STS paths,
real Azure discovery (setup is blocked on Tenant Root Group Reader assignment),
real VMware/Kubernetes/SNMP compatibility, broader ServiceNow class coverage,
additional Kubernetes/cloud resource kinds, relationship precedence and
cross-ID correlation, real ServiceNow MID registration/validation/Heartbeat
evidence and stock Discovery topic contracts, PostgreSQL/HA, and the M3 scale
gates remain explicit follow-ups rather than completed claims.

It also includes **Topo Lab**, a deterministic estate simulator for exercising discovery concurrency, failures, identity resolution, and CMDB mappings without provisioning hundreds of real machines.

## Quick start

Requires Go 1.25 or later. Release and security-review evidence uses exact Go
1.25.13.

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

Discover a Kubernetes cluster's own node and pod inventory read-only over the real Kubernetes API:

```sh
./bin/topo lab kubernetes-serve -scenario examples/lab/clean-500.json > kubernetes-targets.txt
# In another terminal:
TOPO_KUBERNETES_TOKEN=topo-lab-token ./bin/topo discover kubernetes \
  -targets kubernetes-targets.txt -site lab -lab
```

Production targets require HTTPS with normal certificate verification; `-lab` permits HTTP and skipped certificate verification, restricted to loopback targets. Unlike SSH/WinRM/SNMP, a Kubernetes target is one cluster API server rather than one address per simulated host, so `topo lab kubernetes-serve` prints its own single target URL and there is no separate `kubernetes-targets` command. See [Kubernetes cluster discovery](docs/kubernetes.md) for what is collected, the hand-rolled Topo Lab fixture `client-go` is tested against (no official simulator exists, the same gap SNMP had), and current limitations.

Discover an AWS Organization's own account structure read-only over the real AWS Organizations API:

```sh
./bin/topo lab aws-organizations-serve -scenario examples/lab/clean-500.json > aws-targets.txt
# In another terminal:
TOPO_AWS_SECRET_ACCESS_KEY=env:LAB_SECRET LAB_SECRET=topo-lab-aws-secret-access-key-0123456789ab \
TOPO_AWS_SESSION_TOKEN=env:LAB_TOKEN LAB_TOKEN=topo-lab-aws-session-token \
./bin/topo discover aws-organizations \
  -targets aws-targets.txt -site lab -lab \
  -access-key-id AKIATOPOLABFIXTURE00 -region us-east-1
```

Production targets require HTTPS with normal certificate verification; `-lab` permits HTTP, restricted to loopback targets. Like Kubernetes and unlike SSH/WinRM/SNMP, an AWS Organizations target is one organization's API endpoint rather than one address per simulated host, so `topo lab aws-organizations-serve` prints its own single target URL and there is no separate `aws-organizations-targets` command. See [AWS Organizations discovery](docs/aws.md) for what is collected, the hand-rolled Topo Lab fixture with real SigV4 signature verification that `aws-sdk-go-v2` is tested against (no official local simulator exists for the Organizations API), and current limitations.

Discover an Azure AD tenant's own subscription structure read-only over the real Azure Resource Manager API:

```sh
./bin/topo lab azure-serve -scenario examples/lab/clean-500.json
# the printed https://127.0.0.1:6443 URL is both the token authority and the ARM target — copy it into a targets file
TOPO_AZURE_CLIENT_SECRET=env:LAB_SECRET LAB_SECRET=topo-lab-azure-client-secret-0123456789ab \
./bin/topo discover azure \
  -targets azure-targets.txt -site lab -lab \
  -tenant-id 11111111-1111-1111-1111-111111111111 \
  -client-id 22222222-2222-2222-2222-222222222222 \
  -authority-url https://127.0.0.1:6443
```

Production targets require HTTPS with normal certificate verification; `-lab` skips certificate verification against loopback targets, but (unlike Kubernetes/AWS) cannot fall back to plain HTTP — Azure's own SDK unconditionally refuses a non-HTTPS authority host, so Topo Lab's Azure fixture always serves HTTPS with a self-signed certificate. Like Kubernetes and AWS and unlike SSH/WinRM/SNMP, an Azure target is one tenant's ARM endpoint rather than one address per simulated host. See [Azure tenant discovery](docs/azure.md) for what is collected, the hand-rolled Topo Lab fixture that `azure-sdk-for-go` is tested against (no official local simulator exists for the ARM API), and current limitations.

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
curl -s -X POST -H "Authorization: Bearer $TOPO_API_KEY" \
  -H 'Content-Type: application/json' \
  --data '{"collector_id":"collector-1"}' \
  http://localhost:8080/v1/enrollment-tokens
TOPO_AGENT_ENROLLMENT_TOKEN='<token from above>' ./bin/topo agent enroll \
  -controller-url http://localhost:8080 -collector-id collector-1 \
  -cert-dir /etc/topo-agent/enrollment
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

The canonical `ObservationEnvelope` separates immutable source observations from resolved assets. Each asset has a source-native identity, optional strong identifiers, attributes, and evidence. Relationships refer to native identities within the observation. IP addresses remain mutable attributes and do not determine identity. When multiple site/collector/plugin sources report the same stable asset, Topo retains every source's latest claim; `topo serve -source-precedence` selects a deterministic winner and `GET /v1/assets` exposes contributing sources, field conflicts, and first/latest observation timestamps. See [source precedence and asset freshness](docs/source-resolution.md).

The public extension points are small Go interfaces:

- `discovery.Plugin`: capability description, configuration validation, connectivity check, and discovery.
- `publisher.Publisher`: destination validation, preview, and batch publication.
- `store.Repository`: immutable observation storage, per-source asset claims, and resolved-asset reads.

The JSON Schema and Protobuf definitions under `api/` are the cross-process contract. They are currently `v1alpha1`; breaking changes are allowed until `v1` but must increment the schema version.

The product family uses these names as capabilities are delivered:

- **Topo Relay** — agentless discovery collector deployed in a network segment.
- **Topo Agent** — outbound-only endpoint discovery agent.
- **Topo Hub** — self-hosted controller and local asset view.
- **Topo Connect** — ServiceNow and other CMDB publishers.
- **Topo Graph** — the future full CMDB product.

## ServiceNow behavior

Nischoy Topo maps assets to ServiceNow CI classes and supplies `sys_object_source_info` for stable source identity. Publishing uses `/api/now/identifyreconcile/enhanced`; it does not write CMDB tables directly. Preview mode produces the complete proposed payload without network access. The outbound payload is proven duplicate-free and idempotent across repeated scans (deduplicated within a batch, and validated identical across independent Topo Lab discovery runs), and ServiceNow's own identification and reconciliation behavior has been verified against a real developer instance for the `cmdb_ci_computer` class: submitting the same `sys_object_source_info` twice updates the existing CI rather than creating a duplicate. See [ServiceNow publishing](docs/servicenow.md). A production deployment must register the `Nischoy Topo` discovery source as a `discovery_source` choice value (a real ServiceNow requirement found during validation — writes are otherwise rejected), and configure reconciliation rules and explicit canonical-attribute mappings, before enabling writes; Nischoy Topo does not invent custom `cmdb_ci` fields for its internal identifiers.

`topo mid run` is the required control-plane direction. It polls native
`ecc_queue` output through `/ecc_queue.do?SOAP` as
`mid.server.<configured-name>`, journals claims before state transitions,
inserts correlated input results, supports only the stock `Heartbeat` topic in
this first slice, and visibly denies every other topic without executing its
name or payload. It installs nothing on the instance beyond ordinary native
ServiceNow MID/Discovery configuration. Real `ecc_agent` registration,
validation, stock Heartbeat XML, and Up/Down evidence remain unverified until a
sanitized official-MID reference capture is available. See
[Native ServiceNow ECC-compatible MID transport](docs/servicenow-mid.md).

The scoped-app `topo relay run` transport from PR #47 remains in the repository
as an experimental predecessor only; its custom tables and Scripted REST API
are not required for `topo mid run`. See
[predecessor scoped-app Relay](docs/servicenow-relay.md).

## Security posture

- The controller can require a bearer API key and caps request bodies at 10 MiB. With a key configured, operator reads and control-plane mutations require that bearer credential; enrolled collector certificates are limited to observation delivery, heartbeats, job polling/results, and certificate rotation. Leaving the key unset preserves the open evaluation mode.
- Destination URLs must use HTTPS; client timeouts and bounded response reads are mandatory.
- The local plugin needs no privileged account and executes no shell commands.
- The SSH plugin executes a fixed audited command set, requires host-key verification by default, bounds command output, and applies connection and command deadlines.
- The WinRM plugin executes fixed CIM resource/query pairs for required host identity and optional network, volume, service, and patch inventory plus one compiled-in PowerShell command for machine-wide uninstall-registry software inventory; it requires HTTPS outside loopback-only Lab mode, verifies server certificates, performs NTLMv2 without Basic fallback, bounds SOAP and command output, and applies operation deadlines and concurrency limits.
- The container runs as a non-root user with a read-only filesystem and no Linux capabilities.
- Secrets are resolved through bounded `env:`, `file:`, `vault:`, or `k8s:` references and never serialized into observations, CLI arguments, or logs.
- Native ServiceNow control uses a fixed SOAP ECC endpoint, exact MID agent
  identity, bounded/cancellable XML, redirect refusal, a local process lock,
  and a durable crash journal. Only `Heartbeat` is recognized; `Command`,
  `SSHCommand`, PowerShell, JavaScript, Groovy, and unknown topics receive a
  correlated denial. No target-bearing operation is enabled until its stock
  contract is captured and its requested targets intersect a local allowlist.
- The PR #47 scoped-app Relay's local-profile/encrypted-spool boundary remains
  documented as predecessor behavior, not the required native architecture.
- The Topo Agent authenticates with the same bearer API-key contract as any other controller client; its offline spool is AES-256-GCM encrypted at rest with a key from the same credential-reference contract, bounded in total size, and detects tampering rather than returning corrupted data.
- Collector enrollment (opt-in via `-ca-dir`) issues each collector its own short-lived certificate through a single-use, time-bounded token bound to that collector ID at issuance; the collector's private key is generated locally and never transmitted. See [Collector enrollment](docs/enrollment.md).
- Outbound mTLS (opt-in via `-mtls`, requires `-ca-dir`) lets the controller terminate TLS natively and authenticate collectors by their enrolled certificate instead of the bearer API key; a client presenting no certificate at all still reaches `POST /v1/enroll` (authenticated by its one-time token). A verified collector certificate authorizes collector data-plane endpoints but not operator endpoints. See [Running as native mTLS](docs/enrollment.md#running-as-native-mtls).
- Certificate rotation (`topo agent rotate`) renews a collector's certificate before its 90-day expiry, authenticated by the current certificate itself over mTLS rather than a new token, with no bearer-API-key fallback and no way to request a certificate for any identity but the one already proven. See [Renewing a certificate](docs/enrollment.md#renewing-a-certificate).
- Certificate revocation (`POST /v1/certificate-revocations`) immutably invalidates one exact serial; SQLite persists the record across restarts, revoked certificates cannot rotate, and compromise recovery re-enrolls the same collector ID with a fresh key. Revocation lookup failures fail closed. See [Revoking and recovering a certificate](docs/enrollment.md#revoking-and-recovering-a-certificate).
- Collector liveness heartbeats are always available, requiring no CA or opt-in flag: `POST /v1/heartbeats` accepts the bearer API key or a verified mTLS certificate, while operator-only `GET /v1/collectors` requires the bearer key. A heartbeat over mTLS is always recorded under the certificate's real identity even if the request body claims a different one. See [Collector heartbeats](docs/heartbeats.md).
- Job delivery is collector-initiated polling, since the agent never accepts inbound connections: operator-only `POST /v1/jobs` and `GET /v1/jobs/{id}` require the bearer key; collector `GET /v1/jobs` and `POST /v1/jobs/{id}/result` accept the bearer key or a verified certificate and are identity-bound. See [Job delivery](docs/jobs.md).
- Server-side recurring discovery scheduling (`POST /v1/schedules`, `GET /v1/schedules`, `DELETE /v1/schedules/{collector_id}`) lets an operator set a recurring `discover` cadence for a collector centrally rather than relying solely on that collector's own `-interval`; a due schedule becomes an actual job the next time the collector polls `GET /v1/jobs` — no background ticker, reusing job delivery's existing collector-initiated-polling mechanism. See [Server-side recurring discovery scheduling](docs/scheduling.md).
- SQLite live files and sidecars are created or tightened to owner-only POSIX permissions before use, and backup snapshots are built inside a private staging directory before verified, non-overwriting publication by `topo storage backup`; `topo storage restore` likewise publishes only a verified new path. Forward migrations from every supported schema retain persisted data and all pending versions roll back together on failure; downgrade recovery restores a pre-upgrade backup to a new path. See [backup, restore, and upgrade procedures](docs/storage.md#backup-and-restore).
- Semantic-tag releases build deterministic Linux, macOS, and Windows archives twice from different source paths, publish SHA-256 manifests and an SPDX SBOM, and bind the artifacts to keyless Sigstore signatures plus signed GitHub build/SBOM attestations. All release actions are pinned by commit and evidence is verified before publication. See [release artifacts and verification](docs/releases.md).
- Native package assembly consumes those verified binaries without recompiling them: DEB/RPM for Linux amd64/arm64, Authenticode-gated MSI for Windows amd64/arm64, a hardened Helm chart, and a deterministic offline bundle. Host packages install a dormant service definition but never embed credentials or start an unconfigured agent. See [package artifacts and lifecycle](docs/packages.md).
- External-review preparation now includes a reviewer-facing threat-boundary scope, a pinned known-vulnerability CI gate, and a findings/remediation/independent-retest protocol. The independent review has not yet occurred and this preparation makes no production claim. See [external security review](docs/security-review.md).

The current API-key transport, and TLS termination without `-mtls`, are suitable for local evaluation only. Do not expose the controller to an untrusted network until a persistent secret provider and encryption at rest are implemented; without `-mtls`, production deployments need a TLS-terminating reverse proxy in front of the controller. See [SECURITY.md](SECURITY.md).

## Project status

Nischoy Topo is pre-alpha. The implementation order and acceptance gates are in [ROADMAP.md](ROADMAP.md). Contributions should follow [CONTRIBUTING.md](CONTRIBUTING.md). Licensed under Apache 2.0.

For durable project context across coding sessions, start with [the project plan and current handoff](docs/project-plan.md). Coding agents must also follow [AGENTS.md](AGENTS.md); those files are maintained as repository state so progress does not depend on chat history.
