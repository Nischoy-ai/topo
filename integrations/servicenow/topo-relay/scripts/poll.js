(function process(request, response) {
    'use strict';

    var body = request.body.data || {};
    var safeID = /^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/;
    if (body.schema_version !== 'v1alpha1' || !safeID.test(String(body.relay_id || ''))) {
        response.setStatus(400);
        response.setBody({error: 'invalid relay poll request'});
        return;
    }
    if (!Array.isArray(body.profiles) || body.profiles.length < 1 || body.profiles.length > 128) {
        response.setStatus(400);
        response.setBody({error: 'profiles must contain between 1 and 128 entries'});
        return;
    }
    for (var i = 0; i < body.profiles.length; i++) {
        var capability = body.profiles[i] || {};
        if (!safeID.test(String(capability.id || '')) || ['local', 'ssh-linux'].indexOf(String(capability.plugin || '')) < 0) {
            response.setStatus(400);
            response.setBody({error: 'invalid profile capability'});
            return;
        }
    }

    // Bind the caller's OAuth identity to one configured Relay record. A
    // token issued to one Relay cannot claim another Relay's jobs merely by
    // changing relay_id in the request.
    var relay = new GlideRecord('x_nischoy_topo_relay');
    relay.addQuery('u_relay_id', String(body.relay_id));
    relay.addQuery('u_service_user', gs.getUserID());
    relay.addQuery('u_active', true);
    relay.setLimit(1);
    relay.query();
    if (!relay.next()) {
        response.setStatus(403);
        response.setBody({error: 'relay identity is not registered for this user'});
        return;
    }
    if (String(relay.u_site_id) !== String(body.site_id || '')) {
        response.setStatus(409);
        response.setBody({error: 'relay site does not match its registered site'});
        return;
    }
    relay.u_last_seen = new GlideDateTime();
    relay.u_version = String(body.version || '').substring(0, 64);
    relay.u_profiles = JSON.stringify(body.profiles).substring(0, 4000);
    relay.update();

    var jobs = [];
    var job = new GlideRecord('x_nischoy_topo_job');
    job.addQuery('u_relay', relay.getUniqueValue());
    job.addQuery('u_state', 'queued');
    job.orderBy('sys_created_on');
    job.setLimit(1);
    job.query();
    if (job.next()) {
        var profile = job.u_profile.getRefRecord();
        if (!profile.isValidRecord() || String(profile.u_active) !== 'true' || String(profile.u_relay) !== relay.getUniqueValue()) {
            job.u_state = 'failed';
            job.u_success = false;
            job.u_completed_at = new GlideDateTime();
            job.u_error = 'profile is inactive or assigned to another relay';
            job.update();
        } else {
            job.u_state = 'running';
            job.u_started_at = new GlideDateTime();
            job.update();
            jobs.push({
                job_id: String(job.u_job_id),
                type: 'discover',
                profile_id: String(profile.u_profile_id),
                requested_at: String(job.sys_created_on.getGlideObject().getValue()).replace(' ', 'T') + 'Z'
            });
        }
    }
    response.setStatus(200);
    response.setBody({jobs: jobs});
})(request, response);
