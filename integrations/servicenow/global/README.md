# Global installation prerequisites

`register-discovery-source.js` is the source for the Global Fix Script
**Nischoy Topo — Register Discovery Source**. It adds one active English
`Nischoy Topo` choice for `cmdb_ci.discovery_source` when absent. Repeated
execution preserves an existing active choice, including its label. Duplicate
or inactive matching entries require administrator review. Other sources,
translations, dependent choices and domains are preserved.

## Native packaging

1. Select **Global** and the **Nischoy Topo — Database Indexes** child update set.
2. Create the Fix Script through **System Definition → Fix Scripts**. Paste
   the reviewed source verbatim, keep **Unloadable** and **Before** cleared,
   and retain **Record for rollback**. Capture the script definition in the
   child set with the 32 required indexes.
3. Complete both sets and use the base set's **Export Update Set Batch to XML**.
   Inspect the native export before installation or publication. It must retain
   the fixed application identity and all index definitions, with this one
   Global setup script as the only added update.
4. Reject a captured `sys_choice_cmdb_ci_discovery_source` whole-field payload.
   It includes existing source choices and must not be shipped to customers.
   ServiceNow captures a field's complete choice list as one
   [choice update](https://www.servicenow.com/docs/r/platform-administration/t_ViewChoiceListDefinitions.html).

For XML delivery, include a post-commit **Run Fix Script → Proceed** step in
the package's installation guide. Do not assume automatic execution: ServiceNow
documents automatic Fix Script execution for
[Application Repository and Store installations](https://developer.servicenow.com/blog.do?p=/post/training-fixscripts/).
Never enable **Unloadable** when running this script on a customer instance.

The currently published 473-update XML predates this setup script. Its
[installation guide](../../../docs/servicenow-update-set.md) describes that
package's manual registration requirement. This source does not mean a revised
customer XML has been published.

The revised native candidate passed import, preview, commit, exact-byte repeat,
source creation and preservation checks. See its separate
[validation record](../../../docs/servicenow-validation.md#revised-installation-metadata-2026-10-09)
before preparing a manual release. Keep its digest and evidence distinct from
the published 473-update package.

Run `node integrations/servicenow/global/register-discovery-source.test.js`
from the repository root. These contract tests do not replace native XML
preview, commit, repeat and preservation validation.
