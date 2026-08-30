import '@servicenow/sdk/global'
import { ApplicationMenu, Record } from '@servicenow/sdk/core'
import { topoViewer } from './security.now'

export const topoMenu = ApplicationMenu({
    $id: Now.ID['application-menu-topo'],
    title: 'Nischoy Topo',
    name: 'Nischoy Topo',
    description: 'Control panel for stateless Nischoy Topo discovery workers.',
    hint: 'Manage worker pools, fixed local.v1 profiles, schedules, and bounded run evidence.',
    roles: [topoViewer],
    order: 100,
    active: true,
})

Record({
    $id: Now.ID['module-worker-pools'],
    table: 'sys_app_module',
    data: {
        title: 'Worker Pools',
        application: Now.ref('sys_app_application', { title: 'Nischoy Topo' }),
        name: 'x_664635_topo_worker_pool',
        link_type: 'LIST',
        roles: [topoViewer],
        order: 100,
        active: true,
    },
})

Record({
    $id: Now.ID['module-profiles'],
    table: 'sys_app_module',
    data: {
        title: 'Discovery Profiles',
        application: Now.ref('sys_app_application', { title: 'Nischoy Topo' }),
        name: 'x_664635_topo_profile',
        link_type: 'LIST',
        roles: [topoViewer],
        order: 200,
        active: true,
    },
})

Record({
    $id: Now.ID['module-schedules'],
    table: 'sys_app_module',
    data: {
        title: 'Schedules',
        application: Now.ref('sys_app_application', { title: 'Nischoy Topo' }),
        name: 'x_664635_topo_schedule',
        link_type: 'LIST',
        roles: [topoViewer],
        order: 300,
        active: true,
    },
})

Record({
    $id: Now.ID['module-runs'],
    table: 'sys_app_module',
    data: {
        title: 'Runs',
        application: Now.ref('sys_app_application', { title: 'Nischoy Topo' }),
        name: 'x_664635_topo_run',
        link_type: 'LIST',
        roles: [topoViewer],
        order: 400,
        active: true,
    },
})

Record({
    $id: Now.ID['module-workers'],
    table: 'sys_app_module',
    data: {
        title: 'Workers',
        application: Now.ref('sys_app_application', { title: 'Nischoy Topo' }),
        name: 'x_664635_topo_worker',
        link_type: 'LIST',
        roles: [topoViewer],
        order: 500,
        active: true,
    },
})

Record({
    $id: Now.ID['module-tasks'],
    table: 'sys_app_module',
    data: {
        title: 'Tasks',
        application: Now.ref('sys_app_application', { title: 'Nischoy Topo' }),
        name: 'x_664635_topo_task',
        link_type: 'LIST',
        roles: [topoViewer],
        order: 600,
        active: true,
    },
})

Record({
    $id: Now.ID['module-result-chunks'],
    table: 'sys_app_module',
    data: {
        title: 'Result Chunks',
        application: Now.ref('sys_app_application', { title: 'Nischoy Topo' }),
        name: 'x_664635_topo_result',
        link_type: 'LIST',
        roles: [topoViewer],
        order: 700,
        active: true,
    },
})

Record({
    $id: Now.ID['module-ire-deliveries'],
    table: 'sys_app_module',
    data: {
        title: 'IRE Deliveries',
        application: Now.ref('sys_app_application', { title: 'Nischoy Topo' }),
        name: 'x_664635_topo_ire_delivery',
        link_type: 'LIST',
        roles: [topoViewer],
        order: 800,
        active: true,
    },
})
