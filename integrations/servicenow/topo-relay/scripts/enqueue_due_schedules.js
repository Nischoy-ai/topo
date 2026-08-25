(function enqueueDueTopoSchedules() {
    'use strict';

    var now = new GlideDateTime();
    var schedule = new GlideRecord('x_nischoy_topo_schedule');
    schedule.addQuery('u_active', true);
    schedule.addQuery('u_next_run', '<=', now);
    schedule.orderBy('u_next_run');
    schedule.setLimit(100);
    schedule.query();
    while (schedule.next()) {
        var profile = schedule.u_profile.getRefRecord();
        if (!profile.isValidRecord() || String(profile.u_active) !== 'true') {
            schedule.u_active = false;
            schedule.update();
            continue;
        }
        var relay = profile.u_relay.getRefRecord();
        if (!relay.isValidRecord() || String(relay.u_active) !== 'true') {
            schedule.u_active = false;
            schedule.update();
            continue;
        }

        var outstanding = new GlideRecord('x_nischoy_topo_job');
        outstanding.addQuery('u_profile', profile.getUniqueValue());
        outstanding.addQuery('u_state', 'IN', 'queued,running');
        outstanding.setLimit(1);
        outstanding.query();
        if (!outstanding.hasNext()) {
            var job = new GlideRecord('x_nischoy_topo_job');
            job.initialize();
            job.u_job_id = gs.generateGUID();
            job.u_relay = relay.getUniqueValue();
            job.u_profile = profile.getUniqueValue();
            job.u_state = 'queued';
            job.insert();
        }

        var minutes = parseInt(schedule.u_interval_minutes, 10);
        minutes = isNaN(minutes) ? 60 : Math.max(1, Math.min(minutes, 43200));
        var next = new GlideDateTime(schedule.u_next_run);
        do {
            next.addSeconds(minutes * 60);
        } while (next.compareTo(now) <= 0);
        schedule.u_next_run = next;
        schedule.update();
    }
})();
