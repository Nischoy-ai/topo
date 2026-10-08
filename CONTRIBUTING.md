# Contributing

Submit focused pull requests with a description of the behavior changed and
relevant verification. Use descriptive feature/fix branch names. Preserve
truthful commit attribution and do not rewrite shared history.

## Checks

Use Go 1.26 compatibility and exact Go 1.26.9 for release/security evidence.
For Go changes, run formatting, `go vet ./...`, `go test -race ./...`, and
`go build -trimpath ./cmd/topo`. Windows-tagged changes also require Windows
amd64 vet/build. Run `scripts/security-review-checks.sh` for security-sensitive
or release changes. Documentation changes require correct links and accurate
version, security and validation claims.

New plugins need capability metadata, configuration/parser validation,
timeout/cancellation tests, arbitrary-operation rejection, fault isolation,
repeat-scan identity tests, explicit permissions and documented remote operations.
Schema changes require compatibility notes and updated schema, model, fixture
and publisher contract tests.

## Product and security boundaries

Topo is a standalone public product. Contributions must not introduce build or
runtime dependencies on private repositories, internal services, customer data
or proprietary infrastructure. Use destination-neutral observations and stable
source identity; never make an IP address the sole long-lived device identity.
ServiceNow CMDB publication uses supported IRE APIs.

Discovery executes reviewed, compiled-in operations with bounded reads,
deadlines and concurrency. Never accept arbitrary commands from a job or
controller. Verify remote identities, use least-privilege credential references,
and keep secrets out of CLI values, logs, labels and observations. Report
vulnerabilities through [the confidential reporting channel](SECURITY.md#report-a-vulnerability).

Use simulation for scale tests and sanitized real-system fixtures for
compatibility evidence. Match every public claim to its tested scope. See
[architecture](docs/architecture.md), [security](SECURITY.md) and
[the product roadmap](ROADMAP.md).
