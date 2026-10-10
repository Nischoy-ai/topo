# Install the Topo application from XML

[Download Topo 0.4.6 Beta](https://github.com/Nischoy-ai/topo/releases/tag/servicenow-0.4.6-beta.2).
The package contains one native XML batch with the application and all 32
required indexes (17 unique) and discovery-source setup. Check [worker availability](distribution.md#release-availability)
before installing a worker; the XML release does not publish a worker binary.

## Customer installation

Install the combined package:

1. Download the ZIP, `SHA256SUMS` and `SHA256SUMS.sigstore.json`. Verify the
   [checksum-manifest signature](releases.md#verify-the-servicenow-package).
   Unzip in the same directory and retain the ZIP. On macOS run
   `shasum -a 256 -c SHA256SUMS`; on Linux run `sha256sum -c SHA256SUMS`.
   Check component compatibility in the release manifest.
2. As administrator on a development instance, open **System Update Sets →
   Retrieved Update Sets → Import Update Set from XML**. Upload
   `nischoy-topo-0.4.6-combined.xml`.
3. Open the imported **Nischoy Topo** base set and select **Preview Update Set
   Batch**. It must show **474 Customer Updates in Batch**: 441 app updates
   and the **Nischoy Topo — Database Indexes** child with 32 indexes and one
   discovery-source setup script. Review any preview problems individually.
4. Select **Commit Update Set Batch**. Wait for completion, confirm both sets
   are committed, check application version **0.4.6**, and verify the
   [32-index checklist](servicenow-index-setup.md). Do not drop indexes or
   separately import the companion as part of this installation.
5. Open **System Definition → Fix Scripts → Nischoy Topo — Register Discovery
   Source**. Keep **Unloadable** cleared, then select **Run Fix Script →
   Proceed**. It creates the source when absent or preserves an existing active
   choice. Review inactive or duplicate matching choices before discovery.
6. Configure [identities, credentials and targets](pilot-quickstart.md#2-create-the-least-privilege-servicenow-identities),
   install the worker and run `topo worker check` before starting discovery.

Confirm custom-table/application entitlement and CMDB/IRE availability with
the instance administrator. Stop if the existing `x_664635_topo` app has an
unknown origin or came from Application Repository/Store; do not mix delivery
mechanisms. The XML supplies definitions, not users, grants, tokens,
credentials, targets or discovery data. Keep workers and schedules inactive
until installation checks pass.

ServiceNow documents [application publication to an update set](https://www.servicenow.com/docs/r/application-development/t_PublishApplicationsToAnUpdateSet.html)
and [Retrieved Update Sets import, preview and commit](https://developer.servicenow.com/blog.do?p=/post/backup-your-pdi/).
Confirm licensing and application entitlement with your instance administrator.

## Application identity and CMDB prerequisite

The application name is **Nischoy Topo**, with scope `x_664635_topo` and
application ID `d4e2151fdcbc7d97f8c155d1ba873e46`. Retain both for every XML
upgrade; customers must not rename the scope or install a recreated app under
a different prefix. ServiceNow documents how the
[namespace identifies scoped application files](https://www.servicenow.com/docs/r/application-development/c_ApplicationScope.html).
This package uses XML update-set delivery outside the ServiceNow Store.
Vendor display fields do not establish Store certification or namespace ownership.

The packaged Fix Script registers the exact **Nischoy Topo** choice on
**Configuration Item [cmdb_ci] → discovery_source**. Run it after committing
the XML batch; automatic execution on XML commit is not assumed. It preserves
existing active choices and leaves unrelated sources and translations intact.
No hand-entered CMDB choice is needed for this package. Direct publishers
without this package follow [IRE prerequisites](servicenow.md#discovery-source-registration).

## Upgrades and recovery

Retain the XML, manifest, checksum, preview/commit records and installed version
for every deployment. Use the same scope and XML delivery for upgrades. ServiceNow explicitly warns against [mixing update sets and
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
committed application. Dropping app tables may destroy discovery history and
credentials and does not undo IRE changes in CMDB. Removal therefore begins
with disabling schedules, stopping workers, revoking OAuth and deactivating
credentials; app/table removal and CMDB retention are administrator decisions.
No automated removal or lossless backout is currently validated.

## Validation records

Dated installation, repeat-import and upgrade results are kept in the
[package validation record](servicenow-validation.md).
