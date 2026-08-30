(function validateTopoProfile() {
    'use strict';
    if (String(current.u_operation) !== 'local.v1' || String(current.u_schema_version) !== 'v1alpha1' ||
            parseInt(current.u_revision, 10) < 1) {
        gs.addErrorMessage('Slice A profiles must use schema v1alpha1 and the fixed local.v1 operation.');
        current.setAbortAction(true);
        return;
    }
    if (!current.isNewRecord() && (current.u_profile_id.changes() || current.u_revision.changes() ||
            current.u_operation.changes() || current.u_schema_version.changes() || current.u_worker_pool.changes())) {
        var run = new GlideRecord('x_664635_topo_run');
        run.addQuery('u_profile', current.getUniqueValue());
        run.setLimit(1);
        run.query();
        if (run.hasNext()) {
            gs.addErrorMessage('A profile revision referenced by a run is immutable; create a new revision.');
            current.setAbortAction(true);
        }
    }
})();
