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

## Windows managed acceptance precheck — 2026-10-11 UTC

PR #78 was inspected at exact head
`3684e9a9d56f72b7cc89a0dd752c11b46b963ba1`, still draft. All 17 checks passed:
CI run `38119200778` and public beta install run `38119200777`. These checks
verify their documented fixture/channel scopes, not managed Windows acceptance.
Windows follow-up is isolated on `feature/windows-managed-acceptance`; subnet
planner and pacing source are unchanged.

Read-only native checks on the authorized developer instance confirmed the
32-update index companion remains **Loaded**. ServiceNow displayed: “Update
set preview and commit are unavailable because another operation is running:
CICD Operation.” The filtered pending/running execution-tracker list contained
seven platform records, including App Engine Studio and workspace installation
work. No platform operation was cancelled, lock bypassed, update set committed,
app reinstalled/reset or candidate downgraded. This blocks the native companion
acceptance gate; it does not establish a Windows runtime defect.

Azure control-plane reads confirmed all four lab VMs deallocated. No new real
Windows collection, managed credentials, dispatch or IRE publication was
performed in this precheck. The [Windows matrix](../winrm-discovery.md#windows-target-compatibility-matrix)
keeps direct collection, deterministic security coverage and real managed
acceptance separate.

The added `windows_lease_authority.test.js` fixture executes the production
broker and `_ownedLease` implementation together against a bounded fake
GlideRecord and controlled clock. It verifies wrong worker/boot/task/attempt/
token, inactive state, exact expiry boundary, cancellation, renewed-attempt
rejection, protocol/profile/revision/scope mismatch and a redacted Password2
decryption exception. Every authority denial asserts no decryption. It does
not simulate a successful platform ACL check or prove live Password2 storage.

Resume gate: after the platform operation actually finishes, coordinate sole
ownership of shared-instance changes, preview the existing companion, inspect
problems before commit, verify required physical indexes and source setup while
retaining app 0.4.7. Then run the live Windows dispatch/broker/worker/IRE chain,
repeat reconciliation and each matrix failure case on Server 2022 and 2025 with
least-privilege identities and verified HTTPS. Native clean-install/upgrade
acceptance also remains open. No release is published or promoted by this work.

The Windows follow-up passed the complete `security-review-checks.sh` gate with
exact Go 1.26.9 and pinned govulncheck v1.7.0: module verification, full vet,
no reachable vulnerabilities, full race suite, native trimpath build and Windows
amd64 vet/build. All application JavaScript fixtures (including the new combined
lease/broker test), 38 offline ServiceNow Python tests and diff checks passed.
No production Go or ServiceNow script was changed by this follow-up.

## Replacement-instance Windows preparation

The owner authorized Windows acceptance on the replacement developer instance,
with the subnet acceptance chat retaining exclusive instance mutation ownership
until its native 0.4.7 upgrade is complete and verified. No live Windows gate is
closed by that authorization or by local preparation.

`TestRealWindowsManagedBrokerAuthority` is an explicit opt-in live probe for a
dedicated acceptance pool. It uses the production client and lease/broker API,
checks generic Table API denial, local target authority, wrong worker/boot/
attempt/token denials, a valid bounded no-store Password2 response and terminal
lease denial. It finishes the test task as a deliberate `acceptance_probe`
failure without target collection, result upload or IRE. It is not the successful
managed discovery test and does not prove protocol/profile/scope or expiry
negatives by itself. Normal CI skips before credential resolution; a skip is
not live acceptance evidence. Live execution remains pending verified upgrade,
exclusive instance handoff and least-privilege worker setup.

The new test compiled for the Linux lab worker. Focused worker/controlsim/WinRM
race checks passed with the live origin explicitly unset. Production runtime
source is unchanged. Existing installed Topo, lab power state, and both shared
ServiceNow instances were left unchanged during this preparation.
