'use strict'

// Execute the production broker and lease validator together. This is a
// deterministic platform fixture, not a claim about live ServiceNow ACLs.
const assert = require('node:assert/strict')
const crypto = require('node:crypto')
const fs = require('node:fs')
const path = require('node:path')
const vm = require('node:vm')
const hash = value => crypto.createHash('sha256').update(value).digest('hex')
let decrypts = 0
let registered = true
const credential = {
    isValidRecord: () => true, u_active: 'true', u_username: 'SERVER\\topo-scan',
    u_password: { getDecryptedValue() { decrypts++; return 'synthetic-password' } },
}
const binding = {
    isValidRecord: () => true, u_active: 'true', u_protocol: 'winrm_ntlm_password',
    u_profile_id: 'windows', u_profile_revision: '1', u_target_scope: 'scope-1',
    u_credential: { getRefRecord: () => credential },
}
const task = {
    u_task_id: 'task-1', u_lease_worker: 'worker-record', u_lease_boot_id: 'boot-1',
    u_attempt_id: 'attempt-1', u_lease_token_digest: hash('lease-1'),
    u_state: 'leased', u_lease_expires: '2000', u_cancel_requested: 'false',
    u_operation: 'winrm_windows.v1', u_profile_id: 'windows', u_profile_revision: '1',
    u_target_scope: 'scope-1',
    u_credential_binding: { toString: () => 'binding-1', getRefRecord: () => binding },
}
const context = {
    Class: { create: () => function () { this.initialize() } }, global: { JSON },
    GlideDateTime: function (value) {
        this.value = value === undefined ? 1000 : Number(value)
        this.compareTo = other => Math.sign(this.value - other.value)
    },
    GlideRecord: function (table) {
        assert.equal(table, 'x_664635_topo_task')
        const predicates = []
        this.addQuery = (field, value) => predicates.push([field, value])
        this.setLimit = limit => assert.equal(limit, 1)
        this.query = () => {}
        this.next = () => {
            if (!predicates.every(([field, value]) => String(task[field]) === value)) return false
            Object.assign(this, task)
            return true
        }
    },
}
vm.createContext(context)
vm.runInContext(fs.readFileSync(path.join(__dirname, '..', 'scripts', 'TopoControlPlane.js'), 'utf8'), context)
const control = new context.TopoControlPlane()
control._sha256 = hash
control._workerForCaller = (worker, boot) => registered && worker === 'worker-1' && boot === 'boot-1'
    ? { getUniqueValue: () => 'worker-record' } : null
control._recordCredentialAccess = () => true
const request = { schema_version: 'v1alpha1', worker_id: 'worker-1', boot_id: 'boot-1', attempt_id: 'attempt-1', lease_token: 'lease-1' }
assert.equal(control.credential('task-1', request).status, 200)
assert.equal(decrypts, 1)
function denied(body, status, id = 'task-1') {
    const before = decrypts
    const result = control.credential(id, body)
    assert.equal(result.status, status)
    assert.equal(decrypts, before, 'denial must precede Password2 decryption')
    assert.doesNotMatch(JSON.stringify(result), /synthetic-password|lease-1/)
}
for (const field of ['worker_id', 'boot_id', 'attempt_id', 'lease_token']) {
    denied({ ...request, [field]: 'wrong' }, 403)
}
denied(request, 403, 'other-task')
registered = false
denied(request, 403)
registered = true
for (const state of ['queued', 'cancelled', 'complete', 'failed', 'ire_processing']) {
    task.u_state = state
    denied(request, 409)
}
task.u_state = 'leased'
for (const expires of ['999', '1000']) {
    task.u_lease_expires = expires
    denied(request, 409)
}
task.u_lease_expires = '2000'
task.u_cancel_requested = 'true'
denied(request, 409)
task.u_cancel_requested = 'false'
// Renewed attempts cannot reuse a prior attempt's authority.
task.u_attempt_id = 'attempt-2'
task.u_lease_token_digest = hash('lease-2')
denied(request, 403)
const renewed = { ...request, attempt_id: 'attempt-2', lease_token: 'lease-2' }
assert.equal(control.credential('task-1', renewed).status, 200)
for (const [field, value] of [['u_protocol', 'ssh_password'], ['u_profile_id', 'other'], ['u_profile_revision', '2'], ['u_target_scope', 'other']]) {
    const original = binding[field]
    binding[field] = value
    denied(renewed, 409)
    binding[field] = original
}
credential.u_password.getDecryptedValue = () => { throw new Error('synthetic-password') }
denied(renewed, 500)
console.log('Windows broker lease/attempt authority and secret-free denial fixtures passed')

// ServiceNow updateMultiple ignores JavaScript null for existing string values.
// Its documented 'NULL' sentinel clears them. Model that distinction so a
// cancelled expired task cannot retain the unique lease slots and block claims.
const clearFields = ['u_attempt_id', 'u_lease_worker', 'u_lease_boot_id', 'u_lease_token_digest', 'u_pool_lease_slot', 'u_worker_lease_slot', 'u_lease_expires']
const rows = [true, false].map((cancelled, i) => ({
    sys_id: `expired-${i}`, u_state: 'leased', u_lease_expires: '999',
    u_cancel_requested: String(cancelled), u_worker_pool: 'pool-record',
    u_attempt_id: `old-attempt-${i}`, u_lease_worker: 'old-worker',
    u_lease_boot_id: 'old-boot', u_lease_token_digest: 'old-digest',
    u_pool_lease_slot: `pool-record:${i}`, u_worker_lease_slot: `old-worker:${i}`,
    u_run: { getRefRecord: () => ({ id: `run-${i}` }) },
}))
rows.push({ ...rows[0], sys_id: 'future', u_lease_expires: '2000', u_pool_lease_slot: 'other-pool:0', u_worker_pool: 'other-pool' })
context.GlideRecord = function (table) {
    assert.equal(table, 'x_664635_topo_task')
    const queries = [], changes = {}
    let found = [], cursor = 0
    this.addQuery = (field, op, value) => queries.push([field, value === undefined ? '=' : op, value === undefined ? op : value])
    const matches = row => queries.every(([field, op, value]) => op === 'IN'
        ? String(value).split(',').includes(String(row[field]))
        : op === '<=' ? Number(row[field]) <= Number(value.value ?? value)
        : String(row[field]) === String(value))
    this.setLimit = () => {}
    this.orderBy = () => {}
    this.query = () => { found = rows.filter(matches); cursor = 0 }
    this.next = () => { if (cursor >= found.length) return false; Object.assign(this, found[cursor++]); return true }
    this.getUniqueValue = () => this.sys_id
    this.get = id => { const row = rows.find(row => row.sys_id === id); if (!row) return false; Object.assign(this, row); return true }
    this.setValue = (field, value) => { if (value !== null && value !== undefined) changes[field] = value === 'NULL' ? '' : value }
    this.updateMultiple = () => { for (const row of rows.filter(matches)) Object.assign(row, changes) }
}
const marked = [], refreshed = []
control._markAttemptResults = (...args) => marked.push(args)
control._refreshRun = run => refreshed.push(run.id)
control._expireLeases('pool-record')
for (const [i, row] of rows.slice(0, 2).entries()) {
    assert.equal(row.u_state, i === 0 ? 'cancelled' : 'ready')
    for (const field of clearFields) assert.equal(row[field], '', `${field} must be physically cleared after expiry`)
    assert.equal(row.u_cancel_requested, false)
}
assert.equal(rows[2].u_state, 'leased', 'unexpired unrelated lease must remain untouched')
assert.equal(rows[2].u_attempt_id, 'old-attempt-0')
assert.equal(marked.length, 2)
assert.deepEqual(marked.map(args => args[2]), ['failed', 'superseded'])
assert.deepEqual(refreshed, ['run-0', 'run-1'])
const slots = control._availableLeaseSlots({ getUniqueValue: () => 'pool-record', u_max_leases: '1' }, { getUniqueValue: () => 'new-worker', u_max_concurrency: '1' })
assert.equal(slots.pool, 'pool-record:0', 'next task must be able to reserve the released pool slot')
console.log('Expired/cancelled lease physical-null clearing and next-task capacity regression passed')
