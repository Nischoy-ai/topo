'use strict'

const assert = require('node:assert/strict')
const crypto = require('node:crypto')
const fs = require('node:fs')
const path = require('node:path')
const vm = require('node:vm')

const context = {
    Class: {
        create() {
            return function Constructor() {
                if (typeof this.initialize === 'function') this.initialize()
            }
        },
    },
    GlideDigest: function GlideDigest() {
        this.getSHA256Hex = value => crypto.createHash('sha256').update(String(value), 'utf8').digest('hex')
    },
}
vm.createContext(context)
vm.runInContext(
    fs.readFileSync(path.join(__dirname, '..', 'scripts', 'TopoControlPlane.js'), 'utf8'),
    context,
    { filename: 'TopoControlPlane.js' },
)

function record() {
    return {
        u_scope_id: 'site-a-scope',
        u_revision: 7,
        u_site_id: 'site-a',
        u_cidrs: '10.0.1.9/23\n10.0.0.0/24',
        u_exclusions: '10.0.1.128/25',
        u_ipv4_partition_prefix: 25,
        u_worker_pool: {
            getRefRecord() {
                return { isValidRecord: () => true, u_site_id: 'site-a' }
            },
        },
    }
}

const planner = new context.TopoControlPlane()
const firstRecord = record()
const first = planner.compileTargetScope(firstRecord)
const secondRecord = record()
const second = planner.compileTargetScope(secondRecord)

assert.deepEqual(Array.from(first.cidrs), ['10.0.0.0/25', '10.0.0.128/25', '10.0.1.0/25'])
assert.deepEqual(Array.from(first.keys), Array.from(second.keys))
assert.equal(firstRecord.u_cidrs, '10.0.0.0/23')
assert.equal(firstRecord.u_exclusions, '10.0.1.128/25')
assert.equal(firstRecord.u_partition_count, 3)
assert.equal(firstRecord.u_plan_digest, secondRecord.u_plan_digest)
assert.match(firstRecord.u_plan_digest, /^[a-f0-9]{64}$/)

const ipv6 = record()
ipv6.u_cidrs = '2001:db8::/64'
assert.throws(() => planner.compileTargetScope(ipv6), /IPv4 CIDRs only/)
assert.throws(() => planner._partitionIPv4Range({ start: 0, end: 255 }, 32, 2), /100000 deterministic partitions/)

const excludedAll = record()
excludedAll.u_cidrs = '192.0.2.0/24'
excludedAll.u_exclusions = '192.0.2.0/24'
assert.throws(() => planner.compileTargetScope(excludedAll), /remove every selected address/)

console.log('target-scope planner tests passed')
