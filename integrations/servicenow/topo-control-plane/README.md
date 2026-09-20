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
npm ci --ignore-scripts --no-audit --no-fund
npm test
npm run build
npm run pack
```

The SDK compiles the twelve scoped tables, indexes, roles, ACLs, navigation,
Script Includes, seven-route Scripted REST API, immutable profile/target-scope/
credential-binding business rules, **Run now** and **Cancel run** UI actions, two scheduled
scripts, and the narrowly scoped IRE cross-scope privilege into `dist/app`.
Generated output is intentionally ignored; source and `package-lock.json` are
reviewed and committed.

`scripts/build-servicenow-app.sh` from the repository root runs this build
twice for a release, validates the SDK inventory and exact application
contract, and normalizes changing ZIP container metadata into a byte-
reproducible release artifact.

## Install

Developer/export-instance workflow only. Use a reviewed release checkout on a
non-production instance; the SDK requires Node.js 20.18.0+, npm 8.19.3+ and an
installation administrator. See [SDK requirements](https://www.servicenow.com/docs/r/application-development/servicenow-sdk/install-servicenow-sdk.html).
Clone `https://github.com/Nischoy-ai/topo.git`, check out the reviewed tag/commit,
and enter `integrations/servicenow/topo-control-plane` before authenticating.
The source's application version (`0.4.4`) differs from the worker beta version
(`v0.1.0-beta.1`). Record both in distribution evidence.


Authenticate the SDK with a dedicated developer/admin identity and an owner-
only credential store. Do not paste a password, authorization code, access
token, refresh token, or client secret into an issue, pull request, terminal
history, or chat.

```sh
npx now-sdk auth --add dev-instance.service-now.com --type oauth --alias topo-dev
npm run deploy -- --auth topo-dev
```

From the repository root, `scripts/install-servicenow-app.sh topo-dev` performs
the clean install/update sequence with that preconfigured OAuth alias. The
helper has no password, token, authorization-code, or client-secret option.
For developer upgrades, use the same SDK identity and reviewed source commit;
do not use `--reinstall` without explicitly accepting removal of instance-created
metadata absent from source. Keep SDK development separate from XML-installed
customer pilots.

The SDK remains the authoritative developer application-creation/update path.
For customer pilots, the staged [XML distribution process](../../../docs/servicenow-update-set.md)
publishes the installed source-built app through the platform. Do not handcraft
update-set XML or recreate application definitions through forms, background
scripts, the Table API, or direct metadata writes. After installation, create a
separate least-privilege worker identity and API policy for the seven routes; do
not reuse the direct IRE publisher identity.

The Slice A/B contract is scoped as `x_664635_topo`, the company prefix assigned
to the validation developer instance. This is intentionally separate from the
older experimental Relay/MID source under `x_nischoy_topo`; installing Slice A
does not migrate or rename those experiments.

Version `0.4.4` contains the Password2-only Linux SSH pilot from `0.4.3`,
denies credential retrieval as soon as ServiceNow requests cancellation, and
explicitly nulls every attempt/lease field when a lease expires so a stale
unique slot cannot block retry. The pilot provides protected credentials,
immutable profile/scope bindings, secret-free credential-access events, the
fixed attempt-bound `/credential` route, and reviewed `ssh_linux.v1` `/32`
tasks. Workers still have no table ACL, generic Table/CMDB/IRE access, durable
state, arbitrary-command surface, or inbound listener. External Vault binding
is deliberately deferred to Slice C2; it is not silently treated as complete.
This onboarding revision also uses stable Fluent application-menu references
and explicit UTC starts for its two periodic jobs so separately built
installation packages preserve the same application metadata.
