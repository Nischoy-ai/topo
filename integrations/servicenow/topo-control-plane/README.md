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

The SDK compiles the nine scoped tables, indexes, roles, ACLs, navigation,
Script Includes, six-route Scripted REST API, immutable profile/target-scope
business rules, **Run now** and **Cancel run** UI actions, two scheduled
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
separate least-privilege worker identity and API policy for the six routes; do
not reuse the direct IRE publisher identity.

The Slice A/B contract is scoped as `x_664635_topo`, the company prefix assigned
to the validation developer instance. This is intentionally separate from the
older experimental Relay/MID source under `x_nischoy_topo`; installing Slice A
does not migrate or rename those experiments.

Version `0.3.0` adds Slice B's target-scope planning metadata, deterministic
partition/task fields, unique pool/worker lease-slot reservations, renewal,
load-aware backpressure, and cooperative cancellation. Production still
creates exactly one `local.v1` local partition and exposes the same six worker
resources. Password2, Vault, credential endpoints, and remote discovery remain
deferred to later slices.
