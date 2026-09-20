# Install the Topo pilot application from XML

**Status: staged, not yet validated or available for download.** The published
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

   The checker is provisional pending a genuine export. It deliberately rejects
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
| Cross-scope | Source defines only execute access to `sn_cmdb.IdentificationEngine`; inspect resolved target scope and any requested runtime privileges on the clean instance. Do not blanket-approve requests. |
| ACLs | Preserve viewer/operator/credential-admin/admin inheritance, a separate worker role, no worker table grants, credential-admin protection and exact REST authentication/ACL links. Verify with real identities. |
| Password2 | Verify `u_password` is encrypted, mandatory, non-audited/non-replicated, with restricted web-service access. Customer enters the value after installation; no encrypted value is exported. |
| OAuth | Customer creates its own worker identity, client, token and exact seven POST-resource policies. Test denial of generic Table, credential-table, CMDB and direct IRE access. |
| Scheduled scripts | Source defines two active internal maintenance/enqueue jobs. With zero customer operational records there must be no discoverable work; customer schedules remain inactive until validated. |
| Other dependencies | Inspect SDK-generated modules and any attachment/source-map dependency against the export. Unknown record types or missing dependencies block release; do not relax the allowlist just to import. |

## Real evidence matrix — all pending

Use a separate authorized non-production instance with no Topo scope and no
prior App Repository install. Do not reset the existing developer instance to
simulate a clean customer installation.

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
this matrix. Source/export approval and separate-instance access are pending.
