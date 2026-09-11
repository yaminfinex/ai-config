import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

import { buildGroupNodes, buildSupervisionNodes, collapsedLabel, groupMembers, ungroupedID } from '../src/features/sidebar/sidebarNodes.ts'
import { managerItems, reconcileExpansion } from '../src/features/sidebar/sidebarView.ts'
import type { Board, Row } from '../src/types.ts'

function agent(name: string, extra: Partial<Row> & { pane_id?: string }): Row {
  return { pane_id: extra.pane_id ?? '-', agent: name, tool: 'claude', herdr_status: extra.pane_id ? 'unknown' : '-', bus_status: 'listening', gap: extra.pane_id ? '-' : 'no visible pane', ...extra }
}

// ziru manages hine (fleet-refit), geni (fleet-refit), bomi (audit) and
// doza (no label); ziru itself carries no label. vara is a second
// orchestrator with one report pono (audit). tume has a Task subagent
// labelled probe; tume itself is unlabelled. lubo is unlabelled and
// unmanaged. One terminal pane must not appear anywhere.
function board(): Board {
  return {
    workspaces: [{
      workspace_id: 'w1', number: 1, label: 'fleet', focused: false, pane_count: 6, tab_count: 1, active_tab_id: 't1', agent_status: 'active',
      tabs: [{ tab_id: 't1', number: 1, label: 'seats', focused: false, pane_count: 6, agent_status: 'active', panes: [
        agent('ziru', { pane_id: 'w1:p1', manager: 'operator', manager_state: 'operator', created_at: '2026-09-01T00:00:00Z' }),
        agent('impl-hine', { pane_id: 'w1:p2', manager: 'ziru', manager_state: 'live', group: 'fleet-refit', bus_status: 'active', created_at: '2026-09-02T00:00:00Z' }),
        agent('impl-bomi', { pane_id: 'w1:p3', manager: 'ziru', manager_state: 'live', group: 'audit', created_at: '2026-09-03T00:00:00Z' }),
        agent('vara', { pane_id: 'w1:p4', manager: 'operator', manager_state: 'operator', created_at: '2026-09-01T01:00:00Z' }),
        agent('grill-tume', { pane_id: 'w1:p5', manager: 'operator', manager_state: 'operator', created_at: '2026-09-04T00:00:00Z', title: 'grill',
          subagents: [agent('tume_general_purpose_1', { parent_agent: 'grill-tume', group: 'probe', bus_status: 'active', created_at: '2026-09-05T00:00:00Z' })] }),
        { pane_id: 'w1:p6', label: 'lima', agent: '-', tool: '-', herdr_status: '-', bus_status: '-', gap: '-' },
      ] }],
    }],
    unplaced: [
      agent('impl-geni', { manager: 'ziru', manager_state: 'live', group: 'fleet-refit', created_at: '2026-09-02T01:00:00Z' }),
      agent('design-doza', { manager: 'ziru', manager_state: 'live', created_at: '2026-09-02T02:00:00Z' }),
      agent('impl-pono', { manager: 'vara', manager_state: 'live', group: 'audit', created_at: '2026-09-02T03:00:00Z' }),
      agent('grill-lubo', { manager_state: 'unknown', created_at: '2026-09-06T00:00:00Z' }),
    ],
  }
}

test('groups view: one header per label alphabetical, Ungrouped last, membership derived upward so a manager appears under every group it has reports in', () => {
  const nodes = buildGroupNodes(board())
  assert.deepEqual(nodes.get('tree-root')?.children, ['group:audit', 'group:fleet-refit', 'group:probe', ungroupedID])
  // ziru has reports in audit and fleet-refit: it heads both, holding only the matching reports.
  assert.deepEqual(nodes.get('group:fleet-refit')?.children, ['group:fleet-refit/agent:ziru'])
  assert.deepEqual(nodes.get('group:fleet-refit/agent:ziru')?.children, ['group:fleet-refit/agent:impl-hine', 'group:fleet-refit/agent:impl-geni'])
  assert.deepEqual(nodes.get('group:audit')?.children, ['group:audit/agent:ziru', 'group:audit/agent:vara'])
  assert.deepEqual(nodes.get('group:audit/agent:ziru')?.children, ['group:audit/agent:impl-bomi'])
  assert.deepEqual(nodes.get('group:audit/agent:vara')?.children, ['group:audit/agent:impl-pono'])
  // A Task subagent's label pulls its parent in; the parent shows only that child there.
  assert.deepEqual(nodes.get('group:probe')?.children, ['group:probe/agent:grill-tume'])
  assert.deepEqual(nodes.get('group:probe/agent:grill-tume')?.children, ['group:probe/agent:tume_general_purpose_1'])
  assert.equal(nodes.get('group:probe/agent:tume_general_purpose_1')?.kind, 'subagent')
  // Ungrouped: rows with no label anywhere below them; ziru (labels below) is absent, its unlabelled report doza roots here.
  assert.deepEqual(nodes.get(ungroupedID)?.children, ['group:/agent:design-doza', 'group:/agent:grill-lubo'])
  assert.deepEqual(groupMembers(nodes, ungroupedID).sort(), ['design-doza', 'grill-lubo'])
  // Each leaf exactly once across the whole map; ziru exactly twice; no terminal.
  const leaves = [...nodes.values()].filter((node) => node.pane).map((node) => node.pane!.agent)
  const count = (name: string) => leaves.filter((leaf) => leaf === name).length
  for (const leaf of ['impl-hine', 'impl-geni', 'impl-bomi', 'impl-pono', 'design-doza', 'grill-lubo', 'tume_general_purpose_1']) assert.equal(count(leaf), 1, leaf)
  assert.equal(count('ziru'), 2)
  assert.equal(count('-'), 0)
  assert.equal(leaves.includes('lima'), false)
  // Header summary reads like a folded manager: (total · active) and the badge count.
  const header = nodes.get('group:fleet-refit')!
  assert.deepEqual(header.summary, { total: 3, active: 1 })
  assert.equal(header.count, 3)
  assert.equal(collapsedLabel(header), 'fleet-refit (3 · 1 active)')
  assert.equal(header.group, 'fleet-refit')
  assert.equal(nodes.get(ungroupedID)?.group, '')
  // Rows render like supervision rows: title over bus name, context and pane carried.
  assert.equal(nodes.get('group:probe/agent:grill-tume')?.name, 'grill')
  assert.equal(nodes.get('group:probe/agent:grill-tume')?.secondary, 'grill-tume')
  assert.deepEqual(groupMembers(nodes, 'group:audit').sort(), ['impl-bomi', 'impl-pono', 'vara', 'ziru'])
})

test('the Ungrouped header is drawn only when non-empty and a fully grouped board has no header for it', () => {
  const grouped = board()
  grouped.unplaced = grouped.unplaced.filter((row) => row.agent !== 'grill-lubo' && row.agent !== 'design-doza')
  grouped.workspaces[0].tabs[0].panes = grouped.workspaces[0].tabs[0].panes.filter((pane) => pane.agent !== 'vara')
  const nodes = buildGroupNodes(grouped)
  assert.equal(nodes.has(ungroupedID), false)
  assert.equal(nodes.get('tree-root')?.children.includes(ungroupedID), false)
  assert.equal(buildGroupNodes(undefined).get('tree-root')?.children.length, 0)
  assert.equal(buildGroupNodes({ workspaces: [], unplaced: [] }).size, 1)
})

test('the supervision view draws no group nodes and ignores the label', () => {
  const nodes = buildSupervisionNodes(board())
  assert.equal([...nodes.values()].some((node) => node.kind === 'group' || node.kind === 'ungrouped' || node.id.startsWith('group:')), false)
  assert.equal(nodes.has('agent:ziru'), true)
})

test('header ids are stable across frames so expansion persists, and a new header opens once like a new manager', () => {
  const first = buildGroupNodes(board())
  const next = board()
  next.unplaced.push(agent('impl-new', { manager: 'ziru', manager_state: 'live', group: 'fleet-refit', created_at: '2026-09-07T00:00:00Z' }))
  next.unplaced.push(agent('impl-solo', { manager_state: 'unknown', group: 'zed', created_at: '2026-09-07T00:00:00Z' }))
  const second = buildGroupNodes(next)
  assert.deepEqual(second.get('tree-root')?.children, ['group:audit', 'group:fleet-refit', 'group:probe', 'group:zed', ungroupedID])
  assert.ok(first.has('group:fleet-refit') && second.has('group:fleet-refit') && second.has('group:fleet-refit/agent:ziru'))
  const placement = new Map(), supervision = new Map()
  const state = { expandedItems: ['group:audit'], knownWorkspaceItems: [], knownManagerItems: managerItems(first) }
  assert.equal(reconcileExpansion(placement, supervision, state, first), null)
  const transition = reconcileExpansion(placement, supervision, state, second)
  assert.deepEqual(transition?.expandedItems, ['group:audit', 'group:zed'])
  assert.ok(transition?.knownManagerItems?.includes('group:zed'))
  // A collapsed fleet-refit header stays collapsed: it was known, so nothing re-opens it.
  assert.equal(transition?.expandedItems?.includes('group:fleet-refit'), false)
})

test('a reparent cycle among members is finite and every member is still homed', () => {
  const cyclic: Board = { workspaces: [], unplaced: [
    agent('a', { manager: 'b', manager_state: 'live', group: 'g', created_at: '2026-09-01T00:00:00Z' }),
    agent('b', { manager: 'a', manager_state: 'live', group: 'g', created_at: '2026-09-02T00:00:00Z' }),
  ] }
  const nodes = buildGroupNodes(cyclic)
  assert.deepEqual(groupMembers(nodes, 'group:g').sort(), ['a', 'b'])
})

test('the toggle offers the groups view and the sidebar renders the header action and drop class', () => {
  const toggle = readFileSync(new URL('../src/features/sidebar/FleetViewToggle.tsx', import.meta.url), 'utf8')
  assert.match(toggle, /view: 'groups', label: 'groups', title: 'Groups: who works on what'/)
  const sidebar = readFileSync(new URL('../src/features/sidebar/FleetSidebar.tsx', import.meta.url), 'utf8')
  assert.match(sidebar, /className="group-space-button"/)
  assert.match(sidebar, /dropTarget === node\.id \? ' drop-target' : ''/)
  assert.match(sidebar, /` · group \$\{pane\.group\}`/)
  // One drag source and one drop handler per row; the drop plan is the model's.
  assert.equal((sidebar.match(/onDragStart:/g) ?? []).length, 1)
  assert.equal((sidebar.match(/onDrop:/g) ?? []).length, 1)
  assert.equal((sidebar.match(/assignAgent\(/g) ?? []).length, 1)
})
