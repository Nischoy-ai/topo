# AWS Organizations discovery

Topo discovers an AWS Organization's accounts, roots and organizational units
through read-only Describe/List calls. It records the containment hierarchy;
it does not create, move, invite, tag or change organization objects.

## What is collected

A discovery request supplies one or more AWS Organizations API endpoint URLs as targets. For each target, the plugin calls `DescribeOrganization`, `ListRoots`, and then recursively walks `ListOrganizationalUnitsForParent` and `ListAccountsForParent` starting from each root, down to 5 levels of nested organizational units — the same nesting limit AWS Organizations itself enforces.

| Object | Normalized data |
| --- | --- |
| Organization | Organization ID (identity), ARN, feature set, management account ID/ARN/email |
| Root | Root ID (identity), name, ARN |
| OrganizationalUnit | OU ID (identity), name, ARN, path |
| Account | Account ID (identity), name, ARN, email, state, joined method/timestamp |

All four kinds map to `model.AssetCloudResource`, with `kind` identifying
Organization, Root, OrganizationalUnit or Account.

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

The plugin uses the official [`aws-sdk-go-v2`](https://github.com/aws/aws-sdk-go-v2)
Organizations client and credential provider. Exact module versions are pinned
in [go.mod](../go.mod). The SDK supplies SigV4 signing and wire serialization.

## Validation coverage

Topo Lab's `pkg/lab/aws_organizations_server.go` serves the four Organizations
API operations over HTTP and validates SigV4 signatures with the SDK signer.
Integration tests exercise the real SDK request/response path and wrong-secret
rejection. The 500-account fixture contains 506 assets and 505 containment
relationships; repeated scans and store writes preserve identities without
duplicates. This is simulation evidence, separate from live-account coverage.

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

### Real-account coverage (2026-08-25)

A dedicated read-only IAM identity exercised the real Organizations endpoint.
Before Organizations was enabled, the plugin correctly classified
`AWSOrganizationsNotInUseException` as a non-retryable collection error.
After enablement it discovered the organization, root and management account;
a later run included a newly added second account. The four-action IAM policy
listed above produced the same results as the broader managed policy.

The live organization had no OUs. Recursive OU traversal, permission-denied
handling, delegated-administrator credentials and STS session tokens have
fixture coverage, but no recorded live-account validation.

## Security and transport behavior

- Production targets must use HTTPS with normal certificate and hostname verification; there is no fallback to HTTP outside Topo Lab.
- Request options whose names indicate passwords, secrets, tokens, or credentials are rejected.
- Target URLs must not contain embedded credentials, a query string, or a fragment.
- The secret access key and session token are bounded and checked for control characters, and never accepted as CLI values, only through credential references.
- Root, OU, and account listings are bounded to 100,000 objects in total per target; OU recursion is bounded to 5 levels, matching AWS Organizations' own real nesting limit as defense-in-depth against a misbehaving or hostile endpoint.
- Target concurrency is bounded and cancellation propagates through the underlying AWS API calls.
- Structured errors include the target and failing operation, never credentials.
- Only read-only `Describe`/`List` calls are made. No create, invite, move, tag, or policy action is ever issued.

## Supported scope

Discovery covers organization structure, not EC2/S3/IAM resource inventory,
policy content or account lifecycle actions. Topo does not assume roles;
operators resolve temporary credentials explicitly when role assumption is
needed. The real-account and fixture coverage above define the tested scope.
See the [roadmap](../ROADMAP.md) for resource inventory plans.
