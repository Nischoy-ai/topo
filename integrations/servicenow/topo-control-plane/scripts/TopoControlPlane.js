var TopoControlPlane = Class.create();
TopoControlPlane.prototype = {
    CONTRACT: 'v1alpha1',
    OPERATION_LOCAL: 'local.v1',
    OPERATION_SSH_LINUX: 'ssh_linux.v1',
    MAX_SSH_TARGETS: 1024,
    MAX_RESULT_BYTES: 1048576,
    SUCCESS_RETENTION_SECONDS: 86400,
    FAILURE_RETENTION_SECONDS: 604800,

    initialize: function () {},

    register: function (body) {
        if (!this._only(body, ['schema_version', 'boot_id', 'worker_pool', 'site_id', 'version', 'capabilities', 'policy_digest', 'max_concurrency', 'started_at']) ||
                body.schema_version !== this.CONTRACT || !this._safeID(body.boot_id) ||
                !this._safeID(body.worker_pool) || !this._safeID(body.site_id) ||
                !this._short(body.version, 64) || !this._hex(body.policy_digest, 64) ||
                !this._capabilities(body.capabilities) ||
                (typeof body.max_concurrency !== 'undefined' && !this._integer(body.max_concurrency, 1, 32))) {
            return this._result(400, {error: 'invalid worker registration'});
        }

        var pool = this._poolForCaller(String(body.worker_pool), String(body.site_id));
        if (!pool) {
            return this._result(403, {error: 'worker pool is not registered for this identity and site'});
        }

        // Registration is idempotent within one in-memory boot identifier. A
        // process restart generates a new boot identifier and therefore a new
        // ephemeral worker row.
        var existing = new GlideRecord('x_664635_topo_worker');
        existing.addQuery('u_pool', pool.getUniqueValue());
        existing.addQuery('u_boot_id', String(body.boot_id));
        existing.setLimit(1);
        existing.query();
        if (existing.next()) {
            if (String(existing.u_policy_digest) !== String(body.policy_digest) ||
                    String(existing.u_site_id) !== String(body.site_id) ||
                    String(existing.u_capabilities) !== this._capabilities(body.capabilities).join(',')) {
                return this._result(409, {error: 'boot identifier is already registered with different policy'});
            }
            existing.u_last_heartbeat = new GlideDateTime();
            existing.u_max_leases = typeof body.max_concurrency === 'undefined' ? 1 : body.max_concurrency;
            existing.u_active = true;
            existing.update();
            return this._result(200, {worker_id: String(existing.u_worker_id)});
        }

        var worker = new GlideRecord('x_664635_topo_worker');
        worker.initialize();
        worker.u_worker_id = gs.generateGUID();
        worker.u_boot_id = String(body.boot_id);
        worker.u_pool = pool.getUniqueValue();
        worker.u_site_id = String(body.site_id);
        worker.u_version = String(body.version).substring(0, 64);
        worker.u_capabilities = this._capabilities(body.capabilities).join(',');
        worker.u_policy_digest = String(body.policy_digest).toLowerCase();
        worker.u_max_leases = typeof body.max_concurrency === 'undefined' ? 1 : body.max_concurrency;
        worker.u_current_leases = 0;
        worker.u_active = true;
        worker.u_started_at = this._dateOrNow(body.started_at);
        worker.u_last_heartbeat = new GlideDateTime();
        if (!worker.insert()) {
            return this._result(500, {error: 'worker registration could not be stored'});
        }
        return this._result(201, {worker_id: String(worker.u_worker_id)});
    },

    heartbeat: function (body) {
        if (!this._only(body, ['schema_version', 'worker_id', 'boot_id', 'current_leases', 'sent_at']) ||
                body.schema_version !== this.CONTRACT || !this._safeID(body.worker_id) ||
                !this._safeID(body.boot_id) || !this._integer(body.current_leases, 0, 10000)) {
            return this._result(400, {error: 'invalid worker heartbeat'});
        }
        var worker = this._workerForCaller(String(body.worker_id), String(body.boot_id));
        if (!worker) {
            return this._result(403, {error: 'worker identity is not registered'});
        }
        worker.u_last_heartbeat = new GlideDateTime();
        worker.u_current_leases = this._workerLeaseCount(worker.getUniqueValue());
        worker.u_active = true;
        worker.update();

        var cancelled = [];
        var task = new GlideRecord('x_664635_topo_task');
        task.addQuery('u_lease_worker', worker.getUniqueValue());
        task.addQuery('u_lease_boot_id', String(body.boot_id));
        task.addQuery('u_cancel_requested', true);
        task.addQuery('u_state', 'IN', 'leased,running,results_received,ire_processing');
        task.setLimit(100);
        task.query();
        while (task.next()) {
            cancelled.push(String(task.u_attempt_id));
        }
        return this._result(200, {cancel_attempt_ids: cancelled});
    },

    claim: function (body) {
        if (!this._only(body, ['schema_version', 'worker_id', 'boot_id', 'capabilities', 'current_leases']) ||
                body.schema_version !== this.CONTRACT || !this._safeID(body.worker_id) ||
                !this._safeID(body.boot_id) || !this._capabilities(body.capabilities) ||
                (typeof body.current_leases !== 'undefined' && !this._integer(body.current_leases, 0, 32))) {
            return this._result(400, {error: 'invalid task claim'});
        }
        var worker = this._workerForCaller(String(body.worker_id), String(body.boot_id));
        var capabilities = this._capabilities(body.capabilities);
        if (!worker || !capabilities || String(worker.u_capabilities) !== capabilities.join(',')) {
            return this._result(403, {error: 'worker identity or capability is not registered'});
        }
        var pool = worker.u_pool.getRefRecord();
        if (!pool.isValidRecord() || !this._isTrue(pool.u_active)) {
            return this._result(409, {error: 'worker pool is inactive'});
        }

        this._expireLeases(pool.getUniqueValue());
        var slots = this._availableLeaseSlots(pool, worker);
        if (!slots) {
            return this._result(200, {task: null});
        }

        // updateMultiple executes one conditional SQL update. The state and
        // sys_id predicates form the compare-and-swap; a read followed by an
        // unrelated update would allow two workers to win the same task.
        for (var tryNumber = 0; tryNumber < 10; tryNumber++) {
            var candidate = new GlideRecord('x_664635_topo_task');
            candidate.addQuery('u_worker_pool', pool.getUniqueValue());
            candidate.addQuery('u_state', 'ready');
            candidate.addQuery('u_operation', 'IN', capabilities.join(','));
            candidate.addQuery('u_cancel_requested', false);
            candidate.orderBy('u_partition_ordinal');
            candidate.orderBy('sys_created_on');
            candidate.setLimit(1);
            candidate.query();
            if (!candidate.next()) {
                return this._result(200, {task: null});
            }

            var now = new GlideDateTime();
            var deadline = new GlideDateTime(String(candidate.u_deadline));
            if (deadline.compareTo(now) <= 0) {
                this._failUnleasedTask(candidate, 'task deadline expired before claim');
                continue;
            }
            var attemptID = gs.generateGUID();
            var leaseToken = gs.generateGUID() + gs.generateGUID();
            var leaseExpires = new GlideDateTime(now.getValue());
            leaseExpires.addSeconds(this._boundedInteger(pool.u_lease_seconds, 30, 900, 120));
            if (leaseExpires.compareTo(deadline) > 0) {
                leaseExpires = deadline;
            }

            var cas = new GlideRecord('x_664635_topo_task');
            cas.addQuery('sys_id', candidate.getUniqueValue());
            cas.addQuery('u_state', 'ready');
            cas.setValue('u_state', 'leased');
            cas.setValue('u_attempt_count', this._boundedInteger(candidate.u_attempt_count, 0, 1000000, 0) + 1);
            cas.setValue('u_attempt_id', attemptID);
            cas.setValue('u_lease_worker', worker.getUniqueValue());
            cas.setValue('u_lease_boot_id', String(body.boot_id));
            cas.setValue('u_lease_token_digest', this._sha256(leaseToken));
            cas.setValue('u_pool_lease_slot', slots.pool);
            cas.setValue('u_worker_lease_slot', slots.worker);
            cas.setValue('u_lease_expires', leaseExpires);
            try {
                cas.updateMultiple();
            } catch (claimError) {
                // Unique pool/worker lease-slot indexes are the capacity
                // reservation. A concurrent claimant may win either slot;
                // re-read availability rather than exceeding the ceiling.
                slots = this._availableLeaseSlots(pool, worker);
                if (!slots) {
                    return this._result(200, {task: null});
                }
                continue;
            }

            var claimed = new GlideRecord('x_664635_topo_task');
            if (!claimed.get(candidate.getUniqueValue()) || String(claimed.u_state) !== 'leased' ||
                    String(claimed.u_attempt_id) !== attemptID ||
                    String(claimed.u_lease_worker) !== worker.getUniqueValue() ||
                    String(claimed.u_pool_lease_slot) !== slots.pool ||
                    String(claimed.u_worker_lease_slot) !== slots.worker) {
                slots = this._availableLeaseSlots(pool, worker);
                if (!slots) {
                    return this._result(200, {task: null});
                }
                continue;
            }
            this._markRunRunning(claimed.u_run.getRefRecord());
            worker.u_current_leases = this._workerLeaseCount(worker.getUniqueValue());
            worker.u_last_heartbeat = now;
            worker.update();
            var claimedOperation = String(claimed.u_operation);
            var claimedTask = {
                task_id: String(claimed.u_task_id),
                run_id: String(claimed.u_run.u_run_id),
                attempt_id: attemptID,
                lease_token: leaseToken,
                lease_expires_at: this._iso(leaseExpires),
                operation: claimedOperation,
                profile_id: String(claimed.u_profile_id),
                profile_revision: parseInt(claimed.u_profile_revision, 10),
                deadline: this._iso(deadline)
            };
            if (claimedOperation === this.OPERATION_SSH_LINUX) {
                claimedTask.credential_binding_id = String(claimed.u_credential_binding.u_binding_id);
                claimedTask.target_partition = {
                    key: String(claimed.u_partition_key),
                    ordinal: parseInt(claimed.u_partition_ordinal, 10),
                    count: parseInt(claimed.u_partition_count, 10),
                    cidrs: [String(claimed.u_partition_cidrs)]
                };
            }
            return this._result(200, {task: claimedTask});
        }
        return this._result(200, {task: null});
    },

    renew: function (taskID, body) {
        if (!this._safeID(taskID) || !this._leaseBody(body, ['schema_version', 'worker_id', 'boot_id', 'attempt_id', 'lease_token'])) {
            return this._result(400, {error: 'invalid lease renewal'});
        }
        var lease = this._ownedLease(taskID, body, true);
        if (!lease.ok) {
            return lease.result;
        }
        if (this._isTrue(lease.task.u_cancel_requested) || String(lease.task.u_state) === 'cancelled') {
            return this._result(200, {lease_expires_at: this._iso(new GlideDateTime(String(lease.task.u_lease_expires))), cancelled: true});
        }
        var now = new GlideDateTime();
        var pool = lease.worker.u_pool.getRefRecord();
        var expiry = new GlideDateTime(now.getValue());
        expiry.addSeconds(this._boundedInteger(pool.u_lease_seconds, 30, 900, 120));
        var deadline = new GlideDateTime(String(lease.task.u_deadline));
        if (expiry.compareTo(deadline) > 0) {
            expiry = deadline;
        }
        if (expiry.compareTo(now) <= 0) {
            return this._result(409, {error: 'task deadline has expired'});
        }
        lease.task.u_lease_expires = expiry;
        if (String(lease.task.u_state) === 'leased') {
            lease.task.u_state = 'running';
        }
        lease.task.update();
        return this._result(200, {lease_expires_at: this._iso(expiry), cancelled: false});
    },

    credential: function (taskID, body) {
        if (!this._safeID(taskID) ||
                !this._leaseBody(body, ['schema_version', 'worker_id', 'boot_id', 'attempt_id', 'lease_token'])) {
            return this._result(400, {error: 'invalid credential request'});
        }
        var lease = this._ownedLease(taskID, body, false);
        if (!lease.ok) {
            this._recordCredentialAccess(null, null, body, 'denied', 'lease_not_owned');
            return lease.result;
        }
        if (this._isTrue(lease.task.u_cancel_requested)) {
            this._recordCredentialAccess(lease.task, null, body, 'denied', 'task_cancelled');
            return this._result(409, {error: 'task is cancelled'});
        }
        if (String(lease.task.u_operation) !== this.OPERATION_SSH_LINUX ||
                !String(lease.task.u_credential_binding) || !String(lease.task.u_target_scope)) {
            this._recordCredentialAccess(lease.task, null, body, 'denied', 'task_not_credentialed_ssh');
            return this._result(409, {error: 'task has no reviewed SSH credential authority'});
        }
        var binding = lease.task.u_credential_binding.getRefRecord();
        if (!binding.isValidRecord() || !this._isTrue(binding.u_active) ||
                String(binding.u_protocol) !== 'ssh_password' ||
                String(binding.u_profile_id) !== String(lease.task.u_profile_id) ||
                parseInt(binding.u_profile_revision, 10) !== parseInt(lease.task.u_profile_revision, 10) ||
                String(binding.u_target_scope) !== String(lease.task.u_target_scope)) {
            this._recordCredentialAccess(lease.task, binding, body, 'denied', 'binding_mismatch');
            return this._result(409, {error: 'task credential authority is inactive or mismatched'});
        }
        var credential = binding.u_credential.getRefRecord();
        if (!credential.isValidRecord() || !this._isTrue(credential.u_active) ||
                !this._sshUsername(String(credential.u_username))) {
            this._recordCredentialAccess(lease.task, binding, body, 'denied', 'credential_inactive');
            return this._result(409, {error: 'task credential is inactive or invalid'});
        }
        var password;
        try {
            password = String(credential.u_password.getDecryptedValue());
        } catch (error) {
            this._recordCredentialAccess(lease.task, binding, body, 'denied', 'password2_decrypt_failed');
            return this._result(500, {error: 'task credential could not be resolved'});
        }
        if (!password || password.length > 4096) {
            this._recordCredentialAccess(lease.task, binding, body, 'denied', 'password2_value_invalid');
            return this._result(500, {error: 'task credential could not be resolved'});
        }
        if (!this._recordCredentialAccess(lease.task, binding, body, 'allowed', 'attempt_bound')) {
            return this._result(500, {error: 'credential access could not be audited'});
        }
        return this._result(200, {username: String(credential.u_username), password: password});
    },

    ingestResult: function (taskID, body) {
        if (!this._safeID(taskID) ||
                !this._leaseBody(body, ['schema_version', 'worker_id', 'boot_id', 'attempt_id', 'lease_token', 'chunk_number', 'chunk_count', 'checksum_sha256', 'observation_json']) ||
                body.chunk_number !== 0 || body.chunk_count !== 1 || !this._hex(body.checksum_sha256, 64) ||
                typeof body.observation_json !== 'string' || body.observation_json.length === 0 ||
                this._utf8Length(body.observation_json) > this.MAX_RESULT_BYTES) {
            return this._result(400, {error: 'invalid result chunk'});
        }
        var lease = this._ownedLease(taskID, body, false);
        if (!lease.ok) {
            return lease.result;
        }
        if (this._isTrue(lease.task.u_cancel_requested)) {
            return this._result(409, {error: 'task is cancelled'});
        }
        if (String(body.checksum_sha256).toLowerCase() !== this._sha256(body.observation_json)) {
            return this._result(422, {error: 'result checksum does not match observation_json'});
        }

        var duplicate = new GlideRecord('x_664635_topo_result');
        duplicate.addQuery('u_task', lease.task.getUniqueValue());
        duplicate.addQuery('u_attempt_id', String(body.attempt_id));
        duplicate.addQuery('u_chunk_number', 0);
        duplicate.setLimit(1);
        duplicate.query();
        if (duplicate.next()) {
            if (String(duplicate.u_checksum) === String(body.checksum_sha256).toLowerCase() &&
                    parseInt(duplicate.u_chunk_count, 10) === 1 && String(duplicate.u_attachment) !== '') {
                return this._result(200, {accepted: true, duplicate: true});
            }
            return this._result(409, {error: 'chunk key already exists with different content or is still committing'});
        }

        var result = new GlideRecord('x_664635_topo_result');
        result.initialize();
        result.u_task = lease.task.getUniqueValue();
        result.u_attempt_id = String(body.attempt_id);
        result.u_chunk_number = 0;
        result.u_chunk_count = 1;
        result.u_checksum = String(body.checksum_sha256).toLowerCase();
        result.u_payload_bytes = this._utf8Length(body.observation_json);
        result.u_processing_state = 'received';
        var resultID = result.insert();
        if (!resultID) {
            // A concurrent insert may have won the unique key. Re-enter the
            // idempotency check without accepting mismatched content.
            duplicate.query();
            if (duplicate.next() && String(duplicate.u_checksum) === String(body.checksum_sha256).toLowerCase() &&
                    String(duplicate.u_attachment) !== '') {
                return this._result(200, {accepted: true, duplicate: true});
            }
            return this._result(409, {error: 'result chunk could not be stored'});
        }
        var attachmentID = new GlideSysAttachment().write(
            result,
            'topo-observation-' + String(body.attempt_id) + '-0.json',
            'application/json',
            body.observation_json
        );
        if (!attachmentID) {
            result.deleteRecord();
            return this._result(500, {error: 'result attachment could not be stored'});
        }
        result.u_attachment = attachmentID;
        result.update();
        lease.task.u_state = 'results_received';
        lease.task.u_chunk_count = 1;
        lease.task.update();
        return this._result(201, {accepted: true, duplicate: false});
    },

    complete: function (taskID, body) {
        if (!this._safeID(taskID) ||
                !this._leaseBody(body, ['schema_version', 'worker_id', 'boot_id', 'attempt_id', 'lease_token', 'success', 'chunk_count', 'failure']) ||
                typeof body.success !== 'boolean' || !this._integer(body.chunk_count, 0, 1)) {
            return this._result(400, {error: 'invalid task completion'});
        }
        var lease = this._ownedLease(taskID, body, false);
        if (!lease.ok) {
            // A retry after a lost response may observe the same terminal
            // attempt. It is safe to acknowledge without repeating IRE.
            var terminal = this._terminalAttempt(taskID, body);
            if (terminal) {
                return this._result(200, terminal);
            }
            return lease.result;
        }

        if (!body.success) {
            var failure = this._validateFailure(body.failure);
            if (!failure.ok || body.chunk_count !== 0) {
                return this._result(400, {error: 'failed completion requires one bounded structured failure and no chunks'});
            }
            if (this._isTrue(lease.task.u_cancel_requested) || String(body.failure.code) === 'cancelled') {
                lease.task.u_state = 'cancelled';
                lease.task.u_error = 'cancelled';
                lease.task.u_cancel_requested = false;
            } else {
                lease.task.u_state = 'failed';
                lease.task.u_error = failure.message;
            }
            this._releaseLeaseSlots(lease.task);
            lease.task.update();
            var failureOutcome = String(lease.task.u_state) === 'cancelled' ? 'cancelled' : 'failed';
            this._markAttemptResults(lease.task, String(body.attempt_id), 'failed', failureOutcome, this.FAILURE_RETENTION_SECONDS);
            var failedRun = this._refreshRun(lease.task.u_run.getRefRecord());
            return this._result(200, {task_state: String(lease.task.u_state), run_state: failedRun});
        }
        if (body.failure !== null && typeof body.failure !== 'undefined') {
            return this._result(400, {error: 'successful completion cannot include failure detail'});
        }
        if (this._isTrue(lease.task.u_cancel_requested)) {
            return this._result(409, {error: 'task is cancelled'});
        }
        if (body.chunk_count !== 1 || String(lease.task.u_state) !== 'results_received' || parseInt(lease.task.u_chunk_count, 10) !== 1) {
            return this._result(409, {error: 'successful completion requires one acknowledged result chunk'});
        }
        var result = new GlideRecord('x_664635_topo_result');
        result.addQuery('u_task', lease.task.getUniqueValue());
        result.addQuery('u_attempt_id', String(body.attempt_id));
        result.addQuery('u_chunk_number', 0);
        result.setLimit(1);
        result.query();
        if (!result.next() || String(result.u_attachment) === '') {
            return this._result(409, {error: 'result chunk is not available'});
        }

        // This compare-and-swap prevents concurrent completion requests from
        // invoking IRE twice. An interrupted apply remains ambiguous and is
        // never blindly replayed by a later request or maintenance job.
        var completionID = gs.generateGUID();
        var cas = new GlideRecord('x_664635_topo_task');
        cas.addQuery('sys_id', lease.task.getUniqueValue());
        cas.addQuery('u_state', 'results_received');
        cas.addQuery('u_attempt_id', String(body.attempt_id));
        cas.addQuery('u_lease_token_digest', this._sha256(String(body.lease_token)));
        cas.setValue('u_state', 'ire_processing');
        cas.setValue('u_completion_id', completionID);
        cas.updateMultiple();
        lease.task.get(lease.task.getUniqueValue());
        if (String(lease.task.u_state) !== 'ire_processing' || String(lease.task.u_attempt_id) !== String(body.attempt_id) ||
                String(lease.task.u_completion_id) !== completionID) {
            return this._result(409, {error: 'task completion is already being processed'});
        }

        var outcome;
        try {
            outcome = new TopoIREProcessor().process(lease.task, result);
            lease.task.u_state = 'complete';
            lease.task.u_error = '';
        } catch (error) {
            lease.task.u_state = 'failed';
            lease.task.u_error = this._boundedError(error);
            outcome = {assets: 0, relationships: 0, collection_errors: 0};
        }
        this._releaseLeaseSlots(lease.task);
        lease.task.update();
        var run = lease.task.u_run.getRefRecord();
        if (run.isValidRecord()) {
            run.u_assets = this._boundedInteger(run.u_assets, 0, 1000000, 0) + outcome.assets;
            run.u_relationships = this._boundedInteger(run.u_relationships, 0, 1000000, 0) + outcome.relationships;
            run.u_collection_errors = this._boundedInteger(run.u_collection_errors, 0, 1000000, 0) + outcome.collection_errors;
            run.update();
        }
        var runState = this._refreshRun(run);
        return this._result(200, {task_state: String(lease.task.u_state), run_state: runState});
    },

    createRun: function (profileSysID, trigger, scheduleSysID) {
        if (['manual', 'scheduled', 'retry'].indexOf(String(trigger)) < 0) {
            throw new Error('invalid Topo run trigger');
        }
        var profile = new GlideRecord('x_664635_topo_profile');
        if (!profile.get(String(profileSysID)) || !this._isTrue(profile.u_active) ||
                [this.OPERATION_LOCAL, this.OPERATION_SSH_LINUX].indexOf(String(profile.u_operation)) < 0 ||
                String(profile.u_schema_version) !== this.CONTRACT || !this._safeID(String(profile.u_profile_id)) ||
                !this._integer(parseInt(profile.u_revision, 10), 1, 1000000)) {
            throw new Error('profile is not an active reviewed revision');
        }
        var pool = profile.u_worker_pool.getRefRecord();
        if (!pool.isValidRecord() || !this._isTrue(pool.u_active)) {
            throw new Error('profile worker pool is inactive');
        }
        var operation = String(profile.u_operation);
        var targetScope = null;
        var credentialBinding = null;
        var partitions = [{key: 'local', cidr: ''}];
        if (operation === this.OPERATION_LOCAL) {
            if (String(profile.u_target_scope) || String(profile.u_credential_binding)) {
                throw new Error('local.v1 profile must not carry remote target or credential authority');
            }
        } else {
            targetScope = profile.u_target_scope.getRefRecord();
            credentialBinding = profile.u_credential_binding.getRefRecord();
            if (!targetScope.isValidRecord() || !this._isTrue(targetScope.u_active) ||
                    String(targetScope.u_worker_pool) !== pool.getUniqueValue() ||
                    parseInt(targetScope.u_ipv4_partition_prefix, 10) !== 32) {
                throw new Error('ssh_linux.v1 requires an active /32 target scope in the profile worker pool');
            }
            if (!credentialBinding.isValidRecord() || !this._isTrue(credentialBinding.u_active) ||
                    String(credentialBinding.u_protocol) !== 'ssh_password' ||
                    String(credentialBinding.u_profile_id) !== String(profile.u_profile_id) ||
                    parseInt(credentialBinding.u_profile_revision, 10) !== parseInt(profile.u_revision, 10) ||
                    String(credentialBinding.u_target_scope) !== targetScope.getUniqueValue()) {
                throw new Error('ssh_linux.v1 profile credential binding is inactive or mismatched');
            }
            var boundCredential = credentialBinding.u_credential.getRefRecord();
            if (!boundCredential.isValidRecord() || !this._isTrue(boundCredential.u_active)) {
                throw new Error('ssh_linux.v1 profile credential is inactive');
            }
            var compiled = this.compileTargetScope(targetScope);
            if (compiled.cidrs.length < 1 || compiled.cidrs.length > this.MAX_SSH_TARGETS) {
                throw new Error('ssh_linux.v1 target scope must compile to between 1 and ' + this.MAX_SSH_TARGETS + ' addresses');
            }
            partitions = [];
            for (var partitionIndex = 0; partitionIndex < compiled.cidrs.length; partitionIndex++) {
                if (!/^(?:\d{1,3}\.){3}\d{1,3}\/32$/.test(compiled.cidrs[partitionIndex])) {
                    throw new Error('ssh_linux.v1 target scope produced a non-/32 partition');
                }
                partitions.push({key: compiled.keys[partitionIndex], cidr: compiled.cidrs[partitionIndex]});
            }
        }
        var outstanding = new GlideRecord('x_664635_topo_run');
        outstanding.addQuery('u_profile', profile.getUniqueValue());
        outstanding.addQuery('u_state', 'IN', 'ready,running,cancelling');
        outstanding.setLimit(1);
        outstanding.query();
        if (outstanding.next()) {
            return String(outstanding.u_run_id);
        }

        var now = new GlideDateTime();
        var run = new GlideRecord('x_664635_topo_run');
        run.initialize();
        run.u_run_id = gs.generateGUID();
        run.u_profile = profile.getUniqueValue();
        if (scheduleSysID) {
            run.u_schedule = String(scheduleSysID);
        }
        run.u_trigger = String(trigger);
        run.u_state = 'ready';
        run.u_started_at = now;
        run.u_task_count = partitions.length;
        run.u_complete_tasks = 0;
        run.u_failed_tasks = 0;
        run.u_cancelled_tasks = 0;
        run.u_attempts = 0;
        run.u_assets = 0;
        run.u_relationships = 0;
        run.u_collection_errors = 0;
        if (!run.insert()) {
            throw new Error('Topo run could not be created');
        }

        var deadline = new GlideDateTime(now.getValue());
        deadline.addSeconds(this._boundedInteger(pool.u_max_task_seconds, 30, 3600, 300));
        for (var taskIndex = 0; taskIndex < partitions.length; taskIndex++) {
            var task = new GlideRecord('x_664635_topo_task');
            task.initialize();
            task.u_task_id = gs.generateGUID();
            task.u_run = run.getUniqueValue();
            task.u_worker_pool = pool.getUniqueValue();
            task.u_operation = operation;
            task.u_profile_id = String(profile.u_profile_id);
            task.u_profile_revision = parseInt(profile.u_revision, 10);
            if (targetScope) {
                task.u_target_scope = targetScope.getUniqueValue();
                task.u_credential_binding = credentialBinding.getUniqueValue();
            }
            task.u_partition_key = partitions[taskIndex].key;
            task.u_partition_ordinal = taskIndex;
            task.u_partition_count = partitions.length;
            task.u_partition_cidrs = partitions[taskIndex].cidr;
            task.u_state = 'ready';
            task.u_attempt_count = 0;
            task.u_chunk_count = 0;
            task.u_cancel_requested = false;
            task.u_deadline = deadline;
            if (!task.insert()) {
                var cleanup = new GlideRecord('x_664635_topo_task');
                cleanup.addQuery('u_run', run.getUniqueValue());
                cleanup.deleteMultiple();
                run.deleteRecord();
                throw new Error('Topo task partitions could not be created');
            }
        }
        return String(run.u_run_id);
    },

    cancelRun: function (runSysID) {
        var run = new GlideRecord('x_664635_topo_run');
        if (!run.get(String(runSysID))) {
            throw new Error('Topo run was not found');
        }
        if (['complete', 'failed', 'cancelled'].indexOf(String(run.u_state)) >= 0) {
            return String(run.u_state);
        }
        run.u_state = 'cancelling';
        run.update();
        var tasks = new GlideRecord('x_664635_topo_task');
        tasks.addQuery('u_run', run.getUniqueValue());
        tasks.query();
        while (tasks.next()) {
            var state = String(tasks.u_state);
            if (state === 'planned' || state === 'ready') {
                tasks.u_state = 'cancelled';
                tasks.u_error = 'cancelled';
                tasks.u_cancel_requested = false;
                tasks.update();
            } else if (['leased', 'running', 'results_received'].indexOf(state) >= 0) {
                tasks.u_cancel_requested = true;
                tasks.update();
            }
        }
        return this._refreshRun(run);
    },

    enqueueDueSchedules: function () {
        var now = new GlideDateTime();
        var schedule = new GlideRecord('x_664635_topo_schedule');
        schedule.addQuery('u_active', true);
        schedule.addQuery('u_next_run', '<=', now);
        schedule.orderBy('u_next_run');
        schedule.setLimit(100);
        schedule.query();
        var created = 0;
        while (schedule.next()) {
            try {
                this.createRun(String(schedule.u_profile), 'scheduled', schedule.getUniqueValue());
                created++;
            } catch (error) {
                gs.warn('Topo schedule ' + String(schedule.u_schedule_id) + ' was not enqueued: ' + this._boundedError(error));
            }
            var minutes = this._boundedInteger(schedule.u_interval_minutes, 1, 43200, 60);
            var next = new GlideDateTime(String(schedule.u_next_run));
            do {
                next.addSeconds(minutes * 60);
            } while (next.compareTo(now) <= 0);
            schedule.u_next_run = next;
            schedule.update();
        }
        return created;
    },

    maintain: function () {
        this._expireLeases('');
        var now = new GlideDateTime();
        var stale = new GlideRecord('x_664635_topo_task');
        stale.addQuery('u_state', 'ire_processing');
        stale.addQuery('u_deadline', '<=', now);
        stale.setLimit(100);
        stale.query();
        while (stale.next()) {
            stale.u_state = 'failed';
            stale.u_error = 'IRE completion was interrupted; outcome is ambiguous and was not replayed';
            this._releaseLeaseSlots(stale);
            stale.update();
            this._markAttemptResults(stale, String(stale.u_attempt_id), 'failed', 'ambiguous', this.FAILURE_RETENTION_SECONDS);
            this._markAmbiguousDelivery(stale);
            this._refreshRun(stale.u_run.getRefRecord());
        }

        var result = new GlideRecord('x_664635_topo_result');
        result.addNotNullQuery('u_delete_after');
        result.addQuery('u_delete_after', '<=', now);
        result.addQuery('u_processing_state', 'IN', 'processed,failed,superseded');
        result.orderBy('u_delete_after');
        result.setLimit(200);
        result.query();
        var deleted = 0;
        while (result.next()) {
            var attachmentID = String(result.u_attachment);
            if (attachmentID) {
                new GlideSysAttachment().deleteAttachment(attachmentID);
                var verify = new GlideRecord('sys_attachment');
                if (verify.get(attachmentID)) {
                    gs.warn('Topo retention could not delete result attachment ' + attachmentID);
                    continue;
                }
            }
            result.deleteRecord();
            deleted++;
        }
        return deleted;
    },

    _poolForCaller: function (poolID, siteID) {
        var pool = new GlideRecord('x_664635_topo_worker_pool');
        pool.addQuery('u_pool_id', poolID);
        pool.addQuery('u_site_id', siteID);
        pool.addQuery('u_service_user', gs.getUserID());
        pool.addQuery('u_active', true);
        pool.setLimit(1);
        pool.query();
        return pool.next() ? pool : null;
    },

    _workerForCaller: function (workerID, bootID) {
        var worker = new GlideRecord('x_664635_topo_worker');
        worker.addQuery('u_worker_id', workerID);
        worker.addQuery('u_boot_id', bootID);
        worker.addQuery('u_active', true);
        worker.setLimit(1);
        worker.query();
        if (!worker.next()) {
            return null;
        }
        var pool = worker.u_pool.getRefRecord();
        if (!pool.isValidRecord() || !this._isTrue(pool.u_active) || String(pool.u_service_user) !== gs.getUserID() ||
                String(pool.u_site_id) !== String(worker.u_site_id)) {
            return null;
        }
        return worker;
    },

    _ownedLease: function (taskID, body, allowCancelled) {
        var worker = this._workerForCaller(String(body.worker_id), String(body.boot_id));
        if (!worker) {
            return {ok: false, result: this._result(403, {error: 'worker identity is not registered'})};
        }
        var task = new GlideRecord('x_664635_topo_task');
        task.addQuery('u_task_id', String(taskID));
        task.addQuery('u_lease_worker', worker.getUniqueValue());
        task.addQuery('u_lease_boot_id', String(body.boot_id));
        task.addQuery('u_attempt_id', String(body.attempt_id));
        task.setLimit(1);
        task.query();
        if (!task.next() || String(task.u_lease_token_digest) !== this._sha256(String(body.lease_token))) {
            return {ok: false, result: this._result(403, {error: 'task lease is not owned by this worker attempt'})};
        }
        var state = String(task.u_state);
        var allowed = ['leased', 'running', 'results_received'];
        if (allowCancelled) {
            allowed.push('cancelled');
            allowed.push('ire_processing');
        }
        if (allowed.indexOf(state) < 0) {
            return {ok: false, result: this._result(409, {error: 'task lease is not active'})};
        }
        var now = new GlideDateTime();
        var expires = new GlideDateTime(String(task.u_lease_expires));
        if (expires.compareTo(now) <= 0) {
            return {ok: false, result: this._result(409, {error: 'task lease has expired'})};
        }
        return {ok: true, worker: worker, task: task};
    },

    _terminalAttempt: function (taskID, body) {
        var worker = this._workerForCaller(String(body.worker_id), String(body.boot_id));
        if (!worker) {
            return null;
        }
        var task = new GlideRecord('x_664635_topo_task');
        task.addQuery('u_task_id', String(taskID));
        task.addQuery('u_attempt_id', String(body.attempt_id));
        task.addQuery('u_lease_worker', worker.getUniqueValue());
        task.addQuery('u_lease_token_digest', this._sha256(String(body.lease_token)));
        task.addQuery('u_state', 'IN', 'complete,failed,cancelled');
        task.setLimit(1);
        task.query();
        if (!task.next()) {
            return null;
        }
        var run = task.u_run.getRefRecord();
        return {task_state: String(task.u_state), run_state: run.isValidRecord() ? String(run.u_state) : String(task.u_state)};
    },

    _expireLeases: function (poolSysID) {
        var now = new GlideDateTime();
        var expired = new GlideRecord('x_664635_topo_task');
        expired.addQuery('u_state', 'IN', 'leased,running,results_received');
        expired.addQuery('u_lease_expires', '<=', now);
        if (poolSysID) {
            expired.addQuery('u_worker_pool', String(poolSysID));
        }
        expired.orderBy('u_lease_expires');
        expired.setLimit(100);
        expired.query();
        while (expired.next()) {
            var attemptID = String(expired.u_attempt_id);
            var cancelled = this._isTrue(expired.u_cancel_requested);
            var nextState = cancelled ? 'cancelled' : 'ready';
            var cas = new GlideRecord('x_664635_topo_task');
            cas.addQuery('sys_id', expired.getUniqueValue());
            cas.addQuery('u_state', 'IN', 'leased,running,results_received');
            cas.addQuery('u_attempt_id', attemptID);
            cas.addQuery('u_lease_expires', '<=', now);
            cas.setValue('u_state', nextState);
            cas.setValue('u_attempt_id', null);
            cas.setValue('u_lease_worker', null);
            cas.setValue('u_lease_boot_id', null);
            cas.setValue('u_lease_token_digest', null);
            cas.setValue('u_pool_lease_slot', null);
            cas.setValue('u_worker_lease_slot', null);
            cas.setValue('u_lease_expires', null);
            cas.setValue('u_chunk_count', 0);
            cas.setValue('u_cancel_requested', false);
            if (cancelled) {
                cas.setValue('u_error', 'cancelled');
            }
            cas.updateMultiple();
            var verify = new GlideRecord('x_664635_topo_task');
            if (verify.get(expired.getUniqueValue()) && String(verify.u_state) === nextState && String(verify.u_attempt_id) === '') {
                this._markAttemptResults(verify, attemptID, cancelled ? 'failed' : 'superseded', cancelled ? 'cancelled' : 'superseded', this.FAILURE_RETENTION_SECONDS);
                this._refreshRun(verify.u_run.getRefRecord());
            }
        }
    },

    _markAttemptResults: function (task, attemptID, state, outcome, retentionSeconds) {
        if (!attemptID) {
            return;
        }
        var deleteAfter = new GlideDateTime();
        deleteAfter.addSeconds(retentionSeconds);
        var result = new GlideRecord('x_664635_topo_result');
        result.addQuery('u_task', task.getUniqueValue());
        result.addQuery('u_attempt_id', attemptID);
        result.setValue('u_processing_state', state);
        result.setValue('u_terminal_outcome', outcome);
        result.setValue('u_processed_at', new GlideDateTime());
        result.setValue('u_delete_after', deleteAfter);
        result.updateMultiple();
    },

    _markAmbiguousDelivery: function (task) {
        var delivery = new GlideRecord('x_664635_topo_ire_delivery');
        delivery.addQuery('u_task', task.getUniqueValue());
        delivery.addQuery('u_attempt_id', String(task.u_attempt_id));
        delivery.addQuery('u_state', 'preflight');
        delivery.setValue('u_state', 'ambiguous');
        delivery.setValue('u_diagnostics', 'IRE completion interrupted; apply outcome requires operator investigation');
        delivery.updateMultiple();
    },

    _refreshRun: function (run) {
        if (!run || !run.isValidRecord()) {
            return 'failed';
        }
        var tasks = new GlideRecord('x_664635_topo_task');
        tasks.addQuery('u_run', run.getUniqueValue());
        tasks.query();
        var complete = 0;
        var failed = 0;
        var cancelled = 0;
        var active = 0;
        var attempts = 0;
        var error = '';
        while (tasks.next()) {
            attempts += this._boundedInteger(tasks.u_attempt_count, 0, 1000000, 0);
            if (String(tasks.u_state) === 'complete') {
                complete++;
            } else if (String(tasks.u_state) === 'failed') {
                failed++;
                if (!error) {
                    error = String(tasks.u_error).substring(0, 4000);
                }
            } else if (String(tasks.u_state) === 'cancelled') {
                cancelled++;
            } else {
                active++;
            }
        }
        run.u_complete_tasks = complete;
        run.u_failed_tasks = failed;
        run.u_cancelled_tasks = cancelled;
        run.u_attempts = attempts;
        run.u_error = error;
        if (active > 0) {
            if (String(run.u_state) !== 'cancelling') {
                run.u_state = complete + failed + cancelled > 0 ? 'running' : String(run.u_state);
            }
        } else {
            run.u_state = failed > 0 ? 'failed' : (cancelled > 0 ? 'cancelled' : 'complete');
            run.u_completed_at = new GlideDateTime();
        }
        run.update();
        return String(run.u_state);
    },

    _failUnleasedTask: function (task, message) {
        task.u_state = 'failed';
        task.u_error = String(message).substring(0, 4000);
        task.update();
        this._refreshRun(task.u_run.getRefRecord());
    },

    _markRunRunning: function (run) {
        if (run && run.isValidRecord() && String(run.u_state) === 'ready') {
            run.u_state = 'running';
            run.update();
        }
    },

    _poolLeaseCount: function (poolID) {
        var count = new GlideAggregate('x_664635_topo_task');
        count.addQuery('u_worker_pool', poolID);
        count.addQuery('u_state', 'IN', 'leased,running,results_received,ire_processing');
        count.addAggregate('COUNT');
        count.query();
        return count.next() ? parseInt(count.getAggregate('COUNT'), 10) : 0;
    },

    _workerLeaseCount: function (workerSysID) {
        var count = new GlideAggregate('x_664635_topo_task');
        count.addQuery('u_lease_worker', workerSysID);
        count.addQuery('u_state', 'IN', 'leased,running,results_received,ire_processing');
        count.addAggregate('COUNT');
        count.query();
        return count.next() ? parseInt(count.getAggregate('COUNT'), 10) : 0;
    },

    _availableLeaseSlots: function (pool, worker) {
        var poolMaximum = this._boundedInteger(pool.u_max_leases, 1, 10000, 32);
        var workerMaximum = this._boundedInteger(worker.u_max_leases, 1, 32, 1);
        var poolSysID = String(pool.getUniqueValue());
        var workerSysID = String(worker.getUniqueValue());
        var usedPool = {};
        var usedWorker = {};
        var poolCount = 0;
        var workerCount = 0;
        var active = new GlideRecord('x_664635_topo_task');
        active.addQuery('u_worker_pool', poolSysID);
        active.addQuery('u_state', 'IN', 'leased,running,results_received,ire_processing');
        active.setLimit(poolMaximum);
        active.query();
        while (active.next()) {
            poolCount++;
            var poolSlot = String(active.u_pool_lease_slot);
            if (poolSlot) {
                usedPool[poolSlot] = true;
            }
            if (String(active.u_lease_worker) === workerSysID) {
                workerCount++;
                var workerSlot = String(active.u_worker_lease_slot);
                if (workerSlot) {
                    usedWorker[workerSlot] = true;
                }
            }
        }
        if (poolCount >= poolMaximum || workerCount >= workerMaximum) {
            return null;
        }
        var availablePool = '';
        for (var poolIndex = 0; poolIndex < poolMaximum; poolIndex++) {
            var poolKey = poolSysID + ':' + poolIndex;
            if (!usedPool[poolKey]) {
                availablePool = poolKey;
                break;
            }
        }
        var availableWorker = '';
        for (var workerIndex = 0; workerIndex < workerMaximum; workerIndex++) {
            var workerKey = workerSysID + ':' + workerIndex;
            if (!usedWorker[workerKey]) {
                availableWorker = workerKey;
                break;
            }
        }
        return availablePool && availableWorker ? {pool: availablePool, worker: availableWorker} : null;
    },

    _releaseLeaseSlots: function (task) {
        task.u_pool_lease_slot = '';
        task.u_worker_lease_slot = '';
    },

    _leaseBody: function (body, fields) {
        return this._only(body, fields) && body.schema_version === this.CONTRACT &&
            this._safeID(body.worker_id) && this._safeID(body.boot_id) &&
            this._safeID(body.attempt_id) && this._safeID(body.lease_token);
    },

    _recordCredentialAccess: function (task, binding, body, outcome, reason) {
        var event = new GlideRecord('x_664635_topo_credential_access');
        event.initialize();
        event.u_event_id = gs.generateGUID();
        if (task && task.isValidRecord()) {
            event.u_task = task.getUniqueValue();
        }
        event.u_attempt_id = String(body.attempt_id || '').substring(0, 128);
        event.u_worker_id = String(body.worker_id || '').substring(0, 128);
        if (binding && binding.isValidRecord()) {
            event.u_binding = binding.getUniqueValue();
        }
        event.u_outcome = String(outcome);
        event.u_reason = String(reason).substring(0, 128);
        event.u_accessed_at = new GlideDateTime();
        if (!event.insert()) {
            gs.warn('Topo credential access audit event could not be stored; outcome=' + String(outcome));
            return false;
        }
        return true;
    },

    _validateFailure: function (failure) {
        if (!this._only(failure, ['code', 'message', 'retryable']) || !this._safeID(failure.code) ||
                !this._short(failure.message, 1000) || typeof failure.retryable !== 'boolean') {
            return {ok: false};
        }
        return {ok: true, message: (String(failure.code) + ': ' + String(failure.message)).substring(0, 4000)};
    },

    _markResultFailure: function (result, outcome, diagnostic) {
        var deleteAfter = new GlideDateTime();
        deleteAfter.addSeconds(this.FAILURE_RETENTION_SECONDS);
        result.u_processing_state = 'failed';
        result.u_terminal_outcome = outcome;
        result.u_processed_at = new GlideDateTime();
        result.u_delete_after = deleteAfter;
        result.update();
        return this._boundedError(diagnostic);
    },

    compileTargetScope: function (record) {
        var scopeID = String(record.u_scope_id);
        var revision = parseInt(record.u_revision, 10);
        var partitionPrefix = parseInt(record.u_ipv4_partition_prefix, 10);
        if (!this._safeID(scopeID) || !this._integer(revision, 1, 1000000) ||
                !this._integer(partitionPrefix, 8, 32)) {
            throw new Error('scope ID, revision, or IPv4 partition prefix is invalid');
        }
        var pool = record.u_worker_pool.getRefRecord();
        if (!pool.isValidRecord() || String(pool.u_site_id) !== String(record.u_site_id)) {
            throw new Error('target scope site must match its worker pool site');
        }
        var included = this._normalizeIPv4Ranges(this._parseIPv4CIDRs(String(record.u_cidrs), true));
        var excluded = this._normalizeIPv4Ranges(this._parseIPv4CIDRs(String(record.u_exclusions), false));
        var allowed = included;
        for (var exclusionIndex = 0; exclusionIndex < excluded.length; exclusionIndex++) {
            var next = [];
            var exclusion = excluded[exclusionIndex];
            for (var segmentIndex = 0; segmentIndex < allowed.length; segmentIndex++) {
                var segment = allowed[segmentIndex];
                if (exclusion.end < segment.start || exclusion.start > segment.end) {
                    next.push(segment);
                    continue;
                }
                if (exclusion.start > segment.start) {
                    next.push({start: segment.start, end: exclusion.start - 1});
                }
                if (exclusion.end < segment.end) {
                    next.push({start: exclusion.end + 1, end: segment.end});
                }
            }
            allowed = next;
        }

        var cidrs = [];
        for (var allowedIndex = 0; allowedIndex < allowed.length; allowedIndex++) {
            var planned = this._partitionIPv4Range(allowed[allowedIndex], partitionPrefix, 100000 - cidrs.length);
            cidrs = cidrs.concat(planned);
            if (cidrs.length > 100000) {
                throw new Error('target scope exceeds 100000 deterministic partitions');
            }
        }
        if (cidrs.length === 0) {
            throw new Error('target scope exclusions remove every selected address');
        }
        var keys = [];
        for (var index = 0; index < cidrs.length; index++) {
            keys.push(this._sha256(scopeID + '\n' + revision + '\n' + cidrs[index]));
        }
        var canonicalIncluded = [];
        for (var includeIndex = 0; includeIndex < included.length; includeIndex++) {
            canonicalIncluded.push(included[includeIndex].cidr);
        }
        var canonicalExcluded = [];
        for (var denyIndex = 0; denyIndex < excluded.length; denyIndex++) {
            canonicalExcluded.push(excluded[denyIndex].cidr);
        }
        record.u_cidrs = canonicalIncluded.join('\n');
        record.u_exclusions = canonicalExcluded.join('\n');
        record.u_partition_count = cidrs.length;
        record.u_plan_digest = this._sha256(keys.join('\n'));
        return {cidrs: cidrs, keys: keys};
    },

    _parseIPv4CIDRs: function (value, required) {
        if (typeof value !== 'string' || value.length > 4000 || /[\u0000-\u001f\u007f]/.test(value.replace(/[\r\n]/g, ''))) {
            throw new Error('CIDR text is oversized or contains control characters');
        }
        var lines = value.split(/\r?\n/);
        var result = [];
        for (var lineIndex = 0; lineIndex < lines.length; lineIndex++) {
            var text = String(lines[lineIndex]).replace(/^\s+|\s+$/g, '');
            if (!text) {
                continue;
            }
            if (text.indexOf(':') >= 0) {
                throw new Error('Slice B compiles canonical IPv4 CIDRs only');
            }
            var match = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})\/(\d{1,2})$/.exec(text);
            if (!match) {
                throw new Error('CIDR line ' + (lineIndex + 1) + ' is invalid');
            }
            var octets = [];
            for (var octetIndex = 1; octetIndex <= 4; octetIndex++) {
                var octet = parseInt(match[octetIndex], 10);
                if (!this._integer(octet, 0, 255)) {
                    throw new Error('CIDR line ' + (lineIndex + 1) + ' is invalid');
                }
                octets.push(octet);
            }
            var bits = parseInt(match[5], 10);
            if (!this._integer(bits, 0, 32)) {
                throw new Error('CIDR line ' + (lineIndex + 1) + ' is invalid');
            }
            var address = ((octets[0] * 256 + octets[1]) * 256 + octets[2]) * 256 + octets[3];
            var size = Math.pow(2, 32 - bits);
            var start = Math.floor(address / size) * size;
            result.push({start: start, end: start + size - 1, bits: bits, cidr: this._ipv4Text(start) + '/' + bits});
        }
        if (required && result.length === 0) {
            throw new Error('at least one IPv4 CIDR is required');
        }
        if (result.length > 4096) {
            throw new Error('target scope exceeds 4096 CIDR entries');
        }
        return result;
    },

    _normalizeIPv4Ranges: function (ranges) {
        ranges.sort(function (left, right) {
            return left.start === right.start ? right.end - left.end : left.start - right.start;
        });
        var result = [];
        for (var index = 0; index < ranges.length; index++) {
            var current = ranges[index];
            var previous = result.length > 0 ? result[result.length - 1] : null;
            if (previous && previous.start <= current.start && previous.end >= current.end) {
                continue;
            }
            result.push(current);
        }
        return result;
    },

    _partitionIPv4Range: function (range, partitionPrefix, budget) {
        var result = [];
        var maximumBlock = Math.pow(2, 32 - partitionPrefix);
        var current = range.start;
        while (current <= range.end) {
            if (budget - result.length <= 0) {
                throw new Error('target scope exceeds 100000 deterministic partitions');
            }
            var block = 1;
            while (block < 4294967296 && current % (block * 2) === 0) {
                block *= 2;
            }
            block = Math.min(block, maximumBlock);
            while (current + block - 1 > range.end) {
                block /= 2;
            }
            var bits = 32 - Math.round(Math.log(block) / Math.LN2);
            result.push(this._ipv4Text(current) + '/' + bits);
            current += block;
        }
        return result;
    },

    _ipv4Text: function (value) {
        var first = Math.floor(value / 16777216);
        value -= first * 16777216;
        var second = Math.floor(value / 65536);
        value -= second * 65536;
        var third = Math.floor(value / 256);
        var fourth = value - third * 256;
        return first + '.' + second + '.' + third + '.' + fourth;
    },

    _only: function (value, fields) {
        if (!value || typeof value !== 'object' || Array.isArray(value)) {
            return false;
        }
        var allowed = {};
        var i;
        for (i = 0; i < fields.length; i++) {
            allowed[fields[i]] = true;
        }
        for (var key in value) {
            if (Object.prototype.hasOwnProperty.call(value, key) && !allowed[key]) {
                return false;
            }
        }
        return true;
    },

    _capabilities: function (capabilities) {
        if (!Array.isArray(capabilities) || capabilities.length < 1 || capabilities.length > 2) {
            return false;
        }
        var present = {};
        for (var index = 0; index < capabilities.length; index++) {
            if ([this.OPERATION_LOCAL, this.OPERATION_SSH_LINUX].indexOf(capabilities[index]) < 0 || present[capabilities[index]]) {
                return false;
            }
            present[capabilities[index]] = true;
        }
        var result = [];
        if (present[this.OPERATION_LOCAL]) {
            result.push(this.OPERATION_LOCAL);
        }
        if (present[this.OPERATION_SSH_LINUX]) {
            result.push(this.OPERATION_SSH_LINUX);
        }
        return result;
    },

    _sshUsername: function (value) {
        return typeof value === 'string' && /^[A-Za-z0-9._-]{1,64}$/.test(value);
    },

    _safeID: function (value) {
        return typeof value === 'string' && /^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/.test(value);
    },

    _short: function (value, limit) {
        return typeof value === 'string' && value.length > 0 && value.length <= limit && !/[\u0000-\u001f\u007f]/.test(value);
    },

    _hex: function (value, length) {
        return typeof value === 'string' && new RegExp('^[A-Fa-f0-9]{' + length + '}$').test(value);
    },

    _integer: function (value, minimum, maximum) {
        return typeof value === 'number' && isFinite(value) && Math.floor(value) === value && value >= minimum && value <= maximum;
    },

    _boundedInteger: function (value, minimum, maximum, fallback) {
        var parsed = parseInt(value, 10);
        return isNaN(parsed) ? fallback : Math.max(minimum, Math.min(parsed, maximum));
    },

    _sha256: function (value) {
        return String(new GlideDigest().getSHA256Hex(String(value))).toLowerCase();
    },

    _utf8Length: function (value) {
        var length = 0;
        for (var i = 0; i < value.length; i++) {
            var code = value.charCodeAt(i);
            if (code < 128) {
                length++;
            } else if (code < 2048) {
                length += 2;
            } else if (code >= 55296 && code <= 56319 && i + 1 < value.length &&
                    value.charCodeAt(i + 1) >= 56320 && value.charCodeAt(i + 1) <= 57343) {
                length += 4;
                i++;
            } else {
                length += 3;
            }
        }
        return length;
    },

    _dateOrNow: function (value) {
        if (typeof value !== 'string' || !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z$/.test(value)) {
            return new GlideDateTime();
        }
        return new GlideDateTime(value.replace('T', ' ').replace(/(?:\.\d+)?Z$/, ''));
    },

    _iso: function (value) {
        return String(value.getValue()).replace(' ', 'T') + 'Z';
    },

    _isTrue: function (value) {
        return String(value) === 'true' || String(value) === '1';
    },

    _boundedError: function (error) {
        var message = error && error.message ? String(error.message) : String(error || 'internal processing error');
        return message.replace(/[\u0000-\u001f\u007f]/g, ' ').substring(0, 4000);
    },

    _result: function (status, body) {
        return {status: status, body: body};
    },

    type: 'TopoControlPlane'
};
