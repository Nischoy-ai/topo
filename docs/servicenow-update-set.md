# Install the Topo pilot application from XML

**Status: corrected 0.4.5 clean XML installation passed; broader pilot acceptance remains open.** The published
worker beta is `v0.1.0-beta.1`; it does not contain a customer update-set XML.
Do not rename its SDK ZIP or upload an arbitrary XML record export. A separate
clean-instance installation, repeat and upgrade test must pass before the
first XML candidate is offered to pilots.

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
