import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

import { buildSidebarNodes, buildSupervisionNodes } from '../src/features/sidebar/sidebarNodes.ts'
import { defaultExpanded, managerItems, nodeBuilder } from '../src/features/sidebar/sidebarView.ts'
import { defaultFleetView, parseShellPreferences } from '../src/features/layout/shellPreferences.ts'
import type { Board } from '../src/types.ts'

const board: Board = {
  workspaces: [{
    workspace_id: 'w1', number: 1, label: 'repo', focused: false, pane_count: 2, tab_count: 1, active_tab_id: 't1', agent_status: 'active',
    tabs: [{ tab_id: 't1', number: 1, label: 'seats', focused: false, pane_count: 2, agent_status: 'active', panes: [
      { pane_id: 'w1:p1', agent: 'ziru', tool: 'claude', herdr_status: 'working', bus_status: 'active', gap: '-', manager: 'operator', manager_state: 'operator', created_at: '2026-09-10T08:00:00Z' },
      { pane_id: 'w1:p2', agent: 'impl-kolo', tool: 'claude', herdr_status: 'working', bus_status: 'active', gap: '-', manager: 'ziru', manager_state: 'live', created_at: '2026-09-10T08:01:00Z' },
    ] }],
  }],
  unplaced: [],
}

test('the view toggle switches builders and the default view is supervision', () => {
  assert.equal(nodeBuilder('supervision'), buildSupervisionNodes)
  assert.equal(nodeBuilder('placement'), buildSidebarNodes)
  assert.equal(defaultFleetView, 'supervision')
})

test('expandedItems survive a view switch: one shared array, ids never collide, no reset on toggle', () => {
  const placement = defaultExpanded(buildSidebarNodes(board))
  const supervision = defaultExpanded(buildSupervisionNodes(board))
  assert.deepEqual(placement, ['workspace:w1', 'unplaced'])
  assert.deepEqual(supervision, ['operator', 'agent:ziru', 'unadopted', 'terminals'])
  assert.equal(placement.some((id) => supervision.includes(id)), false)
  assert.deepEqual(managerItems(buildSupervisionNodes(board)), ['agent:ziru'])
  const sidebar = readFileSync(new URL('../src/features/sidebar/FleetSidebar.tsx', import.meta.url), 'utf8')
  // The tree state prop is the same array whichever builder is active, and nothing clears it when `view` changes.
  assert.match(sidebar, /const nodes = view === 'placement' \? placementNodes : supervisionNodes/)
  assert.match(sidebar, /state: \{ expandedItems: expandedItems \?\? emptyExpandedItems, selectedItems \}/)
  assert.doesNotMatch(sidebar, /onExpandedItems\(\[\]\)/)
  assert.doesNotMatch(sidebar, /useEffect\([^)]*\[view\]\)/)
})

test('the fleet view persists in shell preferences and rejects unknown values', () => {
  const rails = { fleet: { width: 260, collapsed: false }, notes: { width: 300, collapsed: true } }
  assert.equal(parseShellPreferences(JSON.stringify({ version: 1, rails, fleetView: 'placement' }))?.fleetView, 'placement')
  assert.equal(parseShellPreferences(JSON.stringify({ version: 1, rails }))?.fleetView, undefined)
  assert.equal(parseShellPreferences(JSON.stringify({ version: 1, rails, fleetView: 'graph' })), null)
  assert.deepEqual(parseShellPreferences(JSON.stringify({ version: 1, rails, knownManagerItems: ['agent:ziru'] }))?.knownManagerItems, ['agent:ziru'])
})
