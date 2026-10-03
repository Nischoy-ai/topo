# Install the Topo pilot application from XML

**Status: 0.4.6 clean installation, identical reimport/commit, 0.4.5-to-0.4.6 upgrade, and real Linux manual/repeat/scheduled discovery passed on dev317694. Broader security/recovery acceptance remains open.** The published
worker beta is `v0.1.0-beta.1`; it does not contain a customer update-set XML.
Do not rename its SDK ZIP or upload an arbitrary XML record export. A separate
XML candidate has passed those installation tests; it still requires the
remaining pilot gates and publication approval before customer distribution.

## Customer installation

Once a validated XML release is available:

1. Download its XML, `manifest.json` and `SHA256SUMS` from the release identified
   in the pilot instructions. Verify the authenticated release checksum
   manifest, then the XML checksum (`shasum -a 256 -c SHA256SUMS` on macOS,
   `sha256sum -c SHA256SUMS` on Linux). A checksum downloaded alongside the XML
   detects corruption; it does not independently authenticate the publisher.
   Check the manifest's supported platform, app version and worker version.
2. Use a non-production instance on the tested ServiceNow release. Have the
   instance administrator confirm custom-table/application entitlement and
   CMDB/IRE availability. Stop if `x_664635_topo` already exists with an unknown
   origin or was installed through Application Repository/Store. Do not mix
   delivery mechanisms or rename the scope.
3. Open **System Update Sets → Retrieved Update Sets → Import Update Set from
   XML**, choose the downloaded XML, and upload it. Open the retrieved set and
   select **Preview Update Set**.
4. Review every preview problem with the administrator. Resolve missing
   dependencies and investigate collisions against the release inventory;
   do not bulk-ignore errors or accept an unrelated scope/global change.
   Preserve the preview report. Commit only after review, then inspect the
   commit result and application version. A successful upload is not a
   successful installation.
5. Follow [customer-owned identities and OAuth](pilot-quickstart.md#2-create-the-least-privilege-servicenow-identities),
   create the pool/target/credential/binding/profile, install the worker through
   Homebrew or APT/RPM, and run `topo worker check` before starting discovery.
   The XML supplies application definitions, not users, grants, tokens,
   credentials, targets or discovery data.

Before discovery, the instance administrator must register the exact choice
value `Nischoy Topo` on `cmdb_ci.discovery_source`, as described in
[IRE prerequisites](servicenow.md). This customer-owned global configuration
is not included in the scoped XML. A missing choice causes IRE to reject the
payload with `INVALID_INPUT_DATA`, even when native IRE access works.

ServiceNow documents [application publication to an update set](https://www.servicenow.com/docs/r/application-development/t_PublishApplicationsToAnUpdateSet.html)
and [Retrieved Update Sets import, preview and commit](https://developer.servicenow.com/blog.do?p=/post/backup-your-pdi/).
Those mechanisms do not establish licensing entitlement, Store certification
or unrestricted commercial distribution rights. The platform documentation
was checked against Australia on 2026-09-20; target-release compatibility still
requires real tests.

## Upgrades and recovery

Retain the XML, manifest, checksum, preview/commit records and installed version
for every deployment. Use the same scope and XML delivery for all pilot
upgrades. ServiceNow explicitly warns against [mixing update sets and
Application Repository](https://www.servicenow.com/docs/r/application-development/application-repository-self-hosted/manage-apps.html).
The private repository shares applications within one organization; it is not
a marketplace for Nischoy's unrelated customers.

Before an upgrade, disable customer schedules, stop workers and let active
attempts finish or expire. Take a customer-approved instance recovery point
covering application configuration, operational data and protected credentials.
Preview the next supported version in a representative non-production clone,
review any customer customization collisions, commit, and verify preserved
pools, profiles, schedules, credentials and run summaries before restarting.
A full application snapshot must not be assumed to propagate deletions of
older metadata: any removed file needs a separately reviewed migration and
real upgrade evidence. Never import an older XML as an assumed rollback.

[Update-set backout](https://www.servicenow.com/docs/r/application-development/system-update-sets/t_BackOutUpdateSet.html)
requires separate analysis; it is not a transactional database restore or an
uninstaller. Deleting a retrieved update-set record does not remove its
committed application. Dropping app tables may destroy pilot history and
credentials and does not undo IRE changes in CMDB. Removal therefore begins
with disabling schedules, stopping workers, revoking OAuth and deactivating
credentials; app/table removal and CMDB retention are administrator decisions.
No automated removal or lossless backout is currently validated.

## Maintainer export and release process

Fluent remains authoritative. Build the pinned source with the locked SDK in
a clean disposable workspace, using the [developer installer](../integrations/servicenow/topo-control-plane/README.md#install).
Do not reuse mutable `dist`, `target` or downloaded metadata from an earlier
instance session. Existing local generated output includes elective API policy
records; those are not customer distribution inputs.

1. Pin the source commit, SDK 4.9.0, application version and stable Fluent IDs.
   Run the source/build/package tests. Obtain approval before installing or
   updating the export instance, changing app permissions, or publishing a set.
   Confirm installed metadata matches that build and no local changes or
   elective instance records are included. A version match alone is insufficient.
2. Use the platform's documented **My Company's Applications → In Development
   → Nischoy Topo → Publish to Update Set** action. Specify the source app's
   version; record the resulting set ID and source instance's platform build.
   This is a packaging action, not manual application development. Prefer an
   official supported API/SDK equivalent if one is established for the target
   release; the pinned SDK's build/pack/download operations alone do not prove
   this platform publication step.
3. Open that completed set and use **Export to XML**, following the
   [platform export procedure](https://www.servicenow.com/docs/r/application-development/system-update-sets/t_SaveAnUpdateSetAsAnXMLFile.html).
   Retain the original export privately and unchanged. Inspect it offline:

   ```sh
   python3 scripts/package-servicenow-update-set.py /private/path/export.xml
   ```

   The checker supports the observed Australia export envelope, including nested
   choices/documentation and identity-bound translation cleanup. It rejects
   unfamiliar containers or record types rather than guessing. A format change
   requires a sanitized fixture, review and tests. It never converts SDK XML
   into an update set, redacts an export in place or calls ServiceNow.
4. Review every payload against the pinned generated definitions and source,
   including references in records without `sys_scope`. Account for all twelve
   tables, five roles, role inheritance, 37 ACLs and their mappings, seven REST
   routes, scripts, indexes, choices and navigation. Review all executable
   metadata and defaults for embedded values; reject unexpected records rather
   than deleting them from the XML. Regenerate on an approved clean export
   instance if the source is contaminated. No OAuth entity/policy, user,
   user-role assignment, Password2 value, operational row, attachment, test
   fixture, source-instance endpoint or runtime access grant may travel.
   Do not repair a contaminated export by editing `sys_update_xml` payloads,
   deleting selected Customer Updates, or moving their Update Set references.
   [ServiceNow's update-set guidance](https://www.servicenow.com/docs/r/application-development/system-update-sets/using-system-update-sets.html)
   prohibits changing that reference; its system-update-set documentation also
   advises against direct Customer Update edits. Clean the authoring source
   under an approved, backed-up procedure, then publish a new platform set.
   Source cleanup can affect live layouts and access grants, so review the
   exact records and cascading children before approving it.

5. Record the reviewed SHA-256, then seal those exact bytes into a new private
   candidate directory (replace the placeholders with the recorded values):

   ```sh
   python3 scripts/package-servicenow-update-set.py /private/path/export.xml \
     --review-sha256 <reviewed-64-character-sha256> \
     --source-commit <40-character-source-commit> \
     --output /private/path/new-candidate
   ```

   This produces a versioned XML, inventory manifest and checksums without
   overwriting an existing directory. The digest argument records a review
   assertion; it is not evidence that ServiceNow generated the bytes. Given
   identical inputs, packaging is deterministic. Separate platform exports may
   vary in IDs/timestamps; preserve their bytes instead of normalizing them.
6. Complete the real-instance matrix below. Record evidence bound to the XML
   digest and source commit, not just the version string. Only then prepare a
   **new** app-distribution release with compatibility notes and authenticated
   checksums/provenance under reviewed release controls. Release automation for
   that publication remains pending; this local tool creates candidates only.
   Never replace or append a substitute for beta.1's existing SDK artifact.

## Build-pipeline automation — feasibility checked 2026-10-03

**Status: unattended publication/export is not established.** The SDK build,
metadata checks and exact-byte candidate packager already run locally; they do
not replace the platform publication step. A customer XML must not be fabricated
from SDK files or mislabeled as a successful platform export.

The proposed pipeline has four stages:

| Trigger | Work | Artifact boundary |
| --- | --- | --- |
| Pull request | Compile and test the app; check stable metadata identities | SDK/build evidence only |
| Relevant app change merged to main | Deploy the exact source revision to an isolated packaging instance; publish and export through a validated platform mechanism; inspect the export | Private candidate XML, source/version manifest and checksums |
| Release candidate | Test those exact bytes on a separate acceptance instance: clean install, repeat, upgrade preservation and discovery/security/recovery gates | Acceptance evidence bound to XML digest |
| Approved app release | Promote the already-tested bytes with authenticated checksums/provenance and compatibility notes | Immutable GitHub Release assets |

Documentation-only changes need no new XML. Worker-only releases may reference
an existing compatible app package. Source revision and artifact identity must
be explicit even when the app version has not changed; customer versions must
not be silently replaced. Serialize packaging-instance operations so two builds
cannot export each other's installed metadata. Do not rebuild after acceptance.

### What was checked

- Pinned SDK 4.9.0 packaging produces the SDK ZIP; its inspected command surface
  does not establish unattended application-to-update-set publication.
- The [newer SDK CLI's `cicd publish`](https://servicenow.github.io/sdk/4.13.0/cli#cicd-publish)
  targets the Application Repository, not customer update-set XML. Upgrading the
  SDK alone is not evidence that this export requirement is solved.
- The official [publication procedure](https://www.servicenow.com/docs/r/application-development/t_PublishApplicationsToAnUpdateSet.html)
  describes the platform UI. The reviewed [CI/CD API](https://www.servicenow.com/docs/r/api-reference/rest-apis/cicd-api.html)
  did not establish an equivalent standalone XML publication/export contract.
  This is a bounded research finding, not a claim that no supported option exists.
- On the existing Australia packaging instance, read-only inspection confirmed
  app 0.4.6 and the native UI implementation. Publication uses
  `com.snc.apps.AppsAjaxProcessor`; export uses the platform's `UpdateSetExport`
  action followed by its download processor. These internal implementation
  details are not treated as a supported REST contract.
- The protected SDK OAuth identity read the actual publication form successfully
  (HTTP 200, publication form present, no login form). A single bounded POST to
  the native `createUpdateSet` action returned HTTP 401. A subsequent metadata
  query confirmed no probe update set existed. No publication or export occurred.
  An unrelated `/api/sn_cicd/version` probe returned an unknown-resource response;
  that does not establish plugin availability or the absence of other routes.

The read-only and publication status evidence is retained privately under
`dist/servicenow-xml-automation/`. No tokens, browser session material, raw
response bodies or instance credentials belong in workflow logs or artifacts.
The XML-installed acceptance instance and sealed 0.4.6 candidate were unchanged.

### Admin browser follow-up

After the owner signed in as admin, the normal publication UI succeeded on
2026-10-03. The standalone application form was used because its link did not
open the dialog inside the navigation frame. Version remained 0.4.6; Include
demo data was unchecked before submission. The resulting update set
`579831c893f78b50682e74dcebba101d` was Complete, created by admin, with 441
Customer Updates. The platform reported success in ten seconds. The native
Export to XML action produced a browser download event.

This proves publication through the signed-in admin browser. The current tool
interface did not provide a filesystem path for that download, so the new XML
bytes have not been inspected or sealed. It is not a new validated candidate
and does not replace the existing sealed 0.4.6 package. No browser credentials
were extracted, and the earlier OAuth-only 401 result remains a separate fact.
Unattended login, download capture and CI execution still require proof.

### Implementation boundary and next gate

Until the platform/authentication contract is established, retain an explicit
maintainer export handoff followed by the existing inspector and packager.
Do not add an every-merge workflow that reports an SDK ZIP or an old XML as a
fresh customer package. Public XML release wiring and the remaining acceptance
gates are still open.

For fully unattended generation, first establish either a supported publication
and download API with an authorized non-interactive identity, or a separately
validated browser runner using the normal publication/export UI and a dedicated
packaging account. The latter requires its own session/login lifecycle,
protected credential provisioning and failure/recovery testing; the current
interactive desktop browser is not a GitHub Actions authentication mechanism.
Do not disable CSRF/authentication checks, copy a personal browser session into
CI, or install a privileged custom export endpoint to bypass this limitation.

The export proof must produce a fresh platform set, explicitly exclude demo
and runtime data, preserve downloaded bytes, pass the inspector and source
comparison, and repeat without mixing builds. Only after that proof should the
merge-triggered candidate workflow be enabled. A failure after publication must
retain the set identity for inspection rather than blindly retrying creation.

## Dependency and access review

| Area | Required review and acceptance |
| --- | --- |
| Platform | First target is the export instance's Australia build; confirm target build and required APIs rather than claim all SDK-supported releases. |
| Scope | Preserve `x_664635_topo` / `d4e2151fdcbc7d97f8c155d1ba873e46`; reject collisions with an unrelated app. Prefix availability and customer entitlement remain external gates. |
| CMDB | Require computers, adapters, `Owns::Owned by`, and scoped `sn_cmdb.IdentificationEngine` preflight/apply. No direct CMDB writes or Discovery license/runtime assumption. |
| Native IRE access | Candidate 0.4.5 removes the invalid `sn_cmdb` scope/Script Include privilege. Verify native scoped IRE preflight/apply on the target; do not invent an application reference or blanket-approve runtime requests. |
| ACLs | Preserve viewer/operator/credential-admin/admin inheritance, a separate worker role, no worker table grants, credential-admin protection and exact REST authentication/ACL links. Verify with real identities. |
| Password2 | Verify `u_password` is encrypted, mandatory, non-audited/non-replicated, with restricted web-service access. Customer enters the value after installation; no encrypted value is exported. |
| OAuth | Customer creates its own worker identity, client, token and exact seven POST-resource policies. Test denial of generic Table, credential-table, CMDB and direct IRE access. |
| Scheduled scripts | Source defines two active internal maintenance/enqueue jobs. With zero customer operational records there must be no discoverable work; customer schedules remain inactive until validated. |
| Other dependencies | Inspect SDK-generated modules and any attachment/source-map dependency against the export. Unknown record types or missing dependencies block release; do not relax the allowlist just to import. |

## Export evidence — 2026-09-20 UTC

With owner approval, SDK 4.9.0 installed source commit
`27107afcb5177b6eb1ad72db3f5dbaa7a7304128` as application 0.4.4 on
`dev394887` (Australia Patch 3, build
`glide-australia-02-11-2026__patch3-05-25-2026`). The owner confirmed this
instance replaces the unavailable `dev441060`. OAuth used the official SDK
protected credential store and the built-in browser.

The platform **Publish to Update Set** action succeeded with **Include demo
data unchecked**. Completed local set `464462ff93d74f10682e74dcebba1050`
exported remote set `58c4a6ff93d74f10682e74dcebba1085`, whose exported state is
`loaded` (the local set is `complete`). The unchanged 1,292,986-byte XML has
SHA-256 `f5aff043aed913002cacb38c04d74944cc0a5d5ddd9bfd6a5a113a1a4f433cdd`.

Its 442 updates comprise the application, 12 tables, 143 dictionary entries,
143 documentation entries, 10 choice sets, 37 ACLs and 37 ACL-role mappings,
5 roles and 4 inheritance mappings, 12 modules and 1 menu, 4 business rules,
3 script includes, 2 scheduled scripts, 2 UI actions and 2 role mappings,
7 REST resources with their API/version, 1 cross-scope privilege, 2 SDK JSON
modules and 12 platform table-licensing configurations. The latter are scoped
to the twelve Topo tables and carry no license-role or condition grants; their
`none` value does not establish customer entitlement.

Offline comparison found no differences in 48 script fields, 7 REST script
bodies, 7 authentication and 7 ACL-authorization flags, 131 calculations and
defaults, and 143 dictionary types/attributes against generated source. Platform
normalization includes dictionary IDs, empty booleans and truncated display
labels; this is not evidence of stable IDs across an XML import. SDK modules
contain package metadata and an empty-dependency SBOM. No OAuth/policy, user,
user-role assignment, operational-row or attachment update types occur, and
neither source-instance hostname occurs in the XML. Translation cleanup is
limited to the exact app/business-rule document identity.

The local candidate contains unchanged XML, inventory manifest and checksums;
checksum verification passed. It is private and marked offline-candidate-only.
Existing beta assets were not changed. Ten synthetic format/security tests now
cover observed nesting and rejection of foreign tables and widened deletion
queries. These checks and the real export do not establish installation, ACL
enforcement, customer entitlement, or upgrade compatibility.

## First clean-instance preview — failed

The owner approved using a full reset of `dev394887` in place of a second
simultaneous PDI. After reset, no Topo scope or retrieved update sets existed.
The saved XML imported with 442 updates, but preview reported one error and
zero warnings: the cross-scope privilege references missing `target_scope`
`sn_cmdb`. A read-only check found no corresponding scope or Script Include.
The [official API documentation](https://www.servicenow.com/docs/r/api-reference/server-api-reference/IdentificationEngineScopedAPI.html)
describes `sn_cmdb` as an API namespace; do not assume it identifies a scoped
application record. No commit or error override was performed. The source
permission declaration needs investigation and correction followed by a new
platform export and clean preview. The original candidate is retained as
failed-test evidence and must not be distributed.

## Corrected platform export — 0.4.5

On 2026-09-22, SDK 4.9.0 installed the clean tracked-source 0.4.5 build from
`ba5fc765c02d359bd584335222d6225877c271f8` on `dev394887`. Platform publication
with demo data unchecked produced completed set
`cf74c1b09327c310682e74dcebba10f0`, exported as
`6ea405b09327c310682e74dcebba10d1`: 441 updates, 1,290,748 bytes, SHA-256
`dad292dc3c7395c6d8d27e7da068a1318fd854776aca8f2f9577280c5e6f7981`.
The offline inspector passes with no cross-scope privilege. Comparisons match
48 script fields, seven REST operation bodies and authentication flags, and
143 dictionary defaults/calculations against the generated source. The exact
XML, manifest and checksum are preserved privately under
`dist/servicenow-0.4.5-xml-candidate`; this is not a public release.

A background diagnostic in `x_664635_topo` invoked the documented native
`identifyCIEnhanced` method. Using the existing `ServiceNow` source choice
returned proposed `INSERT`, zero errors/warnings and empty committed-item
arrays. Using `Nischoy Topo` correctly failed because reset removed that
required source choice. This proves native preflight access without the
fabricated privilege; it does not prove Topo apply or end-to-end worker flow.
The diagnostic did not call a CMDB write API; the failed attempt created one
IRE context record. Clean XML preview/commit, repeat and upgrade remain open.

## Customer-instance acceptance matrix — completion pending

### Azure Linux pilot finding — 2026-09-27

The XML-installed 0.4.5 application accepted the customer-owned OAuth worker's
registration, heartbeat, task claim, attempt-bound Password2 retrieval and
15,465-byte result upload. Manual run `8246415693670f10682e74dcebba10c6`
then failed before IRE with `asset attribute is invalid or too deeply nested`.
The broker access event reports `Allowed` / `attempt_bound`; this proves the
successful broker path, not the remaining denial matrix. The failed raw result
has a seven-day expiry, but actual retention deletion remains untested here.

The prior private Linux fixture contains 658 package names and 158 services.
A sanitized regression reproduces the same rejection because 0.4.5 limits all
attribute arrays to 256 entries. Candidate 0.4.6 allows only top-level host
`packages`/`services` to carry up to 4,096 strings; generic and nested arrays
retain the existing limits, as do the result byte cap and strict IRE mappings.
Tests cover both boundaries, malformed entries and absence of inventory lists
from IRE fields. No 0.4.6 platform XML has been exported or installed yet.

Preserve the original 0.4.5 XML and this failed-run history. Before applying the
fix, finish the identical-XML recommit preservation check; then use the reviewed
0.4.6 source with stable metadata IDs for a separately authorized platform
export and versioned XML upgrade. Do not edit the installed Script Include
ad hoc or count a source installation as XML-upgrade evidence. The offline
candidate inspector now expects 0.4.6; the original 0.4.5 bytes and their
previous verification remain historical evidence. Public XML publication and
successful manual/scheduled/IRE/retention acceptance remain pending.

### Recorded clean-install and repeat-preview evidence

The corrected XML passed a clean installation on the owner-reset `dev394887`
on 2026-09-22 (instance-displayed time), Australia Patch 3 build
`glide-australia-02-11-2026__patch3-05-25-2026`. Before import, both the Topo
scope lookup and retrieved-update-set list were empty. Preview succeeded in
24 seconds with 441 inserts, zero updates/deletes/collisions and no preview
problem list. The separate subscription-mapping advisory was retained.
Commit succeeded in one minute, with displayed commit time `22:26:37`.
Post-commit checks found version 0.4.5, 12 tables, five roles, 37 ACLs, seven
REST operations, three Script Includes and zero cross-scope privileges.
Read-only checks in the Topo scope found all 12 operational tables empty.
A preliminary check during commit was incomplete and Global reads were
denied; only the post-commit metadata and in-scope checks count as evidence.

Re-importing the identical XML reused the remote set ID and reset its state
to Loaded, clearing its displayed commit date. Repeat preview succeeded in
18 seconds with 441 updates and zero inserts/deletes/collisions. No second
commit was performed. This establishes repeat upload/preview behavior, not
preservation of customer data through recommit or a version upgrade.

On 2026-09-23, unauthenticated POSTs with empty JSON to all seven installed
version-1 worker routes returned HTTP 401. Per-task routes used a nonexistent
all-zero task ID. This checks the unauthenticated boundary only; role-specific
ACL/Password2 and customer-owned OAuth denial tests remain pending.

After explicit owner approval on 2026-09-23, five passwordless, login-locked
test identities exercised `canRead`, `canCreate`, `canWrite` and `canDelete`
under administrator impersonation on initialized records (no record inserts
or credential values). None had the admin role. The resulting ACL decisions:

| Identity | SSH credentials | Credential bindings | Access events | Tasks |
| --- | --- | --- | --- | --- |
| Credential administrator | Read/create/write/delete | Read/create/write/delete | Read only | Read only |
| Operator | Denied | Read only | Denied | Read only |
| Viewer | Denied | Read only | Denied | Read only |
| Worker | Denied | Denied | Denied | Denied |
| No Topo role | Denied | Denied | Denied | Denied |

All five users were subsequently deactivated through the user forms and the
list verified `Active=false` for each. Their login locks were retained; no
passwords, email addresses or OAuth clients were supplied. Platform user
creation also generated default notification-device/group/inherited-role
records. The fixture users are retained inactive for audit, not deleted.
This proves the initialized-record ACL decision matrix; it does not yet
prove protected-value storage/retrieval, actual CRUD enforcement, OAuth policy,
credential broker behavior or end-to-end worker discovery after XML install.

Use an authorized non-production instance with no Topo scope and no prior
App Repository install. A separately approved full reset may supply that
baseline when only one PDI is available; preserve the source/export first and
obtain explicit approval at the irreversible reset step.

- **Clean install:** record platform build, source/export identities, XML hash,
  empty-scope baseline, preview problems/resolutions and commit completion.
  Verify complete app inventory and absence of users, grants, credentials and
  operational data before customer setup.
- **Functional/security:** create disposable customer-owned identities and
  Password2 data, prove ACL and OAuth denial boundaries, run worker preflight,
  then approved manual and scheduled SSH discovery and repeat IRE reconciliation.
- **Repeat:** re-import the identical XML; record the platform's actual
  duplicate/skipped/preview behavior, verify stable metadata identities and
  unchanged customer data. Do not predeclare this a no-op.
- **Upgrade:** use a separately versioned Fluent change with stable metadata
  IDs; export through the same platform path. Prove preserved customer
  configuration, credentials and run summaries, recheck ACL/OAuth behavior and
  document any deletion/migration semantics. A repeated 0.4.4 import alone is
  not upgrade evidence.
- **Recovery/removal:** exercise the agreed recovery procedure on disposable
  infrastructure; distinguish stop/revoke from destructive app removal and
  instance restore. Record any limitation before recommending it to customers.

Local parser fixtures prove only offline rejection and byte preservation.
Earlier SDK upgrades on `dev441060` and simulator scale tests do not satisfy
this matrix. Source installation and export are approved and completed; a
separate authorized clean instance is still required.

## 0.4.6 clean and repeat installation — dev317694

On 2026-10-03 UTC, the unchanged 0.4.6 XML with SHA-256
`c9fdbb0c73986da0528679e828afd610b3c8e404d4fe0c494cb378f3679defec`
was imported into dev317694 after confirming no Topo scope or retrieved sets.
Preview reported 441 inserts and zero updates, deletes or problems. Commit
completed successfully. Read-only verification found version 0.4.6, twelve
tables, five roles, 37 ACLs, seven REST routes and three Script Includes.

An identical-file reimport preview reported 441 updates, zero inserts/deletes
and no problems. Its second commit succeeded, with all 440 application-file
identities and classes unchanged. This proves this artifact's clean and repeat
installation on this instance; it does not prove the still-pending
0.4.5-to-0.4.6 upgrade or broader discovery/security acceptance.

## 0.4.5-to-0.4.6 upgrade — dev317694

On 2026-10-03 UTC, the actual upgrade of installed 0.4.5 to the exact sealed
0.4.6 XML passed on Australia Patch 3, build
`glide-australia-02-11-2026__patch3-05-25-2026`. Preview reported 441 updates,
zero inserts/deletes and no problems; commit completed. Version, twelve tables,
five roles, 37 ACLs, seven REST routes and three Script Includes were verified.
The installed mapper script exactly matched the reviewed 0.4.6 XML.

An inactive worker pool, inactive local discovery profile and inactive future
schedule retained their three record IDs and all 23 captured field values,
including references, revision, limits and next-run time. No discovery ran.
This preservation evidence does not cover Password2 credentials or run history.

Test setup required removing the prior disposable 0.4.6 installation before
installing 0.4.5. ServiceNow retained 427 local deletion updates; each baseline
preview collision was reviewed against the original scoped XML and DELETE
record before accepting the incoming update through the platform UI. This is
baseline-reset handling, not an error-free older-version clean-install claim
or a customer rollback procedure. The subsequent actual upgrade had no
preview conflicts. The subsequent Linux workflow evidence follows below;
broader credential/security and recovery tests and public distribution
approval remain outstanding.

## Real Azure Linux workflow — dev317694

On 2026-10-03 UTC, the XML-installed 0.4.6 app and unchanged published
`v0.1.0-beta.1` Linux worker completed two independent manual scans and one
automatically scheduled scan of one approved Azure Linux target. SSH host keys
matched the independently Azure-verified fixture; the scan identity remained
non-admin. Each run completed one task in one attempt, reporting three assets,
two relationships and zero collection errors. All three broker events were
`Allowed` / `attempt_bound`, and all IRE deliveries had clean preflight/apply.

| Execution | IRE operations | CMDB result |
| --- | --- | --- |
| First manual | Five `INSERT` | One computer, two adapters, two ownership relationships |
| Independent manual repeat | Three `UPDATE`, two `NO_CHANGE` | Same three CI IDs and two relationship IDs |
| Automatic schedule | Three `UPDATE`, two `NO_CHANGE` | Same three CI IDs and two relationship IDs |

Read-only CMDB snapshots started with zero Topo-source computers/adapters and
verified unchanged captured mapped fields and identities after both later
runs. This proves no duplicate CIs or relationships for this fixture; `UPDATE`
must not be described as an all-`NO_CHANGE` result. The five-minute schedule
fired at 05:05:01 UTC, completed at 05:05:11 and advanced its next-run time to
05:10:00. No manual scheduler invocation was used. The schedule was then
disabled and the temporary worker stopped successfully; the packaged service
remains inactive.

Customer-owned setup used a dedicated worker user, restricted OAuth client,
seven exact POST/v1 scopes and seven matching access policies with all wildcard
flags false. All seven empty-body route checks reached Topo validation (400),
while an unrelated Table API returned 401. This is a focused OAuth/functional
check, not the full role/Password2 security matrix. Generic web-service access
to operational and credential tables remains disabled.

Private evidence is under `dist/servicenow-0.4.6-discovery/`: run/IRE summaries,
worker logs, OAuth assertions, CMDB snapshots and identity/value comparisons.
No lab addresses, credentials or raw observations are included in this public
report. This closes the real Linux mapper retest and manual/repeat/scheduled
workflow gates. Broader security, credential/run-history upgrade preservation,
recovery, volume and public-distribution gates remain separate.
