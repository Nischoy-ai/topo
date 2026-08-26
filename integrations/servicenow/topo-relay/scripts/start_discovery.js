(function startTopoDiscovery() {
    'use strict';

    if (String(current.u_active) !== 'true') {
        gs.addErrorMessage('The Topo discovery profile is inactive.');
        action.setRedirectURL(current);
        return;
    }
    var relay = current.u_relay.getRefRecord();
    if (!relay.isValidRecord() || String(relay.u_active) !== 'true') {
        gs.addErrorMessage('The profile does not have an active Topo Relay.');
        action.setRedirectURL(current);
        return;
    }
    var outstanding = new GlideRecord('x_nischoy_topo_job');
    outstanding.addQuery('u_profile', current.getUniqueValue());
    outstanding.addQuery('u_state', 'IN', 'queued,running');
    outstanding.setLimit(1);
    outstanding.query();
    if (outstanding.next()) {
        gs.addInfoMessage('A discovery job is already queued or running for this profile.');
        action.setRedirectURL(outstanding);
        return;
    }
    var job = new GlideRecord('x_nischoy_topo_job');
    job.initialize();
    job.u_job_id = gs.generateGUID();
    job.u_relay = relay.getUniqueValue();
    job.u_profile = current.getUniqueValue();
    job.u_state = 'queued';
    var sysID = job.insert();
    gs.addInfoMessage('Topo discovery job queued.');
    action.setRedirectURL(job.get(sysID) ? job : current);
})();
