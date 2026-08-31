(function validateTopoTargetScope() {
    'use strict';
    try {
        new TopoControlPlane().compileTargetScope(current);
    } catch (error) {
        gs.addErrorMessage('Topo target scope is invalid: ' + String(error && error.message ? error.message : error).substring(0, 1000));
        current.setAbortAction(true);
        return;
    }
    if (!current.isNewRecord() && (current.u_scope_id.changes() || current.u_revision.changes() ||
            current.u_worker_pool.changes() || current.u_site_id.changes() || current.u_cidrs.changes() ||
            current.u_exclusions.changes() || current.u_ipv4_partition_prefix.changes())) {
        gs.addErrorMessage('Target-scope selection fields are immutable; create a new revision.');
        current.setAbortAction(true);
    }
})();
