(function cancelTopoRun() {
    'use strict';
    try {
        var state = new TopoControlPlane().cancelRun(String(current.getUniqueValue()));
        gs.addInfoMessage('Topo run cancellation state: ' + state);
    } catch (error) {
        gs.addErrorMessage('Topo run could not be cancelled: ' + String(error && error.message ? error.message : error).substring(0, 1000));
    }
    action.setRedirectURL(current);
})();
