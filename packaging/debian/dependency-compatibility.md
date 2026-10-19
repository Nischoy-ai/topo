# Debian dependency compatibility

## Current archive-only candidate (owner direction, 2026-10-18)

The full CLI now uses official Debian unstable NTLM, Azure SDK and MSAL packages.
No separately prepared library update is built or injected by `qualify.sh`.
Declared compatible floors for the three library packages are:

| Debian library package | Archive version selected for testing |
| --- | --- |
| golang-github-azure-go-ntlmssp-dev | 0.0~git20221128.754e69321358-2 |
| golang-github-azure-azure-sdk-for-go-dev | 68.0.0+git20231220.f497335+dfsg1-7 |
| golang-github-azuread-microsoft-authentication-library-for-go-dev | 1.0.0-3 |

The owner explicitly accepted the NTLM malformed-challenge finding
`GO-2026-5543` and Azure Identity `GO-2024-2918` for this downstream candidate.
Actual scan output and source versions remain recorded. A green build does not
make those vulnerabilities fixed. The compiler and other library requirements,
original upstream go.mod/go.sum, source and signed-release inputs are unchanged.
The historical update candidates below are not prerequisites for this route.
Archive-only native build/test/lifecycle evidence must be collected afresh.

## Historical compatibility and security-update investigation

Upstream go.mod/go.sum remain byte-identical to the public 3adbdb3 snapshot.
Debian uses its independently maintained library sources and its module-aware
workspace. Go's reported requested module version is **not** the version of
replaced Debian source; inspect installed package identities and source notices.
Native security analysis must account for this distinction.

The October17 f54d8c8 diagnostic used signed Debian unstable packages, generated
an offline workspace, compiled the complete CLI, ran all Topo unit tests, and
created a DEB. This was a diagnostic `-d` build, not clean-build qualification.
Its actual import graph has 663 packages and 74 modules, without load errors.

The following upstream pins had initially been copied into Debian Build-Depends
without evidence that they were minimum compatible APIs. The diagnostic proved
build/unit compatibility with the listed archive sources. Debian-only floors
now record these observed versions; this does not change signed-release inputs,
prove security equivalence or establish that any still older version works.

| Debian source | Tested archive version |
| --- | --- |
| aws-sdk-go-v2 | 1.41.7-2 |
| smithy-go | 1.25.1-3 |
| microsoft-authentication-library-for-go | 1.0.0-3 |
| go-restful | 3.10.2-1 |
| reflect2 | 1.0.2-2 |
| govmomi | 0.24.2-4 |
| protobuf | 1.36.10-2 |

`go-strftime` is not in the actual native import graph with Debian's SQLite
sources, so it is no longer a declared Debian build dependency. It remains in
the upstream module manifest; no upstream dependency or source was removed.

The NTLM >=0.1.1 requirement and Go/x-crypto/x-net security floors are retained.
`prepare-ntlmssp.py` produces a separate unsigned 0.1.1 source update for the
existing Debian library package, verified against Topo's original Go checksum.
Its MIT source and copied MD4 BSD-3-Clause notices are retained in full. Clean
qualification uses the resulting independent DEB via sbuild --extra-package.
That is a staged dependency update, not availability in Debian's default archive.
No Fedora vendor archive is used. The library's upstream real-IIS test requires
credentials and remains skipped; no Windows/live-system evidence is inferred.

Two helper/package metadata issues are repaired only in the private build tree:
legacy go-flowrate needs a minimal go.mod, and installed json-patch/v5 needs an
explicit nested-module mapping and requirement because Debian client-go imports
v5 while its packaged requirements still list v4. No installed library source,
upstream go.mod/go.sum, arbitrary operation, security pin or runtime compatibility
knob is altered to hide those problems.

## Native security finding and dependency update candidates

The native scan at3dfd473 used actual installed library versions and found
GO-2024-2918 in Debian's azidentity1.5.0-beta.3. The upstream fixed release is
1.6.0. `prepare-azure.py` prepares a separate Azure SDK source package with complete
upstream azidentity1.6.0, azcore1.11.1 and internal1.8.0 modules. Original Debian
sources/notices are retained for the remaining SDK. `prepare-msal.py` supplies
its MSAL1.2.2 dependency as a separate source update. Each complete module archive
is verified against a pinned Go h1 checksum independently checked with the
official checksum database. No vulnerability record or upstream Topo pin changes.

The Azure SDK candidate retains Debian's existing offline test exclusions. Its
compilation scope is the updated SDK modules. Installed development sources
contain the Go APIs; repository automation is omitted. The complete repacked
source, including that automation, remains in the source archive.
It adds explicit race-enabled Azure Arc regression tests for the security fix.
This scope does not establish compatibility of every other Azure SDK consumer.
Topo then independently compiles and tests its complete CLI with these packages.

The original archive's MSAL1.0.0 diagnostic compatibility result above is historical;
that historical security-clean candidate required the new dependency chain.
The owner superseded that requirement with the archive-only, findings-accepted
candidate above; those three proposed updates are no longer prerequisites.

MSAL's separate `apps/tests/integration` and `apps/tests/performance` suites
require Microsoft lab credentials/endpoints and are excluded from the offline
package test. All ordinary library unit tests retain race checking. No live
authentication or performance result is inferred from this package build.

Azure's TestMain normally starts a recording proxy and requires a git checkout
even for mock-only tests. The explicit Arc invocation selects exactly the two
upstream Arc mock-server test functions and sets the supported AZURE_RECORD_MODE
to `live` solely to bypass that recording proxy. Both functions use the SDK mock
transport; the build still has only loopback and no cloud credentials. This is
not an Azure live-authentication test. No upstream test function is rewritten.
