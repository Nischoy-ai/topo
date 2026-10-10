'use strict'

const assert = require('node:assert/strict')
const fs = require('node:fs')
const path = require('node:path')
const vm = require('node:vm')

function classContext() {
    const context = {
        Class: {
            create() {
                return function Constructor() {
                    if (typeof this.initialize === 'function') this.initialize()
                }
            },
        },
        global: { JSON },
    }
    vm.createContext(context)
    return context
}

const controlContext = classContext()
vm.runInContext(
    fs.readFileSync(path.join(__dirname, '..', 'scripts', 'TopoControlPlane.js'), 'utf8'),
    controlContext,
    { filename: 'TopoControlPlane.js' },
)
const control = new controlContext.TopoControlPlane()
assert.deepEqual(Array.from(control._capabilities(['local.v1'])), ['local.v1'])
assert.deepEqual(Array.from(control._capabilities(['local.v1', 'ssh_linux.v1'])), ['local.v1', 'ssh_linux.v1'])
assert.deepEqual(Array.from(control._capabilities(['ssh_linux.v1'])), ['ssh_linux.v1'])
assert.deepEqual(Array.from(control._capabilities(['ssh_linux.v1', 'local.v1'])), ['local.v1', 'ssh_linux.v1'])
assert.equal(control._capabilities(['ssh_linux.v1', 'ssh_linux.v1']), false)
assert.equal(control._capabilities(['shell.v1']), false)
assert.equal(control._sshUsername('topo_discovery'), true)
assert.equal(control._sshUsername('root;id'), false)

const mapperContext = classContext()
vm.runInContext(
    fs.readFileSync(path.join(__dirname, '..', 'scripts', 'TopoObservationMapper.js'), 'utf8'),
    mapperContext,
    { filename: 'TopoObservationMapper.js' },
)
const mapper = new mapperContext.TopoObservationMapper()
const task = {
    u_operation: 'ssh_linux.v1',
    u_task_id: 'task-1',
    u_worker_pool: {
        getRefRecord() {
            return { isValidRecord: () => true, u_site_id: 'site-a', u_pool_id: 'pool-a' }
        },
    },
}
const noData = {
    schema_version: 'v1alpha1',
    observation_id: 'observation-1',
    site_id: 'site-a',
    collector_id: 'worker-pool-pool-a',
    plugin: 'ssh-linux',
    job_id: 'task-1',
    observed_at: '2026-08-30T12:00:00Z',
    assets: [],
    relationships: [],
    errors: [{ code: 'ssh_connect', message: 'target unavailable', retryable: true }],
}
const mapped = mapper.validateAndMap(JSON.stringify(noData), task)
assert.equal(mapped.assets, 0)
assert.equal(mapped.relationships, 0)
assert.equal(mapped.collection_errors, 1)
assert.throws(() => mapper.validateAndMap(JSON.stringify({ ...noData, errors: [] }), task), /empty observation/)
assert.throws(() => mapper.validateAndMap(JSON.stringify({ ...noData, plugin: 'local-host' }), task), /does not match/)

// A real Ubuntu lab host returned 658 packages. These bounded string lists
// travel as host evidence but must never become arbitrary IRE fields or CIs.
const hostInventory = {
    ...noData,
    errors: [],
    assets: [{ type: 'host', native_id: 'host-1', name: 'lab-linux', attributes: {
        packages: Array.from({ length: 658 }, (_, i) => `package-${i}`),
        services: Array.from({ length: 158 }, (_, i) => `service-${i}`),
    } }],
}
const hostMapped = mapper.validateAndMap(JSON.stringify(hostInventory), task)
assert.equal(hostMapped.assets, 1)
assert.equal(hostMapped.payload.items[0].className, 'cmdb_ci_computer')
assert.deepEqual(Object.keys(hostMapped.payload.items[0].values).sort(),
    ['discovery_source', 'last_discovered', 'name'])
function withAttributes(attributes, type = 'host') {
    return JSON.stringify({ ...hostInventory, assets: [{ ...hostInventory.assets[0], type, attributes }] })
}
for (const key of ['packages', 'services']) {
    assert.equal(mapper.validateAndMap(withAttributes({ [key]: Array(4096).fill('entry') }), task).assets, 1)
    for (const invalid of [Array(4097).fill('entry'), ['bad\nentry'], ['x'.repeat(4097)], [12], [{}], [['nested']], 'not-a-list']) {
        assert.throws(() => mapper.validateAndMap(withAttributes({ [key]: invalid }), task), /host inventory/)
    }
    assert.equal(mapper.validateAndMap(withAttributes({ [key]: null }), task).assets, 1)
    assert.throws(() => mapper.validateAndMap(withAttributes({ [key]: Array(257).fill('entry') }, 'network_interface'), task), /asset attribute/)
}
assert.throws(() => mapper.validateAndMap(withAttributes({ unreviewed: Array(257).fill('entry') }), task), /asset attribute/)
assert.throws(() => mapper.validateAndMap(withAttributes({ nested: { packages: Array(257).fill('entry') } }), task), /asset attribute/)

const route = fs.readFileSync(path.join(__dirname, '..', 'scripts', 'credential_task.js'), 'utf8')
assert.match(route, /Cache-Control', 'no-store'/)
assert.match(route, /Pragma', 'no-cache'/)

const controlSource = fs.readFileSync(path.join(__dirname, '..', 'scripts', 'TopoControlPlane.js'), 'utf8')
assert.match(controlSource, /u_password\.getDecryptedValue\(\)/)
assert.match(controlSource, /_recordCredentialAccess/)
assert.doesNotMatch(controlSource, /vault:/i)

const credentialSource = controlSource.slice(
    controlSource.indexOf('credential: function'),
    controlSource.indexOf('ingestResult: function'),
)
assert.match(credentialSource, /u_cancel_requested/)
assert.match(credentialSource, /task_cancelled/)
assert.match(credentialSource, /return this\._result\(409, \{error: 'task is cancelled'\}\)/)

const expirySource = controlSource.slice(
    controlSource.indexOf('_expireLeases: function'),
    controlSource.indexOf('_markAttemptResults: function'),
)
for (const field of [
    'u_attempt_id',
    'u_lease_worker',
    'u_lease_boot_id',
    'u_lease_token_digest',
    'u_pool_lease_slot',
    'u_worker_lease_slot',
    'u_lease_expires',
]) {
    assert.match(expirySource, new RegExp(`setValue\\('${field}', null\\)`))
    assert.doesNotMatch(expirySource, new RegExp(`setValue\\('${field}', ''\\)`))
}

console.log('Password2 SSH contract tests passed')

// Windows uses the same protected Password2 storage, but protocol authority
// comes from the immutable binding and must match the leased task operation.
assert.deepEqual(Array.from(control._capabilities(['winrm_windows.v1', 'ssh_linux.v1', 'local.v1'])),
    ['local.v1', 'ssh_linux.v1', 'winrm_windows.v1'])
assert.equal(control._credentialProtocol('winrm_windows.v1'), 'winrm_ntlm_password')
assert.equal(control._credentialProtocol('shell.v1'), '')
assert.equal(control._credentialUsername('winrm_windows.v1', 'SERVER\\topo-scan'), true)
assert.equal(control._credentialUsername('ssh_linux.v1', 'SERVER\\topo-scan'), false)
assert.equal(control._credentialUsername('winrm_windows.v1', 'topo;whoami'), false)
assert.equal(control._credentialUsername('winrm_windows.v1', 'topo\nuser'), false)
const winTask = { ...task, u_operation: 'winrm_windows.v1' }
const winObservation = { ...hostInventory, plugin: 'winrm-windows' }
assert.equal(mapper.validateAndMap(JSON.stringify(winObservation), winTask).assets, 1)
assert.throws(() => mapper.validateAndMap(JSON.stringify(winObservation), task), /does not match/)
assert.equal(mapper.validateAndMap(JSON.stringify({ ...noData, plugin: 'winrm-windows' }), winTask).assets, 0)

let decrypts = 0
let auditAllowed = true
const credential = {
    isValidRecord: () => true, u_active: 'true', u_username: 'SERVER\\topo-scan',
    u_password: { getDecryptedValue() { decrypts++; return 'synthetic-pilot-password' } },
}
const binding = {
    isValidRecord: () => true, u_active: 'true', u_protocol: 'winrm_ntlm_password',
    u_profile_id: 'windows', u_profile_revision: '1', u_target_scope: 'scope-1',
    u_credential: { getRefRecord: () => credential },
}
const leasedTask = {
    u_operation: 'winrm_windows.v1', u_cancel_requested: 'false',
    u_credential_binding: { toString: () => 'binding-1', getRefRecord: () => binding },
    u_target_scope: 'scope-1', u_profile_id: 'windows', u_profile_revision: '1',
}
control._ownedLease = () => ({ ok: true, task: leasedTask })
control._recordCredentialAccess = () => auditAllowed
const credentialRequest = { schema_version: 'v1alpha1', worker_id: 'worker-1', boot_id: 'boot-1', attempt_id: 'attempt-1', lease_token: 'lease-1' }
assert.equal(control.credential('task-1', credentialRequest).status, 200)
assert.equal(decrypts, 1)
for (const [field, bad] of [['u_protocol', 'ssh_password'], ['u_profile_id', 'other'], ['u_profile_revision', '2'], ['u_target_scope', 'other'], ['u_active', 'false']]) {
    const before = decrypts
    const original = binding[field]; binding[field] = bad
    assert.equal(control.credential('task-1', credentialRequest).status, 409)
    assert.equal(decrypts, before)
    binding[field] = original
}
leasedTask.u_cancel_requested = 'true'
assert.equal(control.credential('task-1', credentialRequest).status, 409)
leasedTask.u_cancel_requested = 'false'
auditAllowed = false
assert.equal(control.credential('task-1', credentialRequest).status, 500)
control._ownedLease = () => ({ ok: false, result: { status: 409, body: { error: 'stale lease' } } })
const beforeStale = decrypts
assert.equal(control.credential('task-1', credentialRequest).status, 409)
assert.equal(decrypts, beforeStale)
console.log('Windows Password2 protocol authority and mapper tests passed')
