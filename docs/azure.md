# Azure tenant discovery

Topo's Azure discovery slice collects an Azure AD (Microsoft Entra ID) tenant's own subscription structure — which subscriptions exist and how they are grouped into management groups under the tenant's root management group — over the real Azure Resource Manager (ARM) API, using only read-only Get/List calls. This is slice 3 of M3's hybrid discovery milestone; it never issues a create, move, or delete operation against any tenant, management group, or subscription object.

## What is collected

A discovery request supplies one or more ARM API endpoint URLs as targets. For each target, the plugin authenticates via the OAuth2 client-credentials grant, looks up the tenant's own details, then calls a single recursive `GET` on the tenant's root management group (`$expand=children&$recurse=true`) to retrieve the whole management-group/subscription hierarchy in one call, bounded to 6 levels of nesting — the same nesting limit Azure itself enforces. A flat subscription list enriches the tree's entries with state and display-name detail.

| Object | Normalized data |
| --- | --- |
| Tenant | Tenant ID (identity), display name, default domain |
| ManagementGroup | Full ARM resource ID (identity), short group name, display name |
| Subscription | Full ARM resource ID (identity), subscription GUID, display name, state |

All three kinds map to `model.AssetCloudResource` — a single generic asset type, not a new `AssetType` per kind — with `Identifiers["kind"]`/`Attributes["kind"]` set to `Tenant`, `ManagementGroup`, or `Subscription`. This is the same choice already made for Kubernetes's Node/Pod objects and AWS's Organization/Root/OrganizationalUnit/Account objects: AWS, Azure, and Kubernetes each have far more object kinds than Topo has fixed asset types, so a generic type plus a `kind` attribute scales the way a per-kind constant would not.

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

The plugin uses [`azure-sdk-for-go`](https://github.com/Azure/azure-sdk-for-go) — specifically `azidentity` (OAuth2 client-credentials token acquisition), `azcore`/`arm` (the ARM request pipeline), `armmanagementgroups`, and `armsubscriptions` — the project's seventh external protocol dependency, after `golang.org/x/crypto` (SSH), `github.com/Azure/go-ntlmssp` (WinRM NTLM), `github.com/gosnmp/gosnmp` (SNMP), `github.com/vmware/govmomi` (VMware), `k8s.io/client-go` (Kubernetes), and `aws-sdk-go-v2` (AWS Organizations) — added for the same reason: hand-rolling Azure AD's OAuth2 flows and the ARM wire protocol from scratch is exactly what this project's "prefer standard-library components and narrowly scoped dependencies" principle exists to weigh against, not to forbid outright. Adding it moved the stripped release binary from roughly 37 MiB (after the AWS slice) to roughly 38 MiB — well within the 512 MiB offline-bundle bound already raised for the Kubernetes slice.

## Testing against a hand-rolled Topo Lab fixture

Azure has no official local simulator for the Resource Manager API (unlike VMware's `vcsim`). Matching the Kubernetes/AWS precedent instead: Topo Lab hand-rolls an ARM fixture (`pkg/lab/azure_server.go`) that serves the small set of real wire responses the plugin actually calls — the tenant's OpenID Connect discovery document, the OAuth2 token endpoint, `GET /tenants`, the recursive management-group `Get`, and `GET /subscriptions` — dispatched the same way a real Azure AD/ARM deployment would route them. As with the AWS fixture, `azure-sdk-for-go`'s types are generated from a service model without JSON struct tags, so the fixture defines minimal local structs mirroring the exact wire field names the generated deserializer expects, verified empirically by running the real client against the fixture in this slice's own tests (a field-name mismatch would show up as a zero-valued field, not a silent pass).

Unlike AWS's SigV4 (a per-request signing scheme that genuinely needs cryptographic re-derivation to test meaningfully), Azure's ARM API has **no per-request signing** — a client obtains a bearer token once via OAuth2 and presents it on every call. Verifying the `client_id`/`client_secret` pair at the token endpoint and the bearer token on every ARM call by plain equality is therefore not a simplification here: it *is* the real protocol, the same way Kubernetes's bearer-token fixture check already was. A wrong-client-secret acceptance test is a real OAuth2 `invalid_client` rejection, not a bypassed check.

One genuine complication: `azidentity` (the Azure SDK's credential package) unconditionally refuses a non-HTTPS authority host — no client option overrides this. So unlike the Kubernetes and AWS Lab fixtures, which serve plain HTTP on loopback, Topo Lab's Azure fixture always serves **HTTPS with a freshly generated, self-signed, loopback-only certificate** (`-lab` skips certificate verification against it, the same as VMware's and WinRM's own `-lab` modes).

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

(`azure-targets.txt` contains that one printed URL — `topo lab azure-serve` doesn't print it to stdout for piping the way `kubernetes-serve`/`aws-organizations-serve` do, since it needs to keep running in the foreground for the certificate/listener lifetime; copy the logged address into a targets file, or capture stdout the same way the other two do.) Like Kubernetes and AWS, an Azure target is one tenant's ARM endpoint, not one address per simulated host, so there is no separate `azure-targets` command. The two-scan idempotency acceptance test (`pkg/discovery/azure/integration_test.go`) runs the full `examples/lab/clean-500.json` scenario (500 simulated hosts, mapped one-to-one to 500 simulated subscriptions nested two levels deep under two management groups, plus one subscription attached directly to the root — deliberately mirroring the AWS fixture's shape): 506 total assets (1 tenant, 5 management groups including the root, 500 subscriptions) and 505 `member_of` relationships, stable and duplicate-free across a repeated scan and a `store.Memory` save — the same numbers as the AWS slice by design, and additionally verified end-to-end via the CLI at the same scale.

A real Azure tenant was evaluated as an alternative fixture and deliberately not required for this slice, for the same reason a real AWS Organization wasn't for the AWS slice: it would require a real Azure AD tenant, a real app registration, and real (if read-only) credentials, none of which are reproducible the way an in-process fixture is. Real-tenant verification remains explicitly unverified, matching the SNMP `authPriv`/real-VMware/real-Kubernetes-cluster/real-AWS-Organization posture — implemented and tested against a faithful fixture only, not yet against a genuinely live Azure tenant.

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

## Current limitations and next work

This slice covers the tenant's own containment structure only — no per-subscription resource inventory (VMs, storage accounts, network resources, and so on, a much larger and separately scoped follow-up, the Azure counterpart to AWS's still-unstaged per-account resource inventory), no Azure Policy or management-group policy content, and no subscription-creation or subscription-transfer lifecycle events. There is no built-in credential chaining beyond the client-credentials grant (no managed-identity or Azure CLI credential fallback) — credentials are always explicit, matching how every other Topo discovery plugin treats credentials. Real-tenant verification beyond the hand-rolled Topo Lab fixture has not been performed; treat this the same way WinRM real-host fixtures, SNMP `authPriv`, real VMware/vCenter, real Kubernetes clusters, and real AWS Organizations are treated — implemented and tested against a faithful fixture, not yet proven against a live system. That completes the three protocol integrations `ROADMAP.md`'s M3 line names (Kubernetes, AWS Organizations, Azure); see `docs/project-plan.md` for what comes next in the milestone (source precedence/conflict-freshness visibility, scale/upgrade testing at 1K/10K/100K assets, and SSO/RBAC commercial modules).
