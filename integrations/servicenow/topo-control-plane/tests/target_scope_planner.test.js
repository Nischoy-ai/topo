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
assert.throws(() => planner._partitionIPv4Range({ start: 0, end: 255 }, 32, 2), /partition budget/)

const excludedAll = record()
excludedAll.u_cidrs = '192.0.2.0/24'
excludedAll.u_exclusions = '192.0.2.0/24'
assert.throws(() => planner.compileTargetScope(excludedAll), /remove every selected address/)

console.log('target-scope planner tests passed')

const sweep = record()
sweep.u_cidrs = '192.0.2.0/29\n192.0.2.0/30'
sweep.u_exclusions = '192.0.2.0/32\n192.0.2.4/31\n192.0.2.7/32'
sweep.u_ipv4_partition_prefix = 32
assert.deepEqual(Array.from(planner.compileTargetScope(sweep).cidrs),
    ['192.0.2.1/32', '192.0.2.2/32', '192.0.2.3/32', '192.0.2.6/32'])
const exactLimit = record()
exactLimit.u_cidrs = '192.0.0.0/22'; exactLimit.u_exclusions = ''; exactLimit.u_ipv4_partition_prefix = 32
assert.equal(planner.compileTargetScope(exactLimit).cidrs.length, 1024)
const tooLarge = record()
tooLarge.u_cidrs = '192.0.0.0/21'; tooLarge.u_exclusions = ''; tooLarge.u_ipv4_partition_prefix = 32
assert.throws(() => planner.compileTargetScope(tooLarge), /partition|bound|budget|limit/)
console.log('Bounded remote subnet expansion tests passed')

// Compare the range planner with an independent address-set oracle. Exercise
// overlapping, reordered and repeated CIDRs with fragmented exclusions.
let seed = 0x746f706f
function random(max) {
    seed = (Math.imul(seed, 1664525) + 1013904223) >>> 0
    return seed % max
}
for (let trial = 0; trial < 128; trial++) {
    const includes = [], excludes = [], selected = new Set(), denied = new Set()
    for (const [lines, addresses, count] of [[includes, selected, 8], [excludes, denied, 5]]) {
        for (let index = 0; index < count; index++) {
            const bits = 28 + random(5), size = 2 ** (32 - bits)
            const host = random(256), start = Math.floor(host / size) * size
            lines.push(`192.0.2.${host}/${bits}`)
            for (let address = start; address < start + size; address++) addresses.add(address)
        }
    }
    const expected = Array.from(selected).filter(address => !denied.has(address))
        .sort((a, b) => a - b).map(address => `192.0.2.${address}/32`)
    const scope = record()
    scope.u_cidrs = includes.join('\n')
    scope.u_exclusions = excludes.join('\n')
    scope.u_ipv4_partition_prefix = 32
    if (!expected.length) {
        assert.throws(() => planner.compileTargetScope(scope), /remove every selected address/)
        continue
    }
    const plan = planner.compileTargetScope(scope)
    assert.deepEqual(Array.from(plan.cidrs), expected, `address-set trial ${trial}`)
    const reordered = record()
    reordered.u_cidrs = includes.reverse().concat(includes[0]).join('\n')
    reordered.u_exclusions = excludes.reverse().join('\n')
    reordered.u_ipv4_partition_prefix = 32
    const repeated = planner.compileTargetScope(reordered)
    assert.deepEqual(Array.from(repeated.keys), Array.from(plan.keys))
    assert.equal(reordered.u_plan_digest, scope.u_plan_digest)
}

// The cap applies after exclusions, across all surviving segments. Failed
// planning must not write normalized scope metadata.
for (const [cidrs, exclusions, expectedCount] of [
    ['192.0.0.0/21', '192.0.4.0/22', 1024],
    ['192.0.0.0/22\n192.0.4.0/32', '', null],
    ['192.0.0.0/21', '192.0.4.0/23\n192.0.6.0/24\n192.0.7.0/25\n192.0.7.128/26\n192.0.7.192/27\n192.0.7.224/28\n192.0.7.240/29\n192.0.7.248/30\n192.0.7.252/31\n192.0.7.254/32', null],
]) {
    const scope = record()
    scope.u_cidrs = cidrs; scope.u_exclusions = exclusions; scope.u_ipv4_partition_prefix = 32
    if (expectedCount !== null) assert.equal(planner.compileTargetScope(scope).cidrs.length, expectedCount)
    else {
        assert.throws(() => planner.compileTargetScope(scope), /budget|limit|bound/)
        assert.equal(scope.u_cidrs, cidrs)
        assert.equal(scope.u_partition_count, undefined)
        assert.equal(scope.u_plan_digest, undefined)
    }
}
for (const cidr of ['0.0.0.0/32', '255.255.255.255/32']) {
    const scope = record()
    scope.u_cidrs = cidr; scope.u_exclusions = ''; scope.u_ipv4_partition_prefix = 32
    assert.deepEqual(Array.from(planner.compileTargetScope(scope).cidrs), [cidr])
}
console.log('Subnet address-set, segmented bounds and IPv4 boundary checks passed')
