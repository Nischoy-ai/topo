var TopoIREProcessor = Class.create();
TopoIREProcessor.prototype = {
    SUCCESS_RETENTION_SECONDS: 86400,
    FAILURE_RETENTION_SECONDS: 604800,
    MAX_IRE_REQUEST_BYTES: 4194304,
    MAX_IRE_RESPONSE_BYTES: 1048576,

    initialize: function () {},

    process: function (task, result) {
        var delivery = new GlideRecord('x_664635_topo_ire_delivery');
        delivery.addQuery('u_task', task.getUniqueValue());
        delivery.addQuery('u_attempt_id', String(task.u_attempt_id));
        delivery.setLimit(1);
        delivery.query();
        if (delivery.next()) {
            if (String(delivery.u_state) === 'applied' || String(delivery.u_state) === 'no_data') {
                return {
                    assets: parseInt(delivery.u_items, 10) || 0,
                    relationships: parseInt(delivery.u_relationships, 10) || 0,
                    collection_errors: 0
                };
            }
            throw new Error('IRE delivery already has a non-replayable terminal or in-progress outcome');
        }

        delivery.initialize();
        delivery.u_idempotency_key = this._sha256(String(task.u_task_id) + ':' + String(task.u_attempt_id));
        delivery.u_run = String(task.u_run);
        delivery.u_task = task.getUniqueValue();
        delivery.u_attempt_id = String(task.u_attempt_id);
        delivery.u_state = 'preflight';
        delivery.u_items = 0;
        delivery.u_relationships = 0;
        if (!delivery.insert()) {
            throw new Error('IRE delivery idempotency record could not be created');
        }

        var raw;
        var mapped;
        try {
            var attachment = new GlideRecord('sys_attachment');
            if (!attachment.get(String(result.u_attachment))) {
                throw new Error('result attachment is unavailable');
            }
            raw = new GlideSysAttachment().getContent(attachment);
            if (typeof raw !== 'string' || this._sha256(raw) !== String(result.u_checksum)) {
                throw new Error('stored result checksum validation failed');
            }
            mapped = new TopoObservationMapper().validateAndMap(raw, task);
        } catch (validationError) {
            this._finishDelivery(delivery, 'rejected', 'observation validation rejected before IRE');
            this._finishResult(result, 'failed', 'failed', this.FAILURE_RETENTION_SECONDS);
            throw new Error(this._boundedError(validationError));
        }

        if (mapped.assets === 0) {
            delivery.u_state = 'no_data';
            delivery.u_preflight_at = new GlideDateTime();
            delivery.u_items = 0;
            delivery.u_relationships = 0;
            delivery.u_diagnostics = 'validated SSH no-data observation; IRE preflight and apply skipped';
            delivery.update();
            this._finishResult(result, 'processed', 'complete', this.SUCCESS_RETENTION_SECONDS);
            return mapped;
        }

        var input = global.JSON.stringify(mapped.payload);
        if (this._utf8Length(input) > this.MAX_IRE_REQUEST_BYTES) {
            this._finishDelivery(delivery, 'rejected', 'mapped IRE request exceeded the byte bound');
            this._finishResult(result, 'failed', 'failed', this.FAILURE_RETENTION_SECONDS);
            throw new Error('mapped IRE request exceeds the byte bound');
        }
        delivery.u_items = mapped.assets;
        delivery.u_relationships = mapped.relationships;
        delivery.u_preflight_at = new GlideDateTime();
        delivery.update();

        var preflight;
        try {
            preflight = sn_cmdb.IdentificationEngine.identifyCIEnhanced('Nischoy Topo', input, {});
            preflight = this._parseResponse(preflight);
        } catch (preflightError) {
            this._finishDelivery(delivery, 'rejected', 'IRE preflight failed: ' + this._boundedError(preflightError));
            this._finishResult(result, 'failed', 'failed', this.FAILURE_RETENTION_SECONDS);
            throw new Error('IRE preflight failed: ' + this._boundedError(preflightError));
        }
        if (this._reports(preflight, 'hasError') || this._reports(preflight, 'hasWarning')) {
            this._finishDelivery(delivery, 'rejected', 'IRE preflight reported an error or warning; operations=' + this._operations(preflight));
            this._finishResult(result, 'failed', 'failed', this.FAILURE_RETENTION_SECONDS);
            throw new Error('IRE preflight reported an error or warning');
        }
        delivery.u_diagnostics = ('preflight clean; operations=' + this._operations(preflight)).substring(0, 4000);
        delivery.update();

        // Anything after this point may have committed. No exception or
        // malformed response is treated as retryable; the delivery becomes
        // ambiguous for operator investigation instead of replaying blindly.
        var applied;
        try {
            applied = sn_cmdb.IdentificationEngine.createOrUpdateCIEnhanced('Nischoy Topo', input, {});
            applied = this._parseResponse(applied);
        } catch (applyError) {
            this._finishDelivery(delivery, 'ambiguous', 'IRE apply outcome is ambiguous: ' + this._boundedError(applyError));
            this._finishResult(result, 'failed', 'ambiguous', this.FAILURE_RETENTION_SECONDS);
            throw new Error('IRE apply outcome is ambiguous and was not retried');
        }
        if (this._reports(applied, 'hasError') || this._reports(applied, 'hasWarning')) {
            this._finishDelivery(delivery, 'ambiguous', 'IRE apply response reported an error or warning; operations=' + this._operations(applied));
            this._finishResult(result, 'failed', 'ambiguous', this.FAILURE_RETENTION_SECONDS);
            throw new Error('IRE apply response is ambiguous and was not retried');
        }

        delivery.u_state = 'applied';
        delivery.u_applied_at = new GlideDateTime();
        delivery.u_diagnostics = ('preflight clean; apply clean; operations=' + this._operations(applied)).substring(0, 4000);
        delivery.update();
        this._finishResult(result, 'processed', 'complete', this.SUCCESS_RETENTION_SECONDS);
        return mapped;
    },

    _parseResponse: function (response) {
        var text = typeof response === 'string' ? response : global.JSON.stringify(response);
        if (!text || this._utf8Length(text) > this.MAX_IRE_RESPONSE_BYTES) {
            throw new Error('IRE response is empty or exceeds the byte bound');
        }
        try {
            return global.JSON.parse(text);
        } catch (error) {
            throw new Error('IRE returned malformed JSON');
        }
    },

    _reports: function (value, name) {
        if (Array.isArray(value)) {
            for (var i = 0; i < value.length; i++) {
                if (this._reports(value[i], name)) {
                    return true;
                }
            }
            return false;
        }
        if (value && typeof value === 'object') {
            for (var key in value) {
                if (!Object.prototype.hasOwnProperty.call(value, key)) {
                    continue;
                }
                if (key === name && value[key] === true) {
                    return true;
                }
                if (this._reports(value[key], name)) {
                    return true;
                }
            }
        }
        return false;
    },

    _operations: function (value) {
        var operations = [];
        this._collectOperations(value, operations);
        return operations.length ? operations.slice(0, 64).join(',') : 'not-reported';
    },

    _collectOperations: function (value, operations) {
        if (operations.length >= 64) {
            return;
        }
        if (Array.isArray(value)) {
            for (var i = 0; i < value.length; i++) {
                this._collectOperations(value[i], operations);
            }
            return;
        }
        if (value && typeof value === 'object') {
            for (var key in value) {
                if (!Object.prototype.hasOwnProperty.call(value, key)) {
                    continue;
                }
                if (key === 'operation' && typeof value[key] === 'string' && /^[A-Z_]{1,32}$/.test(value[key])) {
                    operations.push(value[key]);
                } else {
                    this._collectOperations(value[key], operations);
                }
            }
        }
    },

    _finishDelivery: function (delivery, state, diagnostic) {
        delivery.u_state = state;
        delivery.u_diagnostics = String(diagnostic).replace(/[\u0000-\u001f\u007f]/g, ' ').substring(0, 4000);
        delivery.update();
    },

    _finishResult: function (result, state, outcome, seconds) {
        var deleteAfter = new GlideDateTime();
        deleteAfter.addSeconds(seconds);
        result.u_processing_state = state;
        result.u_terminal_outcome = outcome;
        result.u_processed_at = new GlideDateTime();
        result.u_delete_after = deleteAfter;
        result.update();
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

    _boundedError: function (error) {
        var message = error && error.message ? String(error.message) : String(error || 'internal processing error');
        return message.replace(/[\u0000-\u001f\u007f]/g, ' ').substring(0, 1000);
    },

    type: 'TopoIREProcessor'
};
