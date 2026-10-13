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

## Replacement-instance subnet readiness — 2026-10-11 UTC

After the Windows acceptance chat returned exclusive instance mutation
ownership, native table forms on dev317694 exposed 54 physical index rows
across all twelve installed Topo tables. The offline index-coverage checker
compared their complete ordered columns against the candidate SDK dictionary:
all 32 required combinations are present, including the 17 definitions marked
unique in source. The native physical view does not expose uniqueness, so this
check does not prove database uniqueness enforcement. Virtual index-row IDs
change between page loads and cannot be used as persistent record identities.

The native target-scope validator normalized overlapping inputs
`10.42.1.16/28` and `10.42.1.20/32`, with exclusions `10.42.1.16/30`,
`10.42.1.23/32`, and `10.42.1.24/29`, into three `/32` partitions selecting
only `10.42.1.20`, `.21`, and `.22`. This is live planning evidence, not
execution evidence. A dedicated pool is limited to two leases, 30-second leases
and 180-second tasks. No subnet profile, credential binding or scan has been
activated; storing the existing Linux nonadmin credential awaits specific
browser credential-access approval.

The Windows runtime correction at `78ec6e1f57c89ad0c58b52960920a79ecce7a83c`
is preserved by cherry-pick in the subnet review branch. Windows protocol and
broker ownership remain with the parallel Windows chat. The existing restricted
worker OAuth grant is available; no broader identity is needed. Subnet runtime
exclusions, local denials, bounds, pacing/concurrency, cancellation, unreachable
host isolation and repeat IRE gates remain pending. The published 0.4.6 beta
scope and release channels are unchanged.

## Approved Linux setup resume — 2026-10-11 UTC

The owner approved storing the existing nonadmin Linux scan credential and
binding it to the prepared subnet profile/scope on the replacement instance.
At submission, ServiceNow reported that the authenticated browser session had
expired. The credential save was not confirmed and no binding or subnet scan
was created. Reauthentication was the external gate at this checkpoint; the approval
persists and no new password or broader OAuth grant is required.

`internal/worker/realsubnet_test.go` is an opt-in live observer around the
production worker loop and SSH executor. It is restricted to the reviewed
three-address lab scope and operator-dispatched tasks, with two concurrent
executions, three seconds between starts and a 90-second total deadline. It
checks exact target counts, pacing, concurrency, and that local denials occur
before credential resolution and network access. Normal CI skips real-instance
execution. The observer compiled for Linux amd64; focused worker race checks
and the full exact Go 1.26.9 security gate passed. It has not yet run live and
therefore provides no new runtime or IRE acceptance claim.


## Bounded subnet managed acceptance — 2026-10-11 UTC

Acceptance passed on the owner-authorized replacement instance dev317694,
using the installed native app 0.4.7, its existing indexes, the restricted
seven-route worker OAuth grant, and the approved existing Linux nonadmin
credential stored in Password2. The profile and binding authorize only the
prepared scope and revision. No target permissions or Windows implementation
were changed. These results supersede the pending subnet gates above.

| Gate | Observed result |
| --- | --- |
| Exclusions and deduplication | Overlapping `/28` and `/32` inputs plus exclusions selected exactly three addresses; each executed once. No excluded address reached the broker or network. |
| Local authorization | With only the healthy address locally allowed, two other tasks failed before credential resolution or dialing; the allowed host still published 3 assets/2 relationships. |
| Target ceiling | Native inactive planning accepted 1,024 `/32` partitions; 1,025 targets across two segments produced an invalid-insert error and no persisted scope. No large subnet was scanned. |
| Rate and concurrency | Production executor starts were at least 3 seconds apart. Two independent worker boots concurrently claimed distinct tasks; a third claim returned no task at the pool's two-lease ceiling. The three-host collection run reached one active execution; it does not demonstrate two simultaneous SSH connections. |
| Cancellation | Native Cancel run reached Cancelled with 1 complete/2 cancelled tasks. Two heartbeat-or-renew signals were observed; only the first host began execution or dialed. Both remaining completions reached ServiceNow and active execution returned to zero. |
| Unreachable isolation | Healthy host published 3 assets/2 relationships while two unreachable hosts completed with collection errors and empty arrays. Their validated no-data deliveries did not call IRE. |
| Repeat IRE identity | The repeat healthy receipt applied 3 UPDATE items and 2 NO_CHANGE relationships. Native source mappings retained the same three CIs; the relationship list contained exactly the original two ownership rows. |

Native run IDs are `86bdc5bec33bc710b49fbefdd4013150` (local denial),
`b0edc1bec33bc710b49fbefdd40131da` (collection/repeat),
`32bf85b2c37bc710b49fbefdd401314d` (cancellation), and
`6affc9f2c37bc710b49fbefdd40131a7` (pool capacity). The capacity probe intentionally
reported two `acceptance_probe` failures without resolving credentials or
connecting; cancellation settled its remaining task. Its terminal Failed state
is expected cleanup, not a collection failure. The subnet pool subsequently
had no leased/running/results-received/IRE-processing tasks, and no acceptance
process remained in the lab.

Collection and local-denial observations used Go 1.26.9 Linux amd64 test source
`6e9119e`, executable SHA-256
`3958cb6855a3621fa7bf62f5c74f7abedf989af6a831d1675b9046c0df033dee`.
Final cancellation and capacity observations used source `1af3cec`, SHA-256
`69da9c7f43c68fbed5064aa74e7b9b4f967067a37a357aed06140bcfa91af435`.
Both wrap the production worker loop/executor; production runtime derives from
PR #78 `3684e9a` plus SSH no-data fix `a8e86aa` and the Windows chat's preserved
`78ec6e1` correction. An initial cancellation observer watched renewal only,
missed the production heartbeat signal and stopped before all completions.
The observer was corrected; native cancellation itself succeeded in that run.
Final acceptance above comes from the corrected observer, not that failed check.

Exact Go 1.26.9 final security checks passed: vet, full race suite, trimpath
build, Windows amd64 vet/build and pinned govulncheck v1.7.0 (zero reachable
vulnerabilities). All application JavaScript tests and 38 ServiceNow Python
checks passed. Sanitized native forms, receipts, source mappings, relationship
rows and production observer logs are retained separately in the private
maintainer handoff with exact run and binary attribution.

This closes bounded Linux subnet acceptance for the unpublished candidate.
Wrong-credential/trust tests, remaining Windows/mixed-estate gates, address
changes, restart recovery, clean installation, populated-data upgrade and
release-channel acceptance remain separate. Physical coverage is established
for all 32 indexes; the live pool test establishes this lease ceiling, not
independent enforcement evidence for every unique index. The public 0.4.6 beta
scope and assets remain unchanged; no release was published or promoted.


## Coordinated lease-recovery integration — 2026-10-11 UTC

After bounded subnet acceptance, the Windows acceptance chat found that native
`GlideRecord.updateMultiple` ignored JavaScript null when clearing persisted
lease fields. A cancelled, expired task could retain its unique pool/worker
slots and prevent later claims. The Windows-owned fix
`687b8f0728c7740c4b89a86fb17e6c98c1b55483` is preserved here by cherry-pick
`dad51cd`; it clears all seven expiry authority/slot fields with the documented
`'NULL'` sentinel. No planner, pacing, protocol or credential change is required.
Its behavioral platform-semantics fixture is included in the standard
application test command. The preceding broker fixture is copied unchanged
from Windows source `7a7e46d`; ownership and original history are preserved.

The separate [Windows acceptance PR #80](https://github.com/Nischoy-ai/topo/pull/80)
at final evidence source `5a94445` records the live recovery proof: probe source
`479d140`, SHA-256
`261cbaf6637050ab9eb65238e08b22ff6a4b1cbca4d0314af366df108142b046`,
observed cancellation, waited through a 120-second lease, verified broker and
renewal denial, no cancelled redispatch and all seven fields empty. Subsequent
production-worker runs succeeded and preserved IRE identities. That evidence
comes from the Windows chat; it is not a rerun of the subnet observer or a
claim of subnet restart recovery. The instance Script Include already contains
the source-verified fix, so this integration performs no additional mutation.

The same Windows evidence reports managed success/repeat and its requested
negative gates on Server 2022/2025, including a separately authorized WMI-denied
account and isolated protocol/profile/revision/scope broker mismatches. The
64-byte response-budget probe proves a bounded read of real responses, not a
real oversized 1 MiB response. These records close those Windows gates within
that stated scope; they do not prove address-change, macOS managed runtime,
combined mixed-estate native upgrade or production readiness. Original-instance
preview/CICD status remains last authenticated evidence only.

The integrated source passed the full exact Go 1.26.9 security gate, all
application JavaScript tests including the lease regression, and 38 ServiceNow
Python checks. Public 0.4.6 scope and release channels remain unchanged.


## Combined-candidate package review — 2026-10-11 UTC

The final Windows-owned opt-in acceptance probe is retained unchanged from
PR #80 source `5a94445`; this adds its cancellation-expiry, WMI-denial,
transport-bound and isolated native-binding checks to the subnet review branch.
No new live execution is attributed to this copy. The entire installed
TopoControlPlane script on dev317694 was read without modification and matches
the integrated source byte-for-byte (60,895 characters), including the lease fix.

Two SDK 4.9.0 builds and packs, normalized independently with the product
package validator, produced identical bytes and metadata. Candidate SHA-256:
`605e77ec30c501d0230494238941c0f140c2ba8f35a68f7aaf55041c2547fdbf`.
The package contains 313 entries, twelve tables, five roles and seven worker
routes. This is a developer SDK artifact, not a refreshed native XML export
or customer release. Comparing its dictionary with the previously captured
physical-index rows still covers all 32 required ordered combinations;
uniqueness remains separately limited as stated above. SDK dependency audit
reports build-tool vulnerabilities; the Go runtime security gate does not
certify that dependency tree.

Full exact Go 1.26.9 security checks, application JavaScript tests and all 38
ServiceNow Python checks passed again. Native clean installation remains
unproved because both available instances already contain Topo. The original
instance's companion gate now requires sign-in before its current platform
operation/preview state can be inspected. No uninstall, reset, reinstall,
update-set commit, publication or promotion was attempted to bypass these gates.
A separate empty disposable instance is required for clean-install acceptance;
a populated data-preservation upgrade also remains a separate native gate.

## Native companion and recovery follow-up — 2026-10-11

After renewed sign-in, the original developer instance showed no pending/running
platform execution tracker operations. Native preview of the unchanged 32-index
companion succeeded: 32 inserts, zero updates/deletes and zero collisions. The
owner separately approved its commit; ServiceNow reported Committed, with the
same counts. This closes the formerly blocked companion commit gate, not clean
installation or independent unique-index enforcement.

Before owner-authorized clean-install preparation, a metadata-only native 0.4.7
recovery export completed with 441 records, 1,297,604 bytes and SHA-256
`c663ddfd957548088b93cfb338d650048fa7f1e12e53926eca0318fd505d6e9a`.
Its nonempty script fields match the prior native export. The original installed
TopoControlPlane matched PR #78 source `3684e9a` exactly; a reviewed lease
correction is included in a refreshed native export. Its 441 records total
1,297,882 bytes, SHA-256
`eb42d7212e79fad4e8e5f2425504aa7082a2908dce99759741473cdd608d734a`.
All 55 script fields were compared: only TopoControlPlane changed, and it
matches integrated source `b011c13` exactly. No removal has yet
been performed. The populated managed-acceptance instance remains preserved.
Clean installation and populated-data upgrade remain open gates; the owner has
authorized reusing the original environment rather than supplying a third one.

Independent candidate review at `1c6ddb6` reproduced malformed response field
names reflected into worker decoder errors. The narrow correction from review
commit `eb39ca210c61ddade167bb7934b906a9380f3acf` is integrated with original
attribution as `b011c13f4671b294e828e0c830a1340b97e46e4e`. Synthetic regression
evidence fails before and passes after the correction; integrated worker race
tests pass. This changes worker response diagnostics only, not native app bytes
or the source attribution of earlier live scans. Exact-head CI remains in
progress at this checkpoint. No release, promotion or merge was performed.

## Native restoration and final scoped reliability — 2026-10-12

This section supersedes the earlier pending native checkpoints. Owner-approved
application removal completed on the original developer instance, with app
scope and all 12 table definitions absent before restoration. The reviewed
441-record native XML with SHA-256 `eb42d7212e79fad4e8e5f2425504aa7082a2908dce99759741473cdd608d734a`
was committed after 437 newer-local removal-history collisions were individually
matched to its input and accepted. The installed app showed 0.4.7, all 12 source
table IDs matched, and the installed control-plane script matched reviewed
source SHA-256 `4c697199558ad9106063716b0d8aac2e1ea6a5883d2e0ab5bd35a03eaf9b43f4`.
This is restoration on a used PDI, not pristine-instance or zero-collision proof;
independent installed equivalence for all 55 script fields is not asserted.

The first fresh physical audit found all 32 required index definitions missing.
Supported owner-approved native back out, preview and reapplication of the
unchanged companion succeeded. Fresh reads across 12 tables, with full forms
for two list-truncated composites, found 54 physical indexes and all 32 required
ordered column definitions; the compiled-source coverage checker reported zero
missing. Enforcement of the 17 unique definitions was not independently
exercised: the owner excluded duplicate-insert probes and relies on the platform
contract and reviewed configuration. Populated 0.4.6-to-0.4.7 upgrade is
owner-deferred, not passed. Neither excluded test blocks this undeployed
candidate. The populated managed-acceptance instance remains intact.

The reliability observer at `a2115e6b16f4e60ee54701592028bc5d237f43ee`,
SHA-256 `a8fd4d3514f75a826c6aad31d0af1a2dc90ca182c3dbbb5aac996066839bfe0d`,
used runtime base `06e6ecafbb555efeb87233b6dc841a549cbbc733`. Its sanitized
final records contain 14 runs (12 Complete, two Cancelled), 21 tasks
(18 Complete, three Cancelled), and 18 Applied receipts: exactly one per
completed task, none for cancelled tasks. Seven CI identities and four
relationship identities remained unchanged. Both protocol pools passed manual,
repeat and scheduled scans, a 45-second process-local client fault/recovery,
a precredential worker kill/restart, and precancel followed by a healthy claim.
All ten fixtures and 17 observer registrations were left inactive, with no
observer process and no occupied reserved lease slots. Empty reserved slots
do not independently establish all seven historical lease fields were cleared.
This does not prove a real network outage, address change, midcollection or
ambiguous-IRE restart recovery.

Documentation head `faa4f6a808e46a4f656436ca28100527e379dd50` passed all 17
checks: candidate CI run `38307802007` and unchanged published 0.4.6 channel
run `38307801980`. Later documentation or packaging changes need their own
exact-head results. The observations above retain their earlier runtime/source
pins. Customer packaging, signing and channel cutover are separate from these
native inputs and live acceptance observations; publication is not claimed here.

## Native customer batch comparison — 2026-10-13 UTC

The owner downloaded the native batch from the assembled 0.4.7 application
update set. The unchanged exported XML is 1,418,336 bytes with SHA-256
`45a13e48c675b77d11e2ff1610ff04b0e63cd396daad22f2c529d2be9b05b721`.
It contains 474 metadata updates: 441 application definitions, 32 unchanged
index definitions and one Global discovery-source registration Fix Script.
There are three distinct update sets; the platform serialized the parent
record twice identically. No XML was assembled or edited offline.

All 32 index payloads match the reviewed companion byte for byte. The Global
script matches the reviewed repository script (`fa0f0bcb6b2f23a8df78765e9cdd5f1a6c2b8027b5b54c0dd54ff31309951a20`)
with Unloadable false. Every application payload field was compared to the
previous source-verified integrated export. The only differences are native
restoration of 143 dictionary record IDs and their package/scope display
attributes, 20 reference display attributes, and capitalization of 12 table-level
`NULL` update names. Dictionary field definitions, references, scope IDs and
all remaining application payload fields match. Original bytes and the exact
comparison record are retained privately.

The instruction files were independently reviewed against configuration guide
`c70c5813895fcae20c54ee1fac54e0e44e9e531f`. The ZIP contains the same XML,
manifest and three instruction files; every extracted member equals its
reviewed original. The tracked signing manifest pins all six payloads. This
is export/source comparison and package integrity evidence. Native restoration
and physical index acceptance remain attributed to the component exports above;
this combined digest was not independently committed on a pristine instance.
Populated upgrade remains deferred and unique enforcement unexercised.
Signing and publication require the separately protected workflows and public
verification; this record does not claim a published 0.4.7 release.
