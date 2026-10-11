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
