var TopoObservationMapper = Class.create();
TopoObservationMapper.prototype = {
    MAX_ITEMS: 1000,
    MAX_RELATIONS: 2000,
    MAX_IDENTITY: 1024,
    MAX_NAME: 1024,

    initialize: function () {},

    validateAndMap: function (raw, task) {
        if (typeof raw !== 'string' || raw.length === 0 || this._depth(raw) > 64) {
            throw new Error('observation is empty or exceeds the JSON nesting bound');
        }
        var envelope;
        try {
            envelope = global.JSON.parse(raw);
        } catch (error) {
            throw new Error('observation is not valid JSON');
        }
        if (!this._only(envelope, ['schema_version', 'observation_id', 'site_id', 'collector_id', 'plugin', 'job_id', 'observed_at', 'assets', 'relationships', 'errors', 'labels']) ||
                envelope.schema_version !== 'v1alpha1' || !this._identity(envelope.observation_id) ||
                !this._identity(envelope.site_id) || !this._identity(envelope.collector_id) ||
                envelope.plugin !== 'local-host' || !this._timestamp(envelope.observed_at) ||
                String(envelope.job_id || '') !== String(task.u_task_id)) {
            throw new Error('observation envelope does not match the leased local.v1 task');
        }
        var pool = task.u_worker_pool.getRefRecord();
        if (!pool.isValidRecord() || String(envelope.site_id) !== String(pool.u_site_id) ||
                String(envelope.collector_id) !== 'worker-pool-' + String(pool.u_pool_id)) {
            throw new Error('observation site or collector identity does not match the worker pool');
        }
        if (!Array.isArray(envelope.assets) || envelope.assets.length < 1 || envelope.assets.length > this.MAX_ITEMS ||
                (typeof envelope.relationships !== 'undefined' && (!Array.isArray(envelope.relationships) || envelope.relationships.length > this.MAX_RELATIONS)) ||
                (typeof envelope.errors !== 'undefined' && (!Array.isArray(envelope.errors) || envelope.errors.length > 100))) {
            throw new Error('observation item, relationship, or collection-error bounds were exceeded');
        }
        this._validateStringMap(envelope.labels, 32, 128, 1024, true);

        var items = [];
        var itemIndex = {};
        var itemType = {};
        for (var i = 0; i < envelope.assets.length; i++) {
            var asset = envelope.assets[i];
            if (!this._only(asset, ['type', 'native_id', 'name', 'identifiers', 'attributes', 'evidence']) ||
                    (asset.type !== 'host' && asset.type !== 'network_interface') ||
                    !this._identity(asset.native_id) || !this._name(String(asset.name || ''))) {
                throw new Error('observation contains an invalid or unsupported asset');
            }
            this._validateStringMap(asset.identifiers, 64, 128, this.MAX_IDENTITY, true);
            this._validateAttributes(asset.attributes);
            this._validateEvidence(asset.evidence);
            if (Object.prototype.hasOwnProperty.call(itemType, asset.native_id) && itemType[asset.native_id] !== asset.type) {
                throw new Error('one source_native_key changes ServiceNow class within the observation');
            }
            itemType[asset.native_id] = asset.type;
            var values = {
                name: String(asset.name || ''),
                discovery_source: 'Nischoy Topo',
                last_discovered: this._serviceNowDate(envelope.observed_at)
            };
            if (asset.type === 'network_interface' && asset.attributes && typeof asset.attributes.mac_address !== 'undefined') {
                if (typeof asset.attributes.mac_address !== 'string' || asset.attributes.mac_address.length > 128 || this._control(asset.attributes.mac_address)) {
                    throw new Error('network interface mac_address is invalid');
                }
                if (asset.attributes.mac_address) {
                    values.mac_address = asset.attributes.mac_address;
                }
            }
            var item = {
                className: asset.type === 'host' ? 'cmdb_ci_computer' : 'cmdb_ci_network_adapter',
                values: values,
                sys_object_source_info: {
                    source_name: 'Nischoy Topo',
                    source_native_key: asset.native_id
                }
            };
            if (Object.prototype.hasOwnProperty.call(itemIndex, asset.native_id)) {
                items[itemIndex[asset.native_id]] = item;
            } else {
                itemIndex[asset.native_id] = items.length;
                items.push(item);
            }
        }

        var relations = [];
        var seen = {};
        var sourceRelations = envelope.relationships || [];
        for (var relationIndex = 0; relationIndex < sourceRelations.length; relationIndex++) {
            var relation = sourceRelations[relationIndex];
            if (!this._only(relation, ['type', 'from_native_id', 'to_native_id', 'evidence']) ||
                    relation.type !== 'host_has_interface' || !this._identity(relation.from_native_id) ||
                    !this._identity(relation.to_native_id) || itemType[relation.from_native_id] !== 'host' ||
                    itemType[relation.to_native_id] !== 'network_interface') {
                throw new Error('observation contains an invalid or unsupported relationship');
            }
            this._validateEvidence(relation.evidence);
            var relationKey = relation.type + '\u0000' + relation.from_native_id + '\u0000' + relation.to_native_id;
            if (seen[relationKey]) {
                continue;
            }
            seen[relationKey] = true;
            relations.push({
                type: 'Owns::Owned by',
                parent: itemIndex[relation.from_native_id],
                child: itemIndex[relation.to_native_id]
            });
            if (relations.length > this.MAX_RELATIONS) {
                throw new Error('observation exceeds the unique relationship bound');
            }
        }
        this._validateCollectionErrors(envelope.errors);
        return {
            payload: {items: items, relations: relations},
            assets: items.length,
            relationships: relations.length,
            collection_errors: envelope.errors ? envelope.errors.length : 0
        };
    },

    _validateAttributes: function (attributes) {
        if (typeof attributes === 'undefined' || attributes === null) {
            return;
        }
        if (!this._plainObject(attributes) || this._keys(attributes).length > 64) {
            throw new Error('asset attributes exceed their structural bound');
        }
        var keys = this._keys(attributes);
        for (var i = 0; i < keys.length; i++) {
            if (!this._name(keys[i]) || !this._boundedValue(attributes[keys[i]], 0)) {
                throw new Error('asset attribute is invalid or too deeply nested');
            }
        }
    },

    _boundedValue: function (value, depth) {
        if (depth > 8) {
            return false;
        }
        if (value === null || typeof value === 'boolean') {
            return true;
        }
        if (typeof value === 'number') {
            return isFinite(value);
        }
        if (typeof value === 'string') {
            return value.length <= 4096 && !this._control(value);
        }
        if (Array.isArray(value)) {
            if (value.length > 256) {
                return false;
            }
            for (var i = 0; i < value.length; i++) {
                if (!this._boundedValue(value[i], depth + 1)) {
                    return false;
                }
            }
            return true;
        }
        if (this._plainObject(value)) {
            var keys = this._keys(value);
            if (keys.length > 64) {
                return false;
            }
            for (var keyIndex = 0; keyIndex < keys.length; keyIndex++) {
                if (!this._name(keys[keyIndex]) || !this._boundedValue(value[keys[keyIndex]], depth + 1)) {
                    return false;
                }
            }
            return true;
        }
        return false;
    },

    _validateEvidence: function (evidence) {
        if (typeof evidence === 'undefined' || evidence === null) {
            return;
        }
        if (!Array.isArray(evidence) || evidence.length > 32) {
            throw new Error('asset evidence exceeds its bound');
        }
        for (var i = 0; i < evidence.length; i++) {
            var entry = evidence[i];
            if (!this._only(entry, ['source', 'collected_at', 'path', 'confidence']) ||
                    !this._identity(entry.source) || !this._timestamp(entry.collected_at) ||
                    (typeof entry.path !== 'undefined' && !this._name(entry.path)) ||
                    typeof entry.confidence !== 'number' || !isFinite(entry.confidence) ||
                    entry.confidence < 0 || entry.confidence > 1) {
                throw new Error('asset evidence is invalid');
            }
        }
    },

    _validateCollectionErrors: function (errors) {
        if (typeof errors === 'undefined' || errors === null) {
            return;
        }
        for (var i = 0; i < errors.length; i++) {
            var entry = errors[i];
            if (!this._only(entry, ['code', 'message', 'retryable']) || !this._identity(entry.code) ||
                    typeof entry.message !== 'string' || entry.message.length > 1000 || this._control(entry.message) ||
                    typeof entry.retryable !== 'boolean') {
                throw new Error('collection error is invalid');
            }
        }
    },

    _validateStringMap: function (value, maxKeys, maxKeyLength, maxValueLength, optional) {
        if ((typeof value === 'undefined' || value === null) && optional) {
            return;
        }
        if (!this._plainObject(value)) {
            throw new Error('observation map is invalid');
        }
        var keys = this._keys(value);
        if (keys.length > maxKeys) {
            throw new Error('observation map exceeds its entry bound');
        }
        for (var i = 0; i < keys.length; i++) {
            if (keys[i].length === 0 || keys[i].length > maxKeyLength || this._control(keys[i]) ||
                    typeof value[keys[i]] !== 'string' || value[keys[i]].length > maxValueLength || this._control(value[keys[i]])) {
                throw new Error('observation map entry is invalid');
            }
        }
    },

    _depth: function (raw) {
        var depth = 0;
        var maximum = 0;
        var quoted = false;
        var escaped = false;
        for (var i = 0; i < raw.length; i++) {
            var character = raw.charAt(i);
            if (quoted) {
                if (escaped) {
                    escaped = false;
                } else if (character === '\\') {
                    escaped = true;
                } else if (character === '"') {
                    quoted = false;
                }
                continue;
            }
            if (character === '"') {
                quoted = true;
            } else if (character === '{' || character === '[') {
                depth++;
                maximum = Math.max(maximum, depth);
            } else if (character === '}' || character === ']') {
                depth--;
            }
        }
        return maximum;
    },

    _only: function (value, fields) {
        if (!this._plainObject(value)) {
            return false;
        }
        var allowed = {};
        for (var i = 0; i < fields.length; i++) {
            allowed[fields[i]] = true;
        }
        var keys = this._keys(value);
        for (var keyIndex = 0; keyIndex < keys.length; keyIndex++) {
            if (!allowed[keys[keyIndex]]) {
                return false;
            }
        }
        return true;
    },

    _plainObject: function (value) {
        return value !== null && typeof value === 'object' && !Array.isArray(value);
    },

    _keys: function (value) {
        var keys = [];
        for (var key in value) {
            if (Object.prototype.hasOwnProperty.call(value, key)) {
                keys.push(key);
            }
        }
        return keys;
    },

    _identity: function (value) {
        return typeof value === 'string' && value.length > 0 && value.length <= this.MAX_IDENTITY && !this._control(value);
    },

    _name: function (value) {
        return typeof value === 'string' && value.length <= this.MAX_NAME && !this._control(value);
    },

    _control: function (value) {
        return /[\u0000-\u001f\u007f]/.test(value);
    },

    _timestamp: function (value) {
        return typeof value === 'string' && /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z$/.test(value);
    },

    _serviceNowDate: function (value) {
        return value.substring(0, 19).replace('T', ' ');
    },

    type: 'TopoObservationMapper'
};
