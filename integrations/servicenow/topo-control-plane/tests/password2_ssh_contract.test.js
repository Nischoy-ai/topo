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

const route = fs.readFileSync(path.join(__dirname, '..', 'scripts', 'credential_task.js'), 'utf8')
assert.match(route, /Cache-Control', 'no-store'/)
assert.match(route, /Pragma', 'no-cache'/)

const controlSource = fs.readFileSync(path.join(__dirname, '..', 'scripts', 'TopoControlPlane.js'), 'utf8')
assert.match(controlSource, /u_password\.getDecryptedValue\(\)/)
assert.match(controlSource, /_recordCredentialAccess/)
assert.doesNotMatch(controlSource, /vault:/i)

console.log('Password2 SSH contract tests passed')
