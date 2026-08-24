# Kubernetes cluster discovery

Topo's Kubernetes discovery slice collects a cluster's own node and pod inventory over the real Kubernetes REST API, using read-only `list` calls only. This is slice 1 of M3's Kubernetes discovery milestone; it never issues a create, update, patch, or delete against any cluster object.

## What is collected

A discovery request supplies one or more API server URLs as targets; it cannot supply a namespace filter, label selector, or any write operation. For each target, the plugin lists `v1.Node` objects and `v1.Pod` objects (across all namespaces) and maps both to Topo's `kubernetes_object` asset type.

| Object | Normalized data |
| --- | --- |
| Node | Kubernetes UID (identity), name, addresses, OS image, kernel version, container runtime version, architecture, CPU/memory capacity |
| Pod | Kubernetes UID (identity), name, namespace, phase, pod IP addresses |

Both kinds map to `model.AssetKubernetesObject` — a single generic asset type, not a new `AssetType` per Kubernetes kind — with `Identifiers["kind"]`/`Attributes["kind"]` set to `Node` or `Pod`. Kubernetes has far more object kinds than Topo has fixed asset types, so a generic type plus a `kind` attribute scales the way a per-kind constant would not; the same approach applies to the still-unstaged AWS/Azure slices via `AssetCloudResource`.

Asset identity is always the object's Kubernetes UID (`metadata.uid`), never its name or an IP address: a UID survives a pod reschedule, a node reboot, and IP reassignment, while a name is reusable (a deleted node's name can be reassigned to a differently-provisioned machine) and an IP is expected to change routinely, especially for a pod. A `pod_runs_on_node` relationship connects each pod to its node, resolved from `pod.Spec.NodeName`; an unscheduled pod (no node assigned yet) is kept as an asset without that relationship, since `PodPending` is a legitimate transient state, not a parse failure. Node and pod IP addresses are recorded as attributes only, never as separate `NetworkInterface` assets or as identity, unlike VMware's host/VM NICs — a pod's IP is expected to change on every reschedule, so treating it as a stable sub-asset would misrepresent it.

Node and pod listings are each bounded to 100,000 objects, matching the bounded-read requirement every Topo plugin follows; this slice does not implement chunked pagination beyond that single bound. Listing nodes is required — a failure fails the whole target with a retryable `kubernetes_operation` error. Listing pods is optional: a failure emits a retryable `kubernetes_partial` error and returns node-only inventory for that target, the same required/optional split VMware's host/VM listing uses.

## Authentication and transport

Production targets must use HTTPS with normal certificate verification — there is no insecure fallback outside Topo Lab. Authentication is a bearer token (a Kubernetes ServiceAccount token in production, the standard in-cluster and out-of-cluster auth model), supplied through Topo's shared, bounded credential-reference contract (`env:`, `file:`, `vault:`, `k8s:`), never as a CLI value. A target URL containing embedded credentials, a query string, or a fragment is rejected outright, the same rule VMware, WinRM, and SNMP already enforce for their own targets.

```sh
TOPO_KUBERNETES_TOKEN_REF=vault:secret/kubernetes#token \
./bin/topo discover kubernetes \
  -targets cluster-targets.txt \
  -site pilot \
  -token-ref vault:secret/kubernetes#token
```

A read-only ClusterRole covering `list` on `nodes` and `pods` (the built-in `view` ClusterRole is sufficient) is all that is required; no other verb or resource is used. See [credential references](credential-references.md) for the full provider list.

## Dependency

The plugin uses [`k8s.io/client-go`](https://github.com/kubernetes/client-go), the official Kubernetes Go client, pinned to `v0.35.8` along with `k8s.io/api` and `k8s.io/apimachinery` at the same version — the latest `v0.35.x` release still compatible with the project's exact Go 1.25.13 baseline (the `v0.36.x` series requires Go 1.26). This is the project's fifth external protocol dependency, after `golang.org/x/crypto` (SSH), `github.com/Azure/go-ntlmssp` (WinRM NTLM), `github.com/gosnmp/gosnmp` (SNMP), and `github.com/vmware/govmomi` (VMware) — added for the same reason: hand-rolling the Kubernetes API's request/response and authentication conventions from scratch is exactly what this project's "prefer standard-library components and narrowly scoped dependencies" principle exists to weigh against, not to forbid outright.

## Testing against a hand-rolled Topo Lab fixture

Unlike VMware — which has `vcsim`, an official simulator built for exactly this kind of testing — `client-go` has no equivalent real local test double outside of `envtest`/`kubebuilder`, which requires downloading platform-specific `kube-apiserver`/`etcd` binaries: heavier than this slice needs and not reliably available in every build environment. The in-memory fake clientset (`client-go/kubernetes/fake`) was also considered and rejected: it bypasses HTTP and JSON entirely, satisfying the clientset Go interface directly, so it would never exercise the plugin's actual request construction or response decoding — unlike every other protocol's acceptance test in this project.

Matching SNMP's precedent instead (SNMP also had no official simulator, since `gosnmp` is client-only): Topo Lab hand-rolls a Kubernetes API fixture (`pkg/lab/kubernetes_server.go`) that serves the small set of real Kubernetes REST API JSON responses the plugin actually calls — `GET /version`, `GET /api/v1/nodes`, `GET /api/v1/pods` — over a real HTTP listener, encoding real `k8s.io/api/core/v1` types the same way a genuine API server would. The wire format and the plugin's own `client-go` REST call and JSON-decode path are both genuinely exercised, even though the server behind them is hand-rolled rather than an official implementation. The fixture also enforces the same bearer-token contract a real API server would, so a wrong-token acceptance test is a real, meaningful failure rather than a bypassed check.

```sh
./bin/topo lab kubernetes-serve -scenario examples/lab/clean-500.json > kubernetes-targets.txt
# In another terminal:
TOPO_KUBERNETES_TOKEN=topo-lab-token ./bin/topo discover kubernetes \
  -targets kubernetes-targets.txt -site lab -lab
```

Unlike SSH/WinRM/SNMP, a Kubernetes target is one cluster API server, not one address per simulated host, so `topo lab kubernetes-serve` prints its own single target URL to stdout and there is no separate `kubernetes-targets` command. The two-scan idempotency acceptance test (`pkg/discovery/kubernetes/integration_test.go`) runs the full `examples/lab/clean-500.json` scenario (500 simulated nodes, one pod per node) against this fixture: 500 node assets, 500 pod assets, and 500 `pod_runs_on_node` relationships, stable and duplicate-free across a repeated scan and a `store.Memory` save — the same shape as every prior protocol's acceptance test.

A real cluster (`kind`, a managed cluster, or any conformant API server) was evaluated as an alternative fixture and deliberately not required for this slice, since a hand-rolled fixture over the real REST API shape needs no external binary download or cluster provisioning and is exactly as reproducible as `vcsim`. Real-cluster verification remains explicitly unverified, matching the SNMP `authPriv`/real-VMware/real-Windows posture — implemented and tested against a faithful fixture only, not yet against a genuinely live system.

## Security and transport behavior

- Production targets must use HTTPS with normal certificate and hostname verification; there is no fallback to HTTP or skipped certificate verification outside Topo Lab.
- Request options whose names indicate passwords, secrets, tokens, or credentials are rejected.
- Target URLs must not contain embedded credentials, a query string, or a fragment.
- The bearer token is bounded and checked for control characters, and never accepted as a CLI value, only through credential references.
- Node and pod listings are bounded to 100,000 objects per target.
- Target concurrency is bounded and cancellation propagates through the underlying Kubernetes API calls.
- Structured errors include the target and failing operation, never credentials.
- Only read-only `list` calls are made against `nodes` and `pods`. No create, update, patch, delete, or watch operation is ever issued.

## Current limitations and next work

This slice covers Node and Pod inventory only — no Deployment, Service, ConfigMap, PersistentVolumeClaim, or other workload-management object kinds (real, scoped follow-ups for a later slice once this one's shape is proven), and no CRD/custom-resource discovery. There is no in-cluster auto-config (`rest.InClusterConfig()`-style autodetection): targets and credentials are always explicit, matching every other Topo discovery plugin. Real-cluster verification beyond the hand-rolled Topo Lab fixture has not been performed; treat this the same way WinRM real-host fixtures, SNMP `authPriv`, and real VMware/vCenter are treated — implemented and tested against a faithful fixture, not yet proven against a live system. AWS Organizations and Azure tenants/subscriptions discovery remain separate, unstaged M3 slices; see `docs/project-plan.md` for what comes next.
