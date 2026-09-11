import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'

import { groupHeaderTooltip, membersToOpen, openGroupTooltip, planGroupDrop, planOpenGroupAsSpace, planSidebarDrop } from '../src/features/sidebar/groupDropModel.ts'
import { assignAgent } from '../src/api/client.ts'
import { createAndSwitchSpace } from '../src/features/spaces/spacesControllerModel.ts'
import type { SidebarNode } from '../src/features/sidebar/sidebarNodes.ts'

const header: SidebarNode = { id: 'group:fleet-refit', kind: 'group', name: 'fleet-refit', children: [], group: 'fleet-refit' }
const ungrouped: SidebarNode = { id: 'group:', kind: 'ungrouped', name: 'Ungrouped', children: [], group: '' }
const row: SidebarNode = { id: 'group:audit/agent:ziru', kind: 'agent', name: 'ziru', children: [], pane: { pane_id: '-', agent: 'ziru', tool: 'claude', herdr_status: '-', bus_status: 'listening', gap: '-' } }

test('drop plan: agent onto a header sets the group, onto Ungrouped clears, everything else is refused', () => {
  assert.deepEqual(planGroupDrop('groups', 'impl-hine', header), { name: 'impl-hine', group: 'fleet-refit' })
  assert.deepEqual(planGroupDrop('groups', 'impl-hine', ungrouped), { name: 'impl-hine', group: '' })
  assert.equal(planGroupDrop('supervision', 'impl-hine', header), null)
  assert.equal(planGroupDrop('placement', 'impl-hine', header), null)
  assert.equal(planGroupDrop('groups', 'impl-hine', row), null)
  assert.equal(planGroupDrop('groups', 'impl-hine', undefined), null)
  assert.equal(planGroupDrop('groups', null, header), null)
  assert.equal(planGroupDrop('groups', '', header), null)
  assert.equal(planGroupDrop('groups', '-', header), null)
})

test('one drop issues exactly one POST to the assignment endpoint with only the group', async () => {
  const calls: { url: string, method?: string, body?: string }[] = []
  const fetcher = (async (input: RequestInfo | URL, init?: RequestInit) => {
    calls.push({ url: String(input), method: init?.method, body: String(init?.body) })
    return new Response(JSON.stringify({ name: 'impl-hine', group: 'fleet-refit', by: 'web:yamen' }), { status: 200, headers: { 'Content-Type': 'application/json' } })
  }) as typeof fetch
  const plan = planGroupDrop('groups', 'impl/hine', header)!
  await assignAgent(plan.name, { group: plan.group }, fetcher)
  assert.deepEqual(calls, [{ url: '/api/agents/impl%2Fhine/assignment', method: 'POST', body: '{"group":"fleet-refit"}' }])
  const clear = planGroupDrop('groups', 'impl-hine', ungrouped)!
  await assignAgent(clear.name, { group: clear.group }, fetcher)
  assert.equal(calls.length, 2)
  assert.equal(calls[1].body, '{"group":""}')
})

test('open as space: an exact-name space is switched to, otherwise one is created; members not yet open are opened; the count is in the tooltip', () => {
  const spaces = [{ id: 's1', name: 'fleet-refit' }, { id: 's2', name: 'Fleet-Refit' }]
  assert.deepEqual(planOpenGroupAsSpace('fleet-refit', spaces), { action: 'switch', id: 's1' })
  assert.deepEqual(planOpenGroupAsSpace('audit', spaces), { action: 'create', name: 'audit' })
  assert.deepEqual(membersToOpen(['ziru', 'impl-hine', 'impl-geni'], ['impl-hine']), ['ziru', 'impl-geni'])
  assert.equal(openGroupTooltip('fleet-refit', 3), 'Open group fleet-refit as a space (open 3 transcripts)')
  assert.equal(openGroupTooltip('solo', 1), 'Open group solo as a space (open 1 transcript)')
  assert.equal(groupHeaderTooltip(header, 3), 'group fleet-refit · 3 agents · drop an agent here to set its group')
  assert.equal(groupHeaderTooltip(ungrouped, 1), 'Ungrouped · 1 agent · drop an agent here to clear its group')
})

test('creating the group space writes only the space itself: create, rename, switch, flush and nothing else', () => {
  const log: string[] = []
  const created = { id: 'new', name: 'space 1', created: 1, updated: 1 }
  const result = createAndSwitchSpace('fleet-refit', {
    create: () => { log.push('create'); return { ok: true, value: created } },
    rename: (id, name) => { log.push(`rename ${id} ${name}`); return { ok: true, value: { ...created, name } } },
    switchTo: (id) => { log.push(`switch ${id}`); return true },
    rollbackCreate: (id) => { log.push(`rollback ${id}`); return true },
    flush: () => { log.push('flush'); return true },
  })
  assert.equal(result.ok, true)
  assert.deepEqual(log, ['create', 'rename new fleet-refit', 'switch new', 'flush'])
})

test('the controller opens members pinned and stores no group/space link', () => {
  const controller = readFileSync(new URL('../src/features/workspace/useWorkspaceController.ts', import.meta.url), 'utf8')
  const body = controller.slice(controller.indexOf('const openGroupAsSpace'), controller.indexOf('const renameSpace'))
  assert.match(body, /planOpenGroupAsSpace\(group, store\.list\(\)\)/)
  assert.match(body, /members\.forEach\(\(member\) => openAgent\(member, false\)\)/)
  assert.doesNotMatch(body, /localStorage|upsertState|closePanel|store\.write|store\.upsert/)
})

test('planSidebarDrop is the one seam: groups → header plan by node id, supervision → reparent plan, placement → refused', () => {
  const live = (agent: string, extra: object = {}) => ({ pane_id: '-', agent, tool: 'claude', herdr_status: '-', bus_status: 'listening', gap: '-', manager_state: 'live', ...extra })
  const nodes = new Map<string, SidebarNode>([
    ['group:audit', { ...header, id: 'group:audit', name: 'audit', group: 'audit', children: ['group:audit/agent:ziru'] }],
    ['group:audit/agent:ziru', { id: 'group:audit/agent:ziru', kind: 'agent', name: 'ziru', children: [], pane: live('ziru') }],
    ['group:', { ...ungrouped, children: ['group:/agent:impl-hine'] }],
    ['group:/agent:impl-hine', { id: 'group:/agent:impl-hine', kind: 'agent', name: 'impl-hine', children: [], pane: live('impl-hine') }],
    ['agent:ziru', { id: 'agent:ziru', kind: 'agent', name: 'ziru', children: [], pane: live('ziru') }],
    ['agent:impl-hine', { id: 'agent:impl-hine', kind: 'agent', name: 'impl-hine', children: [], pane: live('impl-hine') }],
  ])
  assert.deepEqual(planSidebarDrop('groups', 'group:/agent:impl-hine', 'group:audit', nodes), { name: 'impl-hine', assignment: { group: 'audit' } })
  assert.deepEqual(planSidebarDrop('groups', 'group:audit/agent:ziru', 'group:', nodes), { name: 'ziru', assignment: { group: '' } })
  assert.equal(planSidebarDrop('groups', 'group:/agent:impl-hine', 'group:audit/agent:ziru', nodes), null, 'a row is never a target in the groups view')
  assert.equal(planSidebarDrop('groups', 'group:/agent:impl-hine', null, nodes), null, 'the empty top level clears nothing in the groups view')
  assert.deepEqual(planSidebarDrop('supervision', 'agent:impl-hine', 'agent:ziru', nodes), { name: 'impl-hine', assignment: { manager: 'ziru' } })
  assert.deepEqual(planSidebarDrop('supervision', 'agent:impl-hine', null, nodes), { name: 'impl-hine', assignment: { manager: 'human' } })
  assert.equal(planSidebarDrop('supervision', 'agent:impl-hine', 'group:audit', nodes), null)
  assert.equal(planSidebarDrop('placement', 'agent:impl-hine', 'agent:ziru', nodes), null)
  // The sidebar has exactly one drag seam: one draggable, one onDragStart, one row onDrop, one container onDrop, one submit.
  const sidebar = readFileSync(new URL('../src/features/sidebar/FleetSidebar.tsx', import.meta.url), 'utf8')
  assert.equal((sidebar.match(/draggable:/g) ?? []).length, 1)
  assert.equal((sidebar.match(/onDragStart:/g) ?? []).length, 1)
  assert.equal((sidebar.match(/onDrop[:=]/g) ?? []).length, 2)
  assert.equal((sidebar.match(/planSidebarDrop\(/g) ?? []).length, 4)
  assert.doesNotMatch(sidebar, /reparentDrop|planGroupDrop/)
  assert.equal((sidebar.match(/role="alert"/g) ?? []).length, 1)
})
