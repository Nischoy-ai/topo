# Nischoy Topo ServiceNow control plane

This directory is a source-driven ServiceNow application. The authoritative,
installable metadata is defined with ServiceNow Fluent in `src/fluent`. The
adjacent `application.json` is a review contract used by Go tests; it is not an
installer and must remain consistent with the Fluent sources.

The package pins `@servicenow/sdk` 4.9.0 and a lock file. It contains no
credentials, discovery targets, arbitrary operation payloads, or worker-side
state.

## Build

Use Node.js 20 or newer:

```sh
npm ci --ignore-scripts
npm test
npm run build
```

The SDK compiles the twelve scoped tables, indexes, roles, ACLs, navigation,
Script Includes, seven-route Scripted REST API, immutable profile/target-scope/
credential-binding business rules, **Run now** and **Cancel run** UI actions, two scheduled
scripts, and the narrowly scoped IRE cross-scope privilege into `dist/app`.
Generated output is intentionally ignored; source and `package-lock.json` are
reviewed and committed.

## Install

Authenticate the SDK with a dedicated developer/admin identity and an owner-
only credential store. Do not paste a password, authorization code, access
token, refresh token, or client secret into an issue, pull request, terminal
history, or chat.

```sh
npx now-sdk auth --add dev-instance.service-now.com --type oauth --alias topo-dev
npm run deploy -- --auth topo-dev
```

The SDK install is the only supported application-creation/update path. Do not
recreate these records through Studio forms, background scripts, update-set
XML, the Table API, or direct metadata writes. After installation, create a
separate least-privilege worker identity and API policy for the seven routes; do
not reuse the direct IRE publisher identity.

The Slice A/B contract is scoped as `x_664635_topo`, the company prefix assigned
to the validation developer instance. This is intentionally separate from the
older experimental Relay/MID source under `x_nischoy_topo`; installing Slice A
does not migrate or rename those experiments.

Version `0.4.3` contains the Password2-only Linux SSH pilot from `0.4.0`,
denies credential retrieval as soon as ServiceNow requests cancellation, and
explicitly nulls every attempt/lease field when a lease expires so a stale
unique slot cannot block retry. The pilot provides protected credentials,
immutable profile/scope bindings, secret-free credential-access events, the
fixed attempt-bound `/credential` route, and reviewed `ssh_linux.v1` `/32`
tasks. Workers still have no table ACL, generic Table/CMDB/IRE access, durable
state, arbitrary-command surface, or inbound listener. External Vault binding
is deliberately deferred to Slice C2; it is not silently treated as complete.
