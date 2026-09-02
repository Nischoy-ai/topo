(function validateTopoProfile() {
    'use strict';
    var operation = String(current.u_operation);
    var control = new TopoControlPlane();
    if (['local.v1', 'ssh_linux.v1'].indexOf(operation) < 0 || String(current.u_schema_version) !== 'v1alpha1' ||
            !/^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/.test(String(current.u_profile_id)) ||
            parseInt(current.u_revision, 10) < 1) {
        gs.addErrorMessage('Topo profile must use schema v1alpha1 and a reviewed operation.');
        current.setAbortAction(true);
        return;
    }
    if (operation === 'local.v1' && (String(current.u_target_scope) || String(current.u_credential_binding))) {
        gs.addErrorMessage('local.v1 cannot carry remote target or credential authority.');
        current.setAbortAction(true);
        return;
    }
    if (operation === 'ssh_linux.v1') {
        var scope = current.u_target_scope.getRefRecord();
        var binding = current.u_credential_binding.getRefRecord();
        if (!scope.isValidRecord() || !binding.isValidRecord() || !control._isTrue(scope.u_active) ||
                !control._isTrue(binding.u_active) || String(scope.u_worker_pool) !== String(current.u_worker_pool) ||
                parseInt(scope.u_ipv4_partition_prefix, 10) !== 32 || parseInt(scope.u_partition_count, 10) < 1 ||
                parseInt(scope.u_partition_count, 10) > control.MAX_SSH_TARGETS ||
                String(binding.u_protocol) !== 'ssh_password' ||
                String(binding.u_profile_id) !== String(current.u_profile_id) ||
                parseInt(binding.u_profile_revision, 10) !== parseInt(current.u_revision, 10) ||
                String(binding.u_target_scope) !== scope.getUniqueValue()) {
            gs.addErrorMessage('ssh_linux.v1 requires a matching active Password2 binding and a 1-1024 address /32 target scope in the same worker pool.');
            current.setAbortAction(true);
            return;
        }
    }
    if (!current.isNewRecord() && (current.u_profile_id.changes() || current.u_revision.changes() ||
            current.u_operation.changes() || current.u_schema_version.changes() || current.u_worker_pool.changes() ||
            current.u_target_scope.changes() || current.u_credential_binding.changes())) {
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
