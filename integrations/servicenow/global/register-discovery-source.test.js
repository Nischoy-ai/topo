'use strict';
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const source = fs.readFileSync(__dirname + '/register-discovery-source.js', 'utf8');

function run(rows, failInsert = false) {
    const messages = [];
    class GlideRecord {
        constructor(table) { assert.equal(table, 'sys_choice'); this.filters = []; }
        addQuery(field, value) { this.filters.push([field, value]); }
        setLimit(limit) { assert.equal(limit, 2); this.limit = limit; }
        query() {
            this.matches = rows.filter(row => this.filters.every(([field, value]) => row[field] === value)).slice(0, this.limit);
            this.index = -1;
        }
        next() { this.index++; return this.index < this.matches.length; }
        getValue(field) { return this.matches[this.index][field]; }
        initialize() { this.newRow = {}; }
        setValue(field, value) { this.newRow[field] = field === 'inactive' ? String(value) : value; }
        insert() { if (failInsert) return null; rows.push({...this.newRow}); return 'new-choice-id'; }
    }
    vm.runInNewContext(source, {GlideRecord, gs: {info: message => messages.push(message)}}, {timeout: 1000});
    return messages;
}

const topo = {name: 'cmdb_ci', element: 'discovery_source', value: 'Nischoy Topo', label: 'Nischoy Topo', language: 'en', dependent_value: '', sys_domain: 'global', inactive: 'false'};
const other = [
    {...topo, value: 'Customer source', label: 'Customer label'},
    {...topo, language: 'fr', label: 'Libellé personnalisé'},
    {...topo, name: 'u_customer_table'},
    {...topo, dependent_value: 'customer'},
    {...topo, sys_domain: 'customer-domain'}
];
let rows = structuredClone(other);
run(rows);
assert.deepEqual(rows, [...other, topo]);
const before = structuredClone(rows);
run(rows);
assert.deepEqual(rows, before, 'repeat must preserve every field and avoid duplicate insert');
rows = [{...topo, label: 'Customer-maintained label', inactive: '0'}, ...structuredClone(other)];
const customBefore = structuredClone(rows);
run(rows);
assert.deepEqual(rows, customBefore, 'existing active source must not be overwritten');
for (const conflicting of [[{...topo, inactive: 'true'}], [topo, {...topo}]]) {
    rows = structuredClone(conflicting);
    const before = structuredClone(rows);
    assert.throws(() => run(rows), /inactive|duplicate/);
    assert.deepEqual(rows, before);
}
rows = structuredClone(other);
assert.throws(() => run(rows, true), /could not register/);
assert.deepEqual(rows, other);
console.log('Discovery-source registration: creation, repeat, preservation and conflict tests passed.');
