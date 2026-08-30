import '@servicenow/sdk/global'
import { Acl, CrossScopePrivilege, Role } from '@servicenow/sdk/core'

export const topoViewer = Role({
    name: 'x_664635_topo.viewer',
    description: 'Read-only access to Topo control-plane configuration and bounded run state.',
    grantable: true,
})

export const topoWorker = Role({
    name: 'x_664635_topo.worker',
    description: 'Execute only the six outbound-worker Scripted REST resources; no generic table or CMDB access.',
    grantable: true,
})

export const topoOperator = Role({
    name: 'x_664635_topo.operator',
    description: 'Manage Topo profiles and schedules and start reviewed discovery runs.',
    grantable: true,
    containsRoles: [topoViewer],
})

export const topoAdmin = Role({
    name: 'x_664635_topo.admin',
    description: 'Administer the Topo scoped control plane and worker pools.',
    grantable: true,
    scopedAdmin: true,
    containsRoles: [topoOperator],
})

export const topoWorkerEndpoint = Acl({
    $id: Now.ID['acl-rest-topo-worker-v1'],
    name: 'topo_worker_v1',
    type: 'rest_endpoint',
    operation: 'execute',
    roles: [topoWorker],
    securityAttribute: 'user_is_authenticated',
    adminOverrides: true,
    active: true,
    description: 'Allows authenticated Topo workers to execute only the Topo Worker API routes.',
})

Acl({
    $id: Now.ID['acl-worker-pool-read'],
    type: 'record',
    operation: 'read',
    table: 'x_664635_topo_worker_pool',
    roles: [topoViewer],
    active: true,
})
Acl({
    $id: Now.ID['acl-worker-pool-create'],
    type: 'record',
    operation: 'create',
    table: 'x_664635_topo_worker_pool',
    roles: [topoAdmin],
    active: true,
})
Acl({
    $id: Now.ID['acl-worker-pool-write'],
    type: 'record',
    operation: 'write',
    table: 'x_664635_topo_worker_pool',
    roles: [topoAdmin],
    active: true,
})
Acl({
    $id: Now.ID['acl-worker-pool-delete'],
    type: 'record',
    operation: 'delete',
    table: 'x_664635_topo_worker_pool',
    roles: [topoAdmin],
    active: true,
})

Acl({
    $id: Now.ID['acl-worker-read'],
    type: 'record',
    operation: 'read',
    table: 'x_664635_topo_worker',
    roles: [topoViewer],
    active: true,
})
Acl({
    $id: Now.ID['acl-worker-delete'],
    type: 'record',
    operation: 'delete',
    table: 'x_664635_topo_worker',
    roles: [topoAdmin],
    active: true,
})

Acl({
    $id: Now.ID['acl-profile-read'],
    type: 'record',
    operation: 'read',
    table: 'x_664635_topo_profile',
    roles: [topoViewer],
    active: true,
})
Acl({
    $id: Now.ID['acl-profile-create'],
    type: 'record',
    operation: 'create',
    table: 'x_664635_topo_profile',
    roles: [topoOperator],
    active: true,
})
Acl({
    $id: Now.ID['acl-profile-write'],
    type: 'record',
    operation: 'write',
    table: 'x_664635_topo_profile',
    roles: [topoOperator],
    active: true,
})
Acl({
    $id: Now.ID['acl-profile-delete'],
    type: 'record',
    operation: 'delete',
    table: 'x_664635_topo_profile',
    roles: [topoAdmin],
    active: true,
})

Acl({
    $id: Now.ID['acl-schedule-read'],
    type: 'record',
    operation: 'read',
    table: 'x_664635_topo_schedule',
    roles: [topoViewer],
    active: true,
})
Acl({
    $id: Now.ID['acl-schedule-create'],
    type: 'record',
    operation: 'create',
    table: 'x_664635_topo_schedule',
    roles: [topoOperator],
    active: true,
})
Acl({
    $id: Now.ID['acl-schedule-write'],
    type: 'record',
    operation: 'write',
    table: 'x_664635_topo_schedule',
    roles: [topoOperator],
    active: true,
})
Acl({
    $id: Now.ID['acl-schedule-delete'],
    type: 'record',
    operation: 'delete',
    table: 'x_664635_topo_schedule',
    roles: [topoOperator],
    active: true,
})

Acl({
    $id: Now.ID['acl-run-read'],
    type: 'record',
    operation: 'read',
    table: 'x_664635_topo_run',
    roles: [topoViewer],
    active: true,
})
Acl({
    $id: Now.ID['acl-run-delete'],
    type: 'record',
    operation: 'delete',
    table: 'x_664635_topo_run',
    roles: [topoAdmin],
    active: true,
})

Acl({
    $id: Now.ID['acl-task-read'],
    type: 'record',
    operation: 'read',
    table: 'x_664635_topo_task',
    roles: [topoViewer],
    active: true,
})
Acl({
    $id: Now.ID['acl-task-delete'],
    type: 'record',
    operation: 'delete',
    table: 'x_664635_topo_task',
    roles: [topoAdmin],
    active: true,
})

Acl({
    $id: Now.ID['acl-result-read'],
    type: 'record',
    operation: 'read',
    table: 'x_664635_topo_result',
    roles: [topoViewer],
    active: true,
})
Acl({
    $id: Now.ID['acl-result-delete'],
    type: 'record',
    operation: 'delete',
    table: 'x_664635_topo_result',
    roles: [topoAdmin],
    active: true,
})

Acl({
    $id: Now.ID['acl-ire-delivery-read'],
    type: 'record',
    operation: 'read',
    table: 'x_664635_topo_ire_delivery',
    roles: [topoViewer],
    active: true,
})
Acl({
    $id: Now.ID['acl-ire-delivery-delete'],
    type: 'record',
    operation: 'delete',
    table: 'x_664635_topo_ire_delivery',
    roles: [topoAdmin],
    active: true,
})

CrossScopePrivilege({
    $id: Now.ID['privilege-identification-engine'],
    targetScope: 'sn_cmdb',
    targetName: 'sn_cmdb.IdentificationEngine',
    targetType: 'sys_script_include',
    operation: 'execute',
    status: 'allowed',
})
