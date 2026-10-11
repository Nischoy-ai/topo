# Mixed inventory candidate evidence

These results concern the unpublished managed mixed-inventory source candidate,
not the published 0.4.6 app or `v0.4.6-beta.1` worker. The full acceptance plan is
[managed mixed inventory](../mixed-inventory-pilot.md).

## Real collection — 2026-10-10

An existing disposable Azure lab supplied Ubuntu 24.04 and Windows Server 2022
and 2025. Target machines have no public IP. Windows scan accounts are
non-admin, with Remote Management Users and read access to WMI `root/cimv2`.
WinRM permits HTTPS/5986 and NTLMv2; Basic/unencrypted authentication are off.
The certificate trust was established through the Azure control plane before
this test. Candidate binaries ran from disposable paths, leaving the installed
Topo and service configuration intact. Credentials were streamed over verified
SSH and resolved from environment references; summaries contain no secrets.

| Target | Mode | Repeat result |
| --- | --- | --- |
| Ubuntu 24.04 | Production SSH worker executor with verified host keys | 3 assets, 2 relationships per scan; zero errors; identical identities across two scans; offline IRE preview passed |
| Windows Server 2022 | Production worker executor; fixed computer/interface mode | 2 assets, 1 relationship per scan; zero errors; identical identities across two scans; offline IRE preview passed |
| Windows Server 2025 | Production worker executor; fixed computer/interface mode | 2 assets, 1 relationship per scan; zero errors; identical identities across two scans; offline IRE preview passed |

The original lab run exposed two compatibility failures. WQL enumeration used
a class-specific resource URI instead of the documented namespace wildcard;
NTLM transport disabled the HTTP/2 handler but retained inherited `h2` ALPN.
The candidate uses the exact reviewed WQL with the wildcard URI and explicitly
advertises HTTP/1.1 for NTLM. Both fixes have regression tests. Microsoft's
[WQL enumeration example](https://learn.microsoft.com/en-us/windows/win32/winrm/querying-for-specific-instances-of-a-resource)
provides the URI contract; arbitrary WQL remains rejected by the fixture.

Direct scans and offline preview establish collection and mapping compatibility
on these two machines. They do not prove the real ServiceNow broker, ACLs,
transaction behavior, IRE reconciliation, app install/upgrade or a general
Windows compatibility guarantee. Address-change and domain/Kerberos behavior
were not exercised. Broader standalone disk/service/patch/software inventory
was deliberately excluded; no privileges were added to enable a WinRS shell.

## Source and verification scope

Reviewable implementation commit:
`997b796a11b9636c04030ede08b4984866b00539`. It retains the collection code
exercised above. The real executor test executable was built from the working
tree before commit and before the final pacing test changes; its SHA-256 is
`18b81335f46fe9104f3345d45edff3f51298a43a3ad10b3153e1bf16768fe0b4`.
The direct executor tests do not exercise worker-loop pacing or a real broker.

Exact Go 1.26.9 passed vet, pinned govulncheck v1.7.0 (no reachable
vulnerabilities), full race tests, native trimpath build and Windows amd64
vet/build. The final mixed-worker fixture was added afterward and passed the
focused worker/protocol/CLI race suite; runtime Go code was unchanged.
The initial security run hit local disk exhaustion during cross-platform
compilation; after removing only the task-created temporary cache, the full
rerun passed. This is a local resource failure, retained separately from the
successful verification.

Node planner, protocol-bound broker and mapper tests passed. ServiceNow SDK
4.9.0 built and packed candidate app 0.4.7 in a disposable directory; package
normalization validated 313 entries, 12 tables, five roles and seven worker
resources. The normalized candidate ZIP SHA-256 is
`0b1fbba4c571d5408b38bfc27df87fbef7410cb543fbf7e9db82d4b7838fa411`.
This developer SDK artifact is not a native XML export or customer release.

The candidate was subsequently installed on a disposable developer instance.
Its native base update-set export contains 441 records (1,297,275 bytes), with
SHA-256 `a517375ad29528453644dd0562fbbfc49fbe2ee3774b033ee9f6095ac01fcaa2`.
Explicit 0.4.7 offline inspection passed; the inspector still defaults to 0.4.6.
All 55 exported script fields match the rebuilt candidate source. Exact-table
reads show all 32 required ordered index column definitions, but the index view
does not prove uniqueness. The base export does not include the required index
and discovery-source companion. These checks establish authoring/export scope;
native XML clean installation, upgrade and managed broker/IRE acceptance remain
open. All 38 ServiceNow Python checks and the application JavaScript tests passed.

Managed acceptance and candidate publication remain open. The four lab VMs
started for these checks were returned to their original deallocated state;
no installed Topo, target privileges or provisioning was changed.

## Subnet acceptance prerequisite — 2026-10-11

The subnet work inspected draft PR #78 at exact source
`3684e9a9d56f72b7cc89a0dd752c11b46b963ba1` and continued in an isolated
branch. Its existing planner and worker execution policy were retained.
Additional planner checks compare 128 deterministic generated selections with
an independent address-set oracle, including overlapping/reordered CIDRs,
fragmented exclusions and repeat plan keys. They also check the post-exclusion
1,024-address limit, a 1,025-address selection across separate segments,
unchanged scope metadata on rejected planning and both IPv4 boundary addresses.
All application JavaScript checks and 38 ServiceNow Python checks passed.
Exact Go 1.26.9 worker and control-plane simulator race suites passed. These
are local/source checks; no real managed scan was performed in this attempt.
The added test and this evidence are follow-on changes to the pinned candidate,
not part of that candidate commit's previously recorded execution.

Read-only inspection of the authorized developer instance confirmed that the
unchanged 32-update index companion remains **Loaded**. Its native form reports
“Update set preview and commit are unavailable because another operation is
running: CICD Operation.” The active execution-tracker list contains seven
pending/running platform operations, including a batch install and App Engine
Studio/workspace loading at 99%; percentage alone does not establish completion.
No operation was cancelled, installation retried, update set committed,
instance reset, security policy changed or Azure VM started for this attempt.
This preserves the shared instance and the separate Windows work.

The exact next gate is native preview/problem review and commit of the already
loaded index companion, followed by verification of all 32 definitions and
17 unique constraints while retaining app 0.4.7. Managed subnet acceptance
then still requires authorized profiles and an attempt-bound worker using real
Password2/OAuth, observed exclusions and local denials before credentials or
connections, target bounds, measured start spacing/concurrency, cancellation,
unreachable-host isolation and two scans reconciling the same source identities
through real IRE. Neither the platform lock nor fixture success closes those
gates. Published 0.4.6 behavior and release scope remain unchanged.

## Replacement-instance native upgrade — 2026-10-11

The owner authorized a replacement developer instance after the platform lock
above. Its existing 0.4.6 app was upgraded using the same native 441-record
0.4.7 base export and SHA-256 recorded above. Preview reported 30 newer-local
collisions. Individual comparisons retained five instance-specific federated
role identities and eleven unchanged navigation modules; fourteen candidate
updates were accepted. The control-plane, profile validator, IRE processor and
observation mapper script fields matched the exact PR #78 source
`3684e9a9d56f72b7cc89a0dd752c11b46b963ba1`. No script was edited on the instance.

The native **Update Set Commit** dialog reported **Succeeded 100%**, in
57 seconds. The installed application form then reported version **0.4.7**,
scope `x_664635_topo`, and 447 application files. Existing companion metadata
contains 32 native index records, including the pool/worker lease-slot unique
definitions; full ordered physical coverage and uniqueness enforcement still
need verification. The companion was not reinstalled. This is evidence of an
existing-installation upgrade; it does not establish a clean installation or
data-preservation acceptance with populated fixture records.

No managed scan ran. Read-only readiness inspection found no existing Topo
worker service user, pool or OAuth application on the replacement instance.
Least-privilege worker setup is the next external gate, coordinated with the
Windows acceptance chat under exclusive instance mutation ownership. Subnet
scan, policy, cancellation and repeat IRE acceptance remain open. The Azure
lab remains deallocated and the published 0.4.6 beta scope is unchanged.

## SSH no-data serialization correction — 2026-10-11

The parallel Windows managed negative test exposed a zero-asset envelope that
encoded `assets` as JSON `null`, rejected by the strict managed mapper before
its no-data path. Inspection found the same defect in SSH discovery. The SSH
plugin now initializes its asset slice so authentication, parsing and connection
failures emit `assets: []` together with collection errors. Existing real-SSH
fixture tests now inspect the serialized envelope for authentication and
malformed-response failures. Exact Go 1.26.9 SSH/worker race tests and the full
security-review gate passed: vet, pinned govulncheck v1.7.0 (no reachable
vulnerabilities), all race tests, native trimpath build and Windows amd64 vet/build.
This source correction is separate from real SSH managed-negative acceptance,
which remains pending the shared instance handoff.
