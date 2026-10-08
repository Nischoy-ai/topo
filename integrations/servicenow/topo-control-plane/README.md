# Nischoy Topo ServiceNow application source

This directory defines application **0.4.6** in ServiceNow Fluent under
`src/fluent`. The adjacent `application.json` is a review contract used by Go
tests; it must remain consistent with the sources and is not an installer.

For customer installation, use the published [combined XML package](../../../docs/servicenow-update-set.md).
Topo 0.4.6 Beta includes the application and all 32 required indexes.
Install the worker through the Beta package channel and record its build
using `topo version`.
Dated installation and upgrade results are kept in
[package validation](../../../docs/servicenow-validation.md).

## Build

The package pins `@servicenow/sdk` 4.9.0 and a lock file. Use Node.js 20 or newer:

```sh
npm ci --ignore-scripts --no-audit --no-fund
npm test
npm run build
npm run pack
```

The SDK compiles twelve scoped tables and index declarations, roles, ACLs,
navigation, Script Includes, seven Scripted REST resources, immutable
profile/target-scope/credential-binding rules, Run now/Cancel run actions and
two scheduled scripts. Generated `dist/app` output is ignored. Source and the
lock file are committed; no credentials or discovery data are bundled.

From the repository root, `scripts/build-servicenow-app.sh` builds twice,
validates the SDK inventory and application contract, and normalizes ZIP
container metadata to produce a reproducible SDK artifact. This build output
is separate from the platform-exported customer XML. SDK build-tool audit
status is recorded in the [security review record](../../../docs/security-review.md#build-tool-dependencies).

Version 0.4.6 accepts at most 4,096 package/service strings per host, with
4,096-character and control-character checks per entry. Other arrays retain
their 256-entry bound; the complete result is capped at 1 MiB. These lists are
observation evidence; IRE receives computers, adapters and ownership relations.

## Install

The SDK workflow is for developer/export instances. Customers should follow
[XML installation](../../../docs/servicenow-update-set.md) instead. Keep the
same delivery mechanism when upgrading an instance.

Use a reviewed checkout on a non-production instance. SDK installation needs
Node.js 20.18.0+, npm 8.19.3+ and an installation administrator; see
[SDK requirements](https://www.servicenow.com/docs/r/application-development/servicenow-sdk/install-servicenow-sdk.html).
Enter `integrations/servicenow/topo-control-plane` before authentication.
Use a dedicated developer/admin identity with an owner-only credential store;
keep passwords, codes, tokens and client secrets out of issues and shell history.

```sh
npx now-sdk auth --add dev-instance.service-now.com --type oauth --alias topo-dev
npm run deploy -- --auth topo-dev
```

From the repository root, `scripts/install-servicenow-app.sh topo-dev` uses
that preconfigured alias. It accepts no password or token values. Do not use
`--reinstall` without reviewing the removal of instance-created metadata absent
from source. Do not handcraft update-set XML or recreate application definitions
through forms, background scripts, Table API calls or direct metadata writes.

The app scope is `x_664635_topo`. It is separate from the older experimental
Relay/MID scope `x_nischoy_topo`; installation does not migrate those experiments.
After installation, create separate least-privilege worker and credential
custodian identities. The worker uses only the seven custom resources and
cannot read credential tables or call generic Table/CMDB/IRE APIs.
[Managed-worker documentation](../../../docs/servicenow-worker.md) covers the
Password2 credential broker, attempt-bound access, cancellation and lease
cleanup. External Vault binding is planned separately; it is not included in
the current managed-credential workflow.
