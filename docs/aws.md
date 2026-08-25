# AWS Organizations discovery

Topo's AWS discovery slice collects an AWS Organization's own account structure — which accounts exist and how they are grouped into organizational units under the organization's roots — over the real AWS Organizations API, using only read-only Describe/List calls. This is slice 2 of M3's hybrid discovery milestone; it never issues a create, invite, move, tag, or policy operation against any organization object.

## What is collected

A discovery request supplies one or more AWS Organizations API endpoint URLs as targets. For each target, the plugin calls `DescribeOrganization`, `ListRoots`, and then recursively walks `ListOrganizationalUnitsForParent` and `ListAccountsForParent` starting from each root, down to 5 levels of nested organizational units — the same nesting limit AWS Organizations itself enforces.

| Object | Normalized data |
| --- | --- |
| Organization | Organization ID (identity), ARN, feature set, management account ID/ARN/email |
| Root | Root ID (identity), name, ARN |
| OrganizationalUnit | OU ID (identity), name, ARN, path |
| Account | Account ID (identity), name, ARN, email, state, joined method/timestamp |

All four kinds map to `model.AssetCloudResource` — a single generic asset type, not a new `AssetType` per kind — with `Identifiers["kind"]`/`Attributes["kind"]` set to `Organization`, `Root`, `OrganizationalUnit`, or `Account`. This is the same choice already made for Kubernetes's Node/Pod objects: AWS, Azure, and Kubernetes each have far more object kinds than Topo has fixed asset types, so a generic type plus a `kind` attribute scales the way a per-kind constant would not.

Asset identity is always the object's AWS-assigned ID (the 12-digit account ID, the `r-xxxx` root ID, the `ou-xxxx-xxxxxxxx` OU ID, or the `o-xxxxxxxxxx` organization ID), never its friendly name — an account's `Name` is mutable and can be changed by its owner, unlike its `Id`. A single `member_of` relationship connects every root, OU, and account to its immediate parent (the organization, or a root, or an OU), forming the organization's containment hierarchy — the same relationship type is reused at every level rather than one relationship type per parent/child kind pairing, since "X is contained in Y" is the same fact at every level of an AWS Organization.

Root, OU, and account listings are bounded to 100,000 objects in total per target, matching the bounded-read requirement every Topo plugin follows; this slice does not implement chunked pagination beyond that bound (each individual List call still pages internally via `NextToken` up to that bound). `DescribeOrganization` and `ListRoots` are required — a failure fails the whole target with a retryable `aws_organizations_operation` error. A failure listing OUs or accounts under one specific parent is a partial failure: it emits a retryable `aws_organizations_partial` error and skips that one subtree, leaving the rest of the organization's structure intact — the same required/optional split every other Topo protocol plugin uses for its own secondary listings.

## Authentication and transport

Production targets must use HTTPS with normal certificate verification — there is no insecure fallback outside Topo Lab. Authentication is a static AWS access key ID and secret access key (optionally with a session token, for temporary credentials from assuming a read-only cross-account organization role), supplied through Topo's shared, bounded credential-reference contract (`env:`, `file:`, `vault:`, `k8s:`) for the secret and session token — never as a CLI value. The access key ID itself, like a username, is not treated as secret and is a plain flag, matching VMware's and WinRM's own username handling. A target URL containing embedded credentials, a query string, or a fragment is rejected outright, the same rule every other Topo plugin enforces for its own targets.

```sh
TOPO_AWS_SECRET_ACCESS_KEY=env:AWS_SECRET_ACCESS_KEY \
./bin/topo discover aws-organizations \
  -targets aws-targets.txt \
  -site pilot \
  -access-key-id AKIA... \
  -secret-access-key-ref vault:secret/aws#secret_access_key \
  -region us-east-1
```

The AWS managed `AWSOrganizationsReadOnlyAccess` policy (or the narrower `organizations:DescribeOrganization`, `organizations:ListRoots`, `organizations:ListOrganizationalUnitsForParent`, and `organizations:ListAccountsForParent` actions alone) is all that is required; no other action is ever used. AWS Organizations API calls must originate from the organization's management account or a delegated administrator account — Topo does not attempt cross-account role assumption itself; if a caller needs to assume a role first, they resolve the resulting temporary access key ID, secret access key, and session token through the credential-reference contract exactly like any other credential. See [credential references](credential-references.md) for the full provider list.

`-region` is required and never defaulted or autodetected: AWS Organizations is only reachable from specific regional endpoints depending on partition (`us-east-1` for the standard `aws` partition; other regions for `aws-us-gov`/`aws-cn`), and Topo never guesses a partition's home region on a caller's behalf.

## Dependency

The plugin uses [`aws-sdk-go-v2`](https://github.com/aws/aws-sdk-go-v2), the official AWS SDK for Go, specifically `github.com/aws/aws-sdk-go-v2/service/organizations` and `github.com/aws/aws-sdk-go-v2/credentials` — this is the project's sixth external protocol dependency, after `golang.org/x/crypto` (SSH), `github.com/Azure/go-ntlmssp` (WinRM NTLM), `github.com/gosnmp/gosnmp` (SNMP), `github.com/vmware/govmomi` (VMware), and `k8s.io/client-go` (Kubernetes) — added for the same reason: hand-rolling AWS's SigV4 request signing and the AWS-JSON-1.1 wire protocol from scratch is exactly what this project's "prefer standard-library components and narrowly scoped dependencies" principle exists to weigh against, not to forbid outright.

## Testing against a hand-rolled Topo Lab fixture

AWS has no official local simulator for the Organizations API (unlike VMware's `vcsim`). LocalStack was evaluated and rejected for this slice: it runs as a separate Docker container rather than an in-process Go fixture, which is heavier than every other Topo Lab fixture and not reliably available in every build/CI environment.

Matching the Kubernetes and SNMP precedent instead: Topo Lab hand-rolls an AWS Organizations API fixture (`pkg/lab/aws_organizations_server.go`) that serves the small set of real AWS-JSON-1.1 wire responses the plugin actually calls — `DescribeOrganization`, `ListRoots`, `ListOrganizationalUnitsForParent`, `ListAccountsForParent` — over a real HTTP listener, dispatched by the real `X-Amz-Target` header AWS itself uses. Unlike the Kubernetes fixture, which encodes real `k8s.io/api` Go types directly (those carry public JSON struct tags), `aws-sdk-go-v2` generates its (de)serializers from a service model rather than JSON struct tags, so its types cannot be `encoding/json`-marshaled directly; the fixture instead defines minimal local structs mirroring the exact wire field names the generated deserializer expects (confirmed by reading `aws-sdk-go-v2`'s own `deserializers.go`, then verified empirically against the real client in this slice's own tests). The wire format and the plugin's real client-side request construction and response decoding are still genuinely exercised — only the server-side JSON encoding uses local types rather than the SDK's own.

Authentication is verified as real AWS SigV4, not a simplified string comparison: the fixture re-derives the expected `Authorization` header using the SDK's own `v4.Signer` against the known Lab credential, over exactly the header set the client's own `Authorization` header claims it signed (its `SignedHeaders` component), and compares it to what the request actually presented — the same technique a from-scratch signature verifier would use, without hand-rolling the HMAC-SHA256 canonicalization itself. A wrong-secret acceptance test is therefore a real, meaningful cryptographic failure rather than a bypassed check.

```sh
./bin/topo lab aws-organizations-serve -scenario examples/lab/clean-500.json > aws-targets.txt
# In another terminal:
TOPO_AWS_SECRET_ACCESS_KEY=env:LAB_SECRET TOPO_AWS_SESSION_TOKEN=env:LAB_SESSION_TOKEN \
LAB_SECRET=topo-lab-aws-secret-access-key-0123456789ab \
LAB_SESSION_TOKEN=topo-lab-aws-session-token \
./bin/topo discover aws-organizations \
  -targets aws-targets.txt -site lab -lab \
  -access-key-id AKIATOPOLABFIXTURE00 -region us-east-1
```

Unlike SSH/WinRM/SNMP, an AWS Organizations target is one organization's API endpoint, not one address per simulated host, so `topo lab aws-organizations-serve` prints its own single target URL to stdout and there is no separate `aws-organizations-targets` command — the same shape as the Kubernetes Lab fixture. The two-scan idempotency acceptance test (`pkg/discovery/aws/integration_test.go`) runs the full `examples/lab/clean-500.json` scenario (500 simulated hosts, mapped one-to-one to 500 simulated AWS accounts nested two levels deep under two organizational units, plus one account attached directly to the root): 506 total assets (1 organization, 1 root, 4 organizational units, 500 accounts) and 505 `member_of` relationships, stable and duplicate-free across a repeated scan and a `store.Memory` save — the same shape as every prior protocol's acceptance test, and additionally verified end-to-end via the CLI at the same scale.

A real AWS Organization was evaluated as an alternative fixture and deliberately not required for this slice, since it would require a real AWS account, a real AWS Organization already set up with a nontrivial account structure, and real (if read-only) IAM credentials — none of which are reproducible the way an in-process fixture is.

**Verified against a real, live AWS account (2026-08-25).** With the maintainer's own free-tier AWS account and a dedicated read-only IAM user, the plugin was run against the genuine `organizations.us-east-1.amazonaws.com` endpoint, outside Topo Lab, with real SigV4-signed requests:

- Before AWS Organizations was enabled on the account, `DescribeOrganization` correctly returned a real `AWSOrganizationsNotInUseException`, and the plugin reported it as a clean, non-retryable `aws_organizations_operation` collection error rather than mishandling it — itself a meaningful confirmation that the real AWS error path is handled correctly, not just the Lab fixture's simulated one.
- After enabling Organizations, `DescribeOrganization`/`ListRoots`/`ListAccountsForParent` correctly returned the real Organization, its Root, and the management account — then, after a second member account was added to the Organization, a repeat run correctly picked up the new account with no code changes, confirming real multi-account enumeration.
- The exact four-action IAM policy this document recommends above (`DescribeOrganization`/`ListRoots`/`ListOrganizationalUnitsForParent`/`ListAccountsForParent`, nothing broader) was attached to the test IAM user in place of the managed policy, and discovery produced identical results — confirming the documented minimum-permission claim is accurate, not just asserted.

This remains a single-account-then-two-account Organization with no organizational units created, so the recursive `ListOrganizationalUnitsForParent` OU-nesting walk, the `AccessDeniedException` permission-denied path, the delegated-administrator credential path, and STS temporary-credential (session token) usage are still verified only against the hand-rolled Topo Lab fixture, not a live account — the same partial-verification posture SNMP's `authPriv` and real VMware/Kubernetes clusters are in.

## Security and transport behavior

- Production targets must use HTTPS with normal certificate and hostname verification; there is no fallback to HTTP outside Topo Lab.
- Request options whose names indicate passwords, secrets, tokens, or credentials are rejected.
- Target URLs must not contain embedded credentials, a query string, or a fragment.
- The secret access key and session token are bounded and checked for control characters, and never accepted as CLI values, only through credential references.
- Root, OU, and account listings are bounded to 100,000 objects in total per target; OU recursion is bounded to 5 levels, matching AWS Organizations' own real nesting limit as defense-in-depth against a misbehaving or hostile endpoint.
- Target concurrency is bounded and cancellation propagates through the underlying AWS API calls.
- Structured errors include the target and failing operation, never credentials.
- Only read-only `Describe`/`List` calls are made. No create, invite, move, tag, or policy action is ever issued.

## Current limitations and next work

This slice covers the organization's own containment structure only — no per-account resource inventory (EC2 instances, S3 buckets, IAM roles, and so on, a much larger and separately scoped follow-up), no Service Control Policy (SCP) or tag-policy content, and no account-creation or account-closure lifecycle events. There is no cross-account role assumption built in: a caller who needs to assume a role resolves the resulting temporary credentials through the existing credential-reference contract, matching how every other Topo discovery plugin treats credentials as always explicit. Basic real-account verification has been performed (see "Verified against a real, live AWS account" above) — real connectivity, real SigV4 authentication, real multi-account enumeration, and real least-privilege IAM policy sufficiency are all confirmed against a genuine AWS account, not only the hand-rolled Topo Lab fixture. What remains unverified against a live account: the recursive OU-nesting walk (no organizational units exist in the test account), the `AccessDeniedException` permission-denied path, the delegated-administrator credential path, and STS temporary-credential (session token) usage — treat those specifically the same way WinRM real-host fixtures, SNMP `authPriv`, real VMware/vCenter, and real Kubernetes clusters are treated: implemented and tested against a faithful fixture, not yet proven against a live system. Azure tenants/subscriptions discovery and source precedence/conflict-freshness visibility are now implemented as later M3 slices; see `docs/project-plan.md` for what comes next.
