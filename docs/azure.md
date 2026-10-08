# Azure tenant discovery

Topo discovers a Microsoft Entra ID tenant's subscriptions and management-group
hierarchy through read-only Azure Resource Manager Get/List calls. It does not
create, move or delete tenant, group or subscription objects.

## What is collected

A discovery request supplies one or more ARM API endpoint URLs as targets. For each target, the plugin authenticates via the OAuth2 client-credentials grant, looks up the tenant's own details, then calls a single recursive `GET` on the tenant's root management group (`$expand=children&$recurse=true`) to retrieve the whole management-group/subscription hierarchy in one call, bounded to 6 levels of nesting — the same nesting limit Azure itself enforces. A flat subscription list enriches the tree's entries with state and display-name detail.

| Object | Normalized data |
| --- | --- |
| Tenant | Tenant ID (identity), display name, default domain |
| ManagementGroup | Full ARM resource ID (identity), short group name, display name |
| Subscription | Full ARM resource ID (identity), subscription GUID, display name, state |

All three kinds map to `model.AssetCloudResource`, with `kind` identifying
Tenant, ManagementGroup or Subscription.

Asset identity is always the object's **full ARM resource path** (for example `/subscriptions/{guid}` or `/providers/Microsoft.Management/managementGroups/{groupId}`), never a bare short name or the mutable display name. This is a deliberate, Azure-specific choice beyond the usual "never use a mutable name" rule: Azure automatically creates a "Tenant Root Group" whose short group name is, by Azure's own convention, identical to the tenant's own GUID — so a Tenant asset and the root ManagementGroup asset would collide on a bare-GUID identity even though they are different resource kinds. The full ARM path disambiguates them (and every other object) because it encodes the resource type in its path, the same way it would for a real Azure user browsing the portal or CLI. A single `member_of` relationship — reusing the same relationship type AWS's Organizations hierarchy uses — connects every management group and subscription to its immediate parent, forming the tenant's containment hierarchy.

Management-group and subscription totals are bounded to 100,000 objects per target in total, matching the bounded-read requirement every Topo plugin follows. Looking up the tenant and fetching the root management-group tree are both required — a failure fails the whole target with a retryable `azure_operation` error. The flat subscription-list enrichment call is optional: a failure emits a retryable `azure_partial` error but keeps the tree-derived subscription entries (with less detail — no `state`), the same required/optional split every other Topo protocol plugin uses for its own secondary listings.

## Authentication and transport

Production targets must use HTTPS with normal certificate verification — there is no insecure fallback outside Topo Lab. Authentication is the standard Azure AD OAuth2 **client-credentials grant**: a tenant ID, an application (service principal) client ID, and a client secret are exchanged for a short-lived bearer token, which the plugin then presents on every ARM call. The client ID, like a username, is a plain flag; the client secret is resolved through Topo's shared, bounded credential-reference contract (`env:`, `file:`, `vault:`, `k8s:`) — never a CLI value. This differs from Kubernetes's bearer-token model (a long-lived ServiceAccount token handed in directly): Azure AD access tokens are short-lived by design, obtained via an app-registration credential rather than distributed as a static secret, so the plugin performs the full token-acquisition round trip itself rather than accepting a pre-obtained token.

```sh
TOPO_AZURE_CLIENT_SECRET=env:AZURE_CLIENT_SECRET \
./bin/topo discover azure \
  -targets azure-targets.txt \
  -site pilot \
  -tenant-id 00000000-0000-0000-0000-000000000000 \
  -client-id 11111111-1111-1111-1111-111111111111 \
  -client-secret-ref vault:secret/azure#client_secret
```

The built-in **Reader** role, assigned at the tenant root management group scope, is all that is required; no write, move, or delete permission is ever used. `-authority-url` (default `https://login.microsoftonline.com`) is required and never defaulted or autodetected beyond that default: sovereign clouds (Azure Government, Azure China) use different authority and ARM hosts, and Topo never guesses which one a caller means. See [credential references](credential-references.md) for the full provider list.

## Dependency

The plugin uses the official [`azure-sdk-for-go`](https://github.com/Azure/azure-sdk-for-go):
`azidentity` for client-credential authentication, the ARM pipeline, and the
management-group/subscription clients. Exact versions are pinned in
[go.mod](../go.mod).

## Validation coverage

Topo Lab's `pkg/lab/azure_server.go` serves OpenID discovery, OAuth2 token
acquisition, tenants, recursive management groups and subscriptions. It checks
the client credentials at the token endpoint and bearer tokens on ARM calls.
The Azure SDK uses these real wire paths; wrong credentials produce
`invalid_client`. The fixture uses loopback HTTPS with a generated certificate;
certificate-verification relaxation is restricted to explicit Lab mode.

```sh
./bin/topo lab azure-serve -scenario examples/lab/clean-500.json
# the printed https://127.0.0.1:6443 URL is both the token authority and the ARM target:
TOPO_AZURE_CLIENT_SECRET=env:LAB_SECRET LAB_SECRET=topo-lab-azure-client-secret-0123456789ab \
./bin/topo discover azure \
  -targets azure-targets.txt -site lab -lab \
  -tenant-id 11111111-1111-1111-1111-111111111111 \
  -client-id 22222222-2222-2222-2222-222222222222 \
  -authority-url https://127.0.0.1:6443
```

The 500-subscription integration fixture contains 506 assets and 505 containment
relationships, including nested management groups. Repeated scans/store writes
retain identities without duplicates; CLI acceptance exercises the same scale.
No live-tenant acceptance result is recorded. Tenant-root Reader authorization
remained unresolved in the attempted setup, so fixture tests must not be
presented as live Azure compatibility evidence.

## Security and transport behavior

- Production targets must use HTTPS with normal certificate and hostname verification; there is no fallback to HTTP, even in Topo Lab (Azure's own SDK enforces this for the authority host regardless of Topo's own settings).
- Request options whose names indicate passwords, secrets, tokens, or credentials are rejected.
- Target and authority URLs must not contain embedded credentials, a query string, or a fragment.
- The client secret is bounded and checked for control characters, and never accepted as a CLI value, only through credential references.
- Management-group and subscription totals are bounded to 100,000 objects per target; management-group recursion is bounded to 6 levels, matching Azure's own real nesting limit as defense-in-depth against a misbehaving or hostile endpoint.
- Target concurrency is bounded and cancellation propagates through the underlying ARM calls.
- Structured errors include the target and failing operation, never credentials.
- Only read-only `Get`/`List` calls are made. No create, move, or delete action is ever issued.
- A network failure while acquiring a token (an unreachable or misconfigured authority) is reported as non-retryable, matching `azidentity`'s own classification of every authentication-phase failure — it cannot distinguish "the authority is briefly unreachable" from "these credentials are wrong," so Topo does not either. A network failure on a subsequent ARM data call, after a token was already obtained, is reported as retryable, the same as Kubernetes and AWS.

## Supported scope

Discovery covers tenant containment, not per-subscription VM/storage/network
inventory, policy content or subscription lifecycle operations. Authentication
uses explicit client credentials; there is no managed-identity or Azure CLI
fallback. Simulation and live-tenant status are described above. See the
[roadmap](../ROADMAP.md) for planned resource inventory.
