(function validateTopoCredentialBinding() {
    'use strict';
    var invalid = !/^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/.test(String(current.u_binding_id)) ||
        parseInt(current.u_revision, 10) < 1 || String(current.u_protocol) !== 'ssh_password' ||
        !/^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/.test(String(current.u_profile_id)) ||
        parseInt(current.u_profile_revision, 10) < 1 || !String(current.u_target_scope) || !String(current.u_credential);
    var scope = current.u_target_scope.getRefRecord();
    var credential = current.u_credential.getRefRecord();
    if (invalid || !scope.isValidRecord() || !credential.isValidRecord() ||
            !new TopoControlPlane()._isTrue(scope.u_active) || !new TopoControlPlane()._isTrue(credential.u_active)) {
        gs.addErrorMessage('Topo credential binding must reference an active target scope and Password2 SSH credential.');
        current.setAbortAction(true);
        return;
    }
    if (!current.isNewRecord() && (current.u_binding_id.changes() || current.u_revision.changes() ||
            current.u_protocol.changes() || current.u_profile_id.changes() || current.u_profile_revision.changes() ||
            current.u_target_scope.changes() || current.u_credential.changes())) {
        gs.addErrorMessage('Credential-binding authority fields are immutable; create a new revision.');
        current.setAbortAction(true);
    }
})();
