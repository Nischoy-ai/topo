(function runTopoNow() {
    'use strict';
    try {
        var runID = new TopoControlPlane().createRun(current.getUniqueValue(), 'manual', '');
        var run = new GlideRecord('x_664635_topo_run');
        run.addQuery('u_run_id', runID);
        run.setLimit(1);
        run.query();
        gs.addInfoMessage('Topo run ' + runID + ' is ready for a stateless worker.');
        action.setRedirectURL(run.next() ? run : current);
    } catch (error) {
        gs.addErrorMessage('Topo run was not created: ' + String(error.message || error).substring(0, 1000));
        action.setRedirectURL(current);
    }
})();
