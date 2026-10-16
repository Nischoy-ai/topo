# Fedora vendor license review

The maintainer classification and notice-retention review is complete for the
pinned module sources. The local Go Vendor Tools report passes: 95 license
files, no unknown/undetected/unmatched entries, and no exclusions. This is
not independent Fedora approval. Native RPM license installation and lint
must validate the result before filing.

## Completed follow-up — 2026-10-16

- Musl generator revision 7ada6dde6f9dc6a2836c3d92c2f762d35fd229e0 has a COPYRIGHT
  exactly matching the vendored file. All 54 distinct source-header attribution
  and license blocks are retained, covering BSD-2-Clause, BSD-3-Clause, ISC, MIT,
  SunPro, bcrypt-Solar-Designer and public-domain portions.
- The memory logo retains CC-BY-2.5 original-artwork and CC0-1.0 retouching
  attribution and full terms. The vendored 200x93 derivative differs from the
  linked 323x150 Commons original; no byte-equality claim is made.
- SQLite's exact dedication maps to LicenseRef-Fedora-Public-Domain.
- libc_musl.go also copies API documentation under Linux-man-pages-copyleft;
  its full notice is retained.

The licenses/ directory and archive post-commands preserve these notices in
the source and vendor trees. Manual classifications pin exact hashes.

Complete expression:

    Apache-2.0 AND BSD-2-Clause AND BSD-3-Clause AND CC-BY-2.5 AND CC0-1.0 AND ISC AND LicenseRef-Fedora-Public-Domain AND Linux-man-pages-copyleft AND MIT AND SunPro AND bcrypt-Solar-Designer

[Fedora license data](https://forge.fedoraproject.org/legal/fedora-license-data)
was inspected at cbd8b74cd481a598668f05d1878fcd53a8ef78e4. Software identifiers
are allowed; CC-BY-2.5, CC0-1.0 and Linux-man-pages-copyleft are confined to
permitted content/documentation. Canonical texts are from
[SPDX v3.27.0](https://github.com/spdx/license-list-data/tree/v3.27.0/text).
See licenses/NOTICE-MEMORY-LOGO for creator/editor credits and source hashes.

## Initial inspection — historical

## Inputs and detection

Candidate source is PR86 commit
`6a431a13b31fa10844cbf82d0cb0f12497974f7b`. It includes the unpublished setup
assistant; it is not the published 0.4.7 worker. Official Go Vendor Tools 0.13.0
generated the vendor tree with exact Go 1.26.9 and its default tidy operation.
Both `go.mod` and `go.sum` stayed byte-identical to that source.

Go Vendor Tools' askalono backend, using askalono 0.5.0, detected most license
files but returned failure for six. No ignore-undetected, ignore-unlicensed,
ignore-unknown or exclusion option was used. A partial expression emitted on
failure is not a usable final `License:` field.

## Reviewed composite files

The explicit entries in `go-vendor-tools.toml` pin each text's SHA-256:

| File | Reviewed expression | Basis |
| --- | --- | --- |
| `vendor/github.com/gosnmp/gosnmp/LICENSE` | `BSD-2-Clause AND BSD-3-Clause` | First section has two redistribution conditions; the separately attributed Go ASN.1 section adds the non-endorsement condition. Both apply to different portions. |
| `vendor/go.yaml.in/yaml/v3/LICENSE` | `Apache-2.0 AND MIT` | The text explicitly applies MIT to the listed libyaml-derived files and Apache-2.0 to the remaining files. |
| `vendor/gopkg.in/yaml.v3/LICENSE` | `Apache-2.0 AND MIT` | Byte-identical composite text to the preceding file. |

These are conjunctions for differently licensed portions, not a choice of
alternative licenses. See the [SPDX BSD-2-Clause](https://spdx.org/licenses/BSD-2-Clause.html)
and [BSD-3-Clause](https://spdx.org/licenses/BSD-3-Clause.html) texts and
[Go Vendor Tools' manual-detection procedure](https://fedora.gitlab.io/sigs/go/go-vendor-tools/scenarios/#manually-detecting-licenses).
A changed text invalidates its explicit entry and requires inspection again.

## Historical remaining review — 2026-10-15

| File | Concrete work remaining |
| --- | --- |
| `vendor/modernc.org/libc/COPYRIGHT-MUSL` | The umbrella MIT text also describes BSD and other permissive third-party portions. Match the actual translated/source content to applicable terms before assigning a complete expression; the umbrella paragraph alone is insufficient. |
| `vendor/modernc.org/memory/LICENSE-LOGO` | This file is a URL, and the vendor tree also contains `logo.png`. Verify image provenance/bytes and retain complete attribution/license terms. The linked Commons page describes original CC-BY-2.5 content and CC0 modifications; do not call the whole image MIT or ignore it. |
| `vendor/modernc.org/sqlite/SQLITE-LICENSE` | The text dedicates SQLite code to the public domain. Confirm Fedora's accepted identifier and the exact translated SQLite/source scope; do not substitute an unrelated permissive license or infer that every accompanying build script is public domain. |

The [linked logo description](https://commons.wikimedia.org/wiki/File:Memory_infra_logo.png)
and [SQLite's copyright statement](https://sqlite.org/copyright.html) are
starting evidence, not a completed redistribution/packaging review. Retain
notices, patents and author files alongside licenses. No dependency source,
artwork or tests have been removed to reduce the license calculation.

A complete review must also confirm source-file headers and notices, Fedora's
allowed-license data, license installation in the RPM and all statically linked
dependencies. Only then may the final expression replace the spec placeholder.
