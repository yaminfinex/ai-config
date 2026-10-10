import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

import { buildGroupNodes, buildSupervisionNodes, collapsedLabel, groupMembers, treeSummary, ungroupedID } from '../src/features/sidebar/sidebarNodes.ts'
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

test('groups view: one header per label alphabetical, Ungrouped last, membership is the agent\'s own label so a manager never joins its reports\' groups', () => {
  const nodes = buildGroupNodes(board())
  // probe is only a Task subagent's own label; the subagent follows its unlabelled owner, so no probe header.
  assert.deepEqual(nodes.get('tree-root')?.children, ['group:audit', 'group:fleet-refit', ungroupedID])
  // ziru carries no label: its labelled reports root under their own headers without it.
  assert.deepEqual(nodes.get('group:fleet-refit')?.children, ['group:fleet-refit/agent:impl-hine', 'group:fleet-refit/agent:impl-geni'])
  assert.deepEqual(nodes.get('group:audit')?.children, ['group:audit/agent:impl-pono', 'group:audit/agent:impl-bomi'])
  assert.equal(nodes.has('group:fleet-refit/agent:ziru'), false)
  assert.equal(nodes.has('group:audit/agent:ziru'), false)
  assert.equal(nodes.has('group:audit/agent:vara'), false)
  // Ungrouped: every row with no label of its own, as the manager tree restricted to them;
  // doza hangs under ziru, and ziru shows only that unlabelled report.
  assert.deepEqual(nodes.get(ungroupedID)?.children, ['group:/agent:ziru', 'group:/agent:vara', 'group:/agent:grill-tume', 'group:/agent:grill-lubo'])
  assert.deepEqual(nodes.get('group:/agent:ziru')?.children, ['group:/agent:design-doza'])
  assert.deepEqual(nodes.get('group:/agent:vara')?.children, [])
  // A Task subagent takes its owner's label, not its own.
  assert.deepEqual(nodes.get('group:/agent:grill-tume')?.children, ['group:/agent:tume_general_purpose_1'])
  assert.equal(nodes.get('group:/agent:tume_general_purpose_1')?.kind, 'subagent')
  assert.deepEqual(groupMembers(nodes, ungroupedID).sort(), ['design-doza', 'grill-lubo', 'grill-tume', 'tume_general_purpose_1', 'vara', 'ziru'])
  // Every agent exactly once across the whole map; no terminal.
  const leaves = [...nodes.values()].filter((node) => node.pane).map((node) => node.pane!.agent)
  const count = (name: string) => leaves.filter((leaf) => leaf === name).length
  for (const leaf of ['ziru', 'vara', 'grill-tume', 'impl-hine', 'impl-geni', 'impl-bomi', 'impl-pono', 'design-doza', 'grill-lubo', 'tume_general_purpose_1']) assert.equal(count(leaf), 1, leaf)
  assert.equal(count('-'), 0)
  assert.equal(leaves.includes('lima'), false)
  // A header reads its member count alone, folded or open: no active count.
  const header = nodes.get('group:fleet-refit')!
  assert.deepEqual(header.summary, { total: 2, active: 1 })
  assert.equal(header.count, 2)
  assert.equal(collapsedLabel(header), 'fleet-refit (2)')
  assert.equal(treeSummary(header, false), '(2)')
  assert.equal(collapsedLabel(nodes.get(ungroupedID)!), 'Ungrouped (6)')
  assert.equal(header.group, 'fleet-refit')
  assert.equal(nodes.get(ungroupedID)?.group, '')
  // Rows render like supervision rows: title over bus name, context and pane carried.
  assert.equal(nodes.get('group:/agent:grill-tume')?.name, 'grill')
  assert.equal(nodes.get('group:/agent:grill-tume')?.secondary, 'grill-tume')
  assert.deepEqual(groupMembers(nodes, 'group:audit').sort(), ['impl-bomi', 'impl-pono'])
})

test('the Ungrouped header is drawn only when non-empty and a fully grouped board has no header for it', () => {
  const grouped = board()
  grouped.unplaced = grouped.unplaced.filter((row) => row.agent !== 'grill-lubo' && row.agent !== 'design-doza')
  grouped.workspaces[0].tabs[0].panes = grouped.workspaces[0].tabs[0].panes
    .filter((pane) => pane.agent !== 'vara')
    .map((pane) => pane.agent === 'ziru' || pane.agent === 'grill-tume' ? { ...pane, group: 'ops' } : pane)
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
  assert.deepEqual(second.get('tree-root')?.children, ['group:audit', 'group:fleet-refit', 'group:zed', ungroupedID])
  assert.ok(first.has('group:fleet-refit') && second.has('group:fleet-refit') && second.has('group:fleet-refit/agent:impl-hine'))
  const placement = new Map(), supervision = new Map()
  const state = { expandedItems: ['group:audit'], knownWorkspaceItems: [], knownManagerItems: managerItems(first) }
  assert.equal(reconcileExpansion(placement, supervision, state, first), null)
  const transition = reconcileExpansion(placement, supervision, state, second)
  assert.deepEqual(transition?.expandedItems, ['group:audit', 'group:zed'])
  assert.ok(transition?.knownManagerItems?.includes('group:zed'))
  // A collapsed fleet-refit header stays collapsed: it was known, so nothing re-opens it.
  assert.equal(transition?.expandedItems?.includes('group:fleet-refit'), false)
})

test('membership is order-independent: an a↔b cycle carrying labels A and B puts each agent under its own header only, in either roster order', () => {
  const a = () => agent('a', { manager: 'b', manager_state: 'live', group: 'A', created_at: '2026-09-01T00:00:00Z' })
  const b = () => agent('b', { manager: 'a', manager_state: 'live', group: 'B', created_at: '2026-09-02T00:00:00Z' })
  for (const unplaced of [[a(), b()], [b(), a()]]) {
    const nodes = buildGroupNodes({ workspaces: [], unplaced })
    assert.deepEqual(nodes.get('tree-root')?.children, ['group:A', 'group:B'])
    assert.deepEqual(groupMembers(nodes, 'group:A'), ['a'])
    assert.deepEqual(groupMembers(nodes, 'group:B'), ['b'])
  }
  // Order independence without a cycle too: the labelled leaf may arrive before or after its unlabelled manager.
  const m = () => agent('m', { manager_state: 'unknown', created_at: '2026-09-01T00:00:00Z' })
  const leaf = () => agent('leaf', { manager: 'm', manager_state: 'live', group: 'G', created_at: '2026-09-02T00:00:00Z' })
  for (const unplaced of [[m(), leaf()], [leaf(), m()]]) {
    const nodes = buildGroupNodes({ workspaces: [], unplaced })
    assert.deepEqual(nodes.get('group:G')?.children, ['group:G/agent:leaf'])
    assert.deepEqual(nodes.get(ungroupedID)?.children, ['group:/agent:m'])
  }
})

test('header ids are label-safe: a label containing "/agent:", ":" or "/" never collides with another header\'s row ids', () => {
  const nodes = buildGroupNodes({ workspaces: [], unplaced: [
    agent('ziru', { manager_state: 'unknown', group: 'A', created_at: '2026-09-01T00:00:00Z' }),
    agent('evil', { manager_state: 'unknown', group: 'A/agent:ziru', created_at: '2026-09-02T00:00:00Z' }),
    agent('colon', { manager_state: 'unknown', group: 'x:y/z', created_at: '2026-09-03T00:00:00Z' }),
  ] })
  const headers = nodes.get('tree-root')!.children
  assert.deepEqual(headers, ['group:A', 'group:A%2Fagent%3Aziru', 'group:x%3Ay%2Fz'])
  assert.equal(nodes.get('group:A/agent:ziru')?.kind, 'agent', "group A's ziru row keeps its id")
  assert.equal(nodes.get('group:A%2Fagent%3Aziru')?.kind, 'group', 'the hostile label is a distinct header')
  assert.equal(nodes.get('group:A%2Fagent%3Aziru')?.group, 'A/agent:ziru', 'the raw label stays for display and assignment')
  assert.equal(nodes.get('group:A%2Fagent%3Aziru')?.name, 'A/agent:ziru')
  assert.deepEqual(groupMembers(nodes, 'group:A%2Fagent%3Aziru'), ['evil'])
  assert.deepEqual(groupMembers(nodes, 'group:A'), ['ziru'])
  assert.equal(new Set(nodes.keys()).size, nodes.size)
})

test('a reparent cycle among members is finite and every member is still homed', () => {
  const cyclic: Board = { workspaces: [], unplaced: [
    agent('a', { manager: 'b', manager_state: 'live', group: 'g', created_at: '2026-09-01T00:00:00Z' }),
    agent('b', { manager: 'a', manager_state: 'live', group: 'g', created_at: '2026-09-02T00:00:00Z' }),
  ] }
  const nodes = buildGroupNodes(cyclic)
  assert.deepEqual(groupMembers(nodes, 'group:g').sort(), ['a', 'b'])
})

test('Task subagents inherit their owning top-level agent group and appear once beneath the owner', () => {
  const groupedOwner = agent('owner', { manager_state: 'operator', group: 'build', subagents: [
    agent('owner_task_1', { parent_agent: 'owner', created_at: '2026-09-02T00:00:00Z' }),
  ] })
  const grouped = buildGroupNodes({ workspaces: [], unplaced: [groupedOwner] })
  assert.deepEqual(grouped.get('group:build')?.children, ['group:build/agent:owner'])
  assert.deepEqual(grouped.get('group:build/agent:owner')?.children, ['group:build/agent:owner_task_1'])
  assert.equal(grouped.has('group:/agent:owner_task_1'), false)
  assert.equal([...grouped.values()].filter((node) => node.pane?.agent === 'owner_task_1').length, 1)

  const ungroupedOwner = agent('solo', { manager_state: 'operator', subagents: [
    agent('solo_task_1', { parent_agent: 'solo', created_at: '2026-09-02T00:00:00Z' }),
  ] })
  const ungrouped = buildGroupNodes({ workspaces: [], unplaced: [ungroupedOwner] })
  assert.deepEqual(ungrouped.get(ungroupedID)?.children, ['group:/agent:solo'])
  assert.deepEqual(ungrouped.get('group:/agent:solo')?.children, ['group:/agent:solo_task_1'])
  assert.equal([...ungrouped.values()].filter((node) => node.pane?.agent === 'solo_task_1').length, 1)
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
  assert.equal((sidebar.match(/await assignAgent\(/g) ?? []).length, 1)
})

test('a manager with no label and one labelled report gives a header with exactly that report, and the manager under Ungrouped', () => {
  const nodes = buildGroupNodes({ workspaces: [], unplaced: [
    agent('chief', { manager: 'operator', manager_state: 'operator', created_at: '2026-09-01T00:00:00Z' }),
    agent('impl-kela', { manager: 'chief', manager_state: 'live', group: 'build', created_at: '2026-09-02T00:00:00Z' }),
  ] })
  assert.deepEqual(nodes.get('tree-root')?.children, ['group:build', ungroupedID])
  assert.deepEqual(nodes.get('group:build')?.children, ['group:build/agent:impl-kela'])
  assert.deepEqual(groupMembers(nodes, 'group:build'), ['impl-kela'])
  assert.equal(nodes.get('group:build')?.count, 1)
  assert.equal(nodes.has('group:build/agent:chief'), false)
  assert.deepEqual(nodes.get(ungroupedID)?.children, ['group:/agent:chief'])
  assert.deepEqual(nodes.get('group:/agent:chief')?.children, [], 'the grouped report does not hang under its manager in Ungrouped')
})

test('every group header reads "<label> (N)" in either state, while folded supervision rows keep their summaries', () => {
  const nodes = buildGroupNodes(board(), ['unit-x'])
  for (const id of ['group:audit', 'group:fleet-refit', 'group:unit-x', ungroupedID]) {
    const node = nodes.get(id)!
    const text = `${node.name} ${treeSummary(node, false)}`
    assert.equal(text, collapsedLabel(node), id)
    assert.match(text, /^\S.* \(\d+\)$/, id)
    assert.doesNotMatch(text, /active/, id)
  }
  assert.equal(collapsedLabel(nodes.get('group:unit-x')!), 'unit-x (0)', 'a placeholder header reads its zero count')
  // Open agent rows read nothing; supervision's folded rows are unchanged.
  assert.equal(treeSummary(nodes.get('group:/agent:ziru')!, false), '')
  const supervision = buildSupervisionNodes(board())
  assert.equal(collapsedLabel(supervision.get('agent:ziru')!), 'ziru (4)')
  const sidebar = readFileSync(new URL('../src/features/sidebar/FleetSidebar.tsx', import.meta.url), 'utf8')
  assert.match(sidebar, /const summary = treeSummary\(node, folded\)/)
  assert.match(sidebar, /\{summary && <span className="tree-summary"> \{summary\}<\/span>\}/)
  assert.match(sidebar, /folder && !folded && !groupHeader && <span className="count-badge">/, 'a header carries no second count')
})
