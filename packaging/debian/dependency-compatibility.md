# Debian dependency compatibility

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
