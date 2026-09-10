import assert from 'node:assert/strict'
import test from 'node:test'

import { buildSupervisionNodes, collapsedLabel } from '../src/features/sidebar/sidebarNodes.ts'
import type { Board, Pane, Row } from '../src/types.ts'

function agent(name: string, extra: Partial<Row> & { pane_id?: string }): Row {
  return { pane_id: extra.pane_id ?? '-', agent: name, tool: 'claude', herdr_status: extra.pane_id ? 'unknown' : '-', bus_status: 'listening', gap: extra.pane_id ? '-' : 'no visible pane', ...extra }
}

// Shaped like today's live board: operator roots ziru/vara/riko, riko's
// seats, kele and mesa under an ENDED hamo, lubo unadopted, a Task subagent
// under tume, and one terminal pane.
function liveShapedBoard(): Board {
  const panes: Pane[] = [
    agent('ziru', { pane_id: 'w1A:p2D', manager: 'operator', manager_state: 'operator', created_at: '2026-09-01T20:49:36Z' }),
    agent('sesh-kele', { pane_id: 'w6A:p1', manager: 'orch-hamo', manager_state: 'ended', created_at: '2026-08-31T04:04:09Z' }),
    agent('vara', { pane_id: 'w6M:p3', manager: 'operator', manager_state: 'operator', created_at: '2026-09-01T20:47:44Z' }),
    agent('sesh-mesa', { pane_id: 'w6N:p1', manager: 'orch-hamo', manager_state: 'ended', created_at: '2026-09-01T01:16:11Z' }),
    agent('corestack-review-beri', { pane_id: 'w7Q:p1', manager: 'riko', manager_state: 'live', bus_status: 'active', created_at: '2026-09-02T06:29:34Z' }),
    agent('grill-confirm-lubo', { pane_id: 'w8Y:p7', manager_state: 'unknown', created_at: '2026-09-10T01:09:07Z' }),
    agent('grill-confirm-tume', {
      pane_id: 'w97:p1', manager: 'riko', manager_state: 'live', created_at: '2026-09-09T06:21:06Z', title: 'grill confirm',
      subagents: [agent('tume_general_purpose_1', { parent_agent: 'grill-confirm-tume', bus_status: 'active', manager: 'grill-confirm-tume', manager_state: 'live', created_at: '2026-09-10T09:00:00Z' })],
    }),
    { pane_id: 'w94:p1', label: 'lima', agent: '-', tool: '-', herdr_status: '-', bus_status: '-', gap: '-' },
    agent('riko', { pane_id: 'wY:p6R', manager: 'operator', manager_state: 'operator', created_at: '2026-09-01T20:46:07Z' }),
  ]
  return {
    workspaces: [{
      workspace_id: 'w1', number: 1, label: 'fleet', focused: false, pane_count: panes.length, tab_count: 1, active_tab_id: 't1', agent_status: 'active',
      tabs: [{ tab_id: 't1', number: 1, label: 'seats', focused: false, pane_count: panes.length, agent_status: 'active', panes }],
    }],
    unplaced: [
      agent('durlog-grill-nego', { manager: 'riko', manager_state: 'live', created_at: '2026-08-31T04:16:39Z' }),
      agent('impl-pono', { manager: 'fimu', manager_state: 'unknown', bus_status: 'active', created_at: '2026-09-10T10:01:54Z' }),
    ],
  }
}

test('supervision tree groups by manager: operator roots, creation order, tombstone, unadopted, terminals', () => {
  const nodes = buildSupervisionNodes(liveShapedBoard())
  assert.deepEqual(nodes.get('tree-root')?.children, ['operator', 'unadopted', 'terminals'])
  // Roots in roster creation order (riko, vara, ziru), not pane order or status; the ended manager's tombstone last.
  assert.deepEqual(nodes.get('operator')?.children, ['agent:riko', 'agent:vara', 'agent:ziru', 'tombstone:orch-hamo'])
  assert.equal(nodes.get('operator')?.kind, 'operator')
  // riko's reports by creation order: nego (unplaced) before beri before tume; beri is active but does not jump ahead.
  assert.deepEqual(nodes.get('agent:riko')?.children, ['agent:durlog-grill-nego', 'agent:corestack-review-beri', 'agent:grill-confirm-tume'])
  // A Task subagent nests under its parent_agent, not its manager group.
  assert.deepEqual(nodes.get('agent:grill-confirm-tume')?.children, ['agent:tume_general_purpose_1'])
  assert.equal(nodes.get('agent:tume_general_purpose_1')?.kind, 'subagent')
  // Tombstone: built from the reports' manager string; greyed "ended" with kele/mesa under it in creation order.
  const tombstone = nodes.get('tombstone:orch-hamo')
  assert.equal(tombstone?.kind, 'tombstone')
  assert.equal(tombstone?.name, 'orch-hamo')
  assert.equal(tombstone?.secondary, 'ended')
  assert.deepEqual(tombstone?.children, ['agent:sesh-kele', 'agent:sesh-mesa'])
  // Unadopted: lubo (no manager) then pono under its unresolvable manager group.
  assert.deepEqual(nodes.get('unadopted')?.children, ['agent:grill-confirm-lubo', 'unknown:fimu'])
  assert.equal(nodes.get('unknown:fimu')?.kind, 'unknown-manager')
  assert.deepEqual(nodes.get('unknown:fimu')?.children, ['agent:impl-pono'])
  // Terminals grouped by workspace; the pane keeps its placement id and kind.
  assert.deepEqual(nodes.get('terminals')?.children, ['terminals:w1'])
  assert.deepEqual(nodes.get('terminals:w1')?.children, ['pane:w94:p1'])
  assert.equal(nodes.get('pane:w94:p1')?.kind, 'pane')
  assert.equal(nodes.get('terminals')?.count, 1)
})

test('supervision rows carry title over bus name and the placement chip', () => {
  const nodes = buildSupervisionNodes(liveShapedBoard())
  const tume = nodes.get('agent:grill-confirm-tume')
  assert.equal(tume?.name, 'grill confirm')
  assert.equal(tume?.secondary, 'grill-confirm-tume')
  assert.equal(tume?.paneChip, 'w97:p1')
  assert.equal(tume?.workspaceLabel, 'fleet')
  const nego = nodes.get('agent:durlog-grill-nego')
  assert.equal(nego?.name, 'durlog-grill-nego')
  assert.equal(nego?.secondary, undefined)
  assert.equal(nego?.paneChip, undefined)
})

test('a collapsed manager subtree reads name (descendants · active)', () => {
  const nodes = buildSupervisionNodes(liveShapedBoard())
  assert.equal(collapsedLabel(nodes.get('agent:riko')!), 'riko (4 · 2 active)')
  assert.equal(collapsedLabel(nodes.get('agent:vara')!), 'vara')
  assert.equal(collapsedLabel(nodes.get('operator')!), 'you (9 · 2 active)')
})

test('a live manager never becomes a tombstone and an unknown manager never joins the operator', () => {
  const board = liveShapedBoard()
  const nodes = buildSupervisionNodes(board)
  assert.equal(nodes.has('tombstone:riko'), false)
  assert.equal(nodes.get('operator')?.children.includes('agent:grill-confirm-lubo'), false)
  assert.equal(nodes.get('operator')?.children.includes('agent:impl-pono'), false)
})

test('empty board yields the three empty groups', () => {
  const nodes = buildSupervisionNodes({ workspaces: [], unplaced: [] })
  assert.deepEqual(nodes.get('tree-root')?.children, ['operator', 'unadopted', 'terminals'])
  assert.deepEqual(nodes.get('operator')?.children, [])
  assert.equal(buildSupervisionNodes(undefined).size, 1)
})
