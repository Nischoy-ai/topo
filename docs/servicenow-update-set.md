# Install the Topo application from XML

[Download ServiceNow 0.4.6 preview 2](https://github.com/Nischoy-ai/topo/releases/tag/servicenow-0.4.6-preview.2).
The package contains one native XML batch with the application and all 32
required indexes (17 unique). Compatible worker: **v0.1.0-beta.1**.

Use preview 2 for index-inclusive installation. Preview 1 is retained as an
older artifact and does not include the indexes. Start on a development
instance and follow your organization's change process before deployment.
Detailed test results are maintained in the [validation record](servicenow-validation.md).

## Customer installation

For combined preview 2 (preview 1 is a different package):

1. Obtain the package and matching checksum manifest. On macOS run
   `shasum -a 256 -c SHA256SUMS`; on Linux run `sha256sum -c SHA256SUMS`.
   Check the supported ServiceNow and worker versions. A checksum detects
   corruption; it does not independently authenticate the publisher.
2. As administrator on a development instance, open **System Update Sets →
   Retrieved Update Sets → Import Update Set from XML**. Upload
   `nischoy-topo-0.4.6-combined.xml`.
3. Open the imported **Nischoy Topo** base set and select **Preview Update Set
   Batch**. It must show **473 Customer Updates in Batch**: 441 app updates
   and one child set with 32 indexes. Review any preview problems individually.
4. Select **Commit Update Set Batch**. Wait for completion, confirm both sets
   are committed, check application version **0.4.6**, and verify the
   [32-index checklist](servicenow-index-setup.md). Do not drop indexes or
   separately import the companion as part of this installation.
5. Configure [identities, credentials and targets](pilot-quickstart.md#2-create-the-least-privilege-servicenow-identities),
   install the worker and run `topo worker check` before starting discovery.

Confirm custom-table/application entitlement and CMDB/IRE availability with
the instance administrator. Stop if the existing `x_664635_topo` app has an
unknown origin or came from Application Repository/Store; do not mix delivery
mechanisms. The XML supplies definitions, not users, grants, tokens,
credentials, targets or discovery data. Keep workers and schedules inactive
until installation checks pass.

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
