(function validateTopoSSHCredential() {
    'use strict';
    if (!/^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/.test(String(current.u_credential_id)) ||
            !/^[A-Za-z0-9._-]{1,64}$/.test(String(current.u_username))) {
        gs.addErrorMessage('Topo SSH credential ID or username is invalid.');
        current.setAbortAction(true);
    }
})();
