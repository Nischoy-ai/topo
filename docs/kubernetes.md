# Kubernetes cluster discovery

Topo discovers Kubernetes nodes and pods through read-only REST list calls.
It does not create, update, patch, delete or watch cluster objects.

## What is collected

A discovery request supplies one or more API server URLs as targets; it cannot supply a namespace filter, label selector, or any write operation. For each target, the plugin lists `v1.Node` objects and `v1.Pod` objects (across all namespaces) and maps both to Topo's `kubernetes_object` asset type.

| Object | Normalized data |
| --- | --- |
| Node | Kubernetes UID (identity), name, addresses, OS image, kernel version, container runtime version, architecture, CPU/memory capacity |
| Pod | Kubernetes UID (identity), name, namespace, phase, pod IP addresses |

Both kinds map to `model.AssetKubernetesObject`, with `kind` set to Node or Pod.
Identity uses `metadata.uid` for the object's lifetime. A recreated object has
a new UID even if it reuses a name; see [Kubernetes object identity](https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#uids).
IP addresses are attributes, not identities. `pod_runs_on_node` connects a
scheduled pod to its node; unscheduled pods remain assets without that relation.

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

Use a ClusterRole granting `list` on core API resources `nodes` and `pods`,
bound to the discovery identity across the cluster. Do not assume the built-in
`view` role grants node access. See [Kubernetes RBAC](https://kubernetes.io/docs/reference/access-authn-authz/rbac/). See [credential references](credential-references.md) for the full provider list.

## Dependency

The plugin uses the official [`k8s.io/client-go`](https://github.com/kubernetes/client-go)
with `k8s.io/api` and `k8s.io/apimachinery` pinned to v0.35.8. It imports the
Core v1 typed client and discovery client for nodes, pods and connectivity.
Exact dependencies are recorded in [go.mod](../go.mod).

## Validation coverage

Topo Lab's `pkg/lab/kubernetes_server.go` serves `/version`, `/api/v1/nodes`
and `/api/v1/pods` over HTTP using Kubernetes API types. It exercises the
plugin's client-go requests and JSON decoding, including bearer-token checks
and wrong-token rejection.

```sh
./bin/topo lab kubernetes-serve -scenario examples/lab/clean-500.json > kubernetes-targets.txt
# In another terminal:
TOPO_KUBERNETES_TOKEN=topo-lab-token ./bin/topo discover kubernetes \
  -targets kubernetes-targets.txt -site lab -lab
```

The 500-node integration scenario produces 500 node assets, 500 pod assets and
500 pod-to-node relationships. A repeated scan and store write retain the same
identities without duplicates. These tests use the Topo Lab API fixture; a live
Kubernetes cluster has not been validated.

## Security and transport behavior

- Production targets must use HTTPS with normal certificate and hostname verification; there is no fallback to HTTP or skipped certificate verification outside Topo Lab.
- Request options whose names indicate passwords, secrets, tokens, or credentials are rejected.
- Target URLs must not contain embedded credentials, a query string, or a fragment.
- The bearer token is bounded and checked for control characters, and never accepted as a CLI value, only through credential references.
- Node and pod listings are bounded to 100,000 objects per target.
- Target concurrency is bounded and cancellation propagates through the underlying Kubernetes API calls.
- Structured errors include the target and failing operation, never credentials.
- Only read-only `list` calls are made against `nodes` and `pods`. No create, update, patch, delete, or watch operation is ever issued.

## Supported scope

Inventory covers Node and Pod objects. Other workload kinds and custom resources
are planned separately. Targets and tokens are explicit; in-cluster credential
autodetection and namespace/label filters are not provided. The fixture coverage
above does not establish live-cluster compatibility. Cloud-structure discovery
is also documented for [AWS](aws.md) and [Azure](azure.md).
