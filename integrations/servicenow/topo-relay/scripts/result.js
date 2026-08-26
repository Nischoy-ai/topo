(function process(request, response) {
    'use strict';

    var body = request.body.data || {};
    var safeID = /^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/;
    if (body.schema_version !== 'v1alpha1' || typeof body.success !== 'boolean' || !safeID.test(String(body.relay_id || '')) ||
        !safeID.test(String(body.profile_id || '')) || !safeID.test(String(body.job_id || ''))) {
        response.setStatus(400);
        response.setBody({error: 'invalid relay result'});
        return;
    }

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

    var job = new GlideRecord('x_nischoy_topo_job');
    job.addQuery('u_job_id', String(body.job_id));
    job.addQuery('u_relay', relay.getUniqueValue());
    job.setLimit(1);
    job.query();
    if (!job.next()) {
        response.setStatus(404);
        response.setBody({error: 'job not found for this relay'});
        return;
    }
    var profile = job.u_profile.getRefRecord();
    if (!profile.isValidRecord() || String(profile.u_profile_id) !== String(body.profile_id)) {
        response.setStatus(409);
        response.setBody({error: 'job profile does not match result'});
        return;
    }
    if (String(job.u_state) === 'completed' || String(job.u_state) === 'failed') {
        var storedSuccess = String(job.u_success) === 'true' || String(job.u_success) === '1';
        if (storedSuccess === Boolean(body.success)) {
            response.setStatus(200);
            response.setBody({acknowledged: true, duplicate: true});
            return;
        }
        response.setStatus(409);
        response.setBody({error: 'job already has a different terminal result'});
        return;
    }
    if (String(job.u_state) !== 'running') {
        response.setStatus(409);
        response.setBody({error: 'job is not running'});
        return;
    }

    function boundedCount(value) {
        var parsed = parseInt(value || 0, 10);
        return isNaN(parsed) ? 0 : Math.max(0, Math.min(parsed, 1000000));
    }
    job.u_state = body.success ? 'completed' : 'failed';
    job.u_success = Boolean(body.success);
    job.u_completed_at = new GlideDateTime();
    job.u_observation_id = String(body.observation_id || '').substring(0, 128);
    job.u_assets = boundedCount(body.assets);
    job.u_relationships = boundedCount(body.relationships);
    job.u_collection_errors = boundedCount(body.collection_errors);
    job.u_error = String(body.error || '').substring(0, 4000);
    job.update();

    response.setStatus(200);
    response.setBody({acknowledged: true, duplicate: false});
})(request, response);
