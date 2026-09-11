import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'

import { ContextUsed, contextUsedTooltip } from '../src/features/sidebar/ContextUsed.ts'
import { buildSidebarNodes, buildSupervisionNodes } from '../src/features/sidebar/sidebarNodes.ts'
import { defaultExpanded, managerItems, reconcileExpansion } from '../src/features/sidebar/sidebarView.ts'
import { defaultFleetView, parseShellPreferences, shellPreferencesValue } from '../src/features/layout/shellPreferences.ts'
import { treeClickGuardSelector } from '../src/features/sidebar/renameModel.ts'
import type { Board } from '../src/types.ts'

const board: Board = {
  workspaces: [{
    workspace_id: 'w1', number: 1, label: 'repo', focused: false, pane_count: 2, tab_count: 1, active_tab_id: 't1', agent_status: 'active',
    tabs: [{ tab_id: 't1', number: 1, label: 'seats', focused: false, pane_count: 2, agent_status: 'active', panes: [
      { pane_id: 'w1:p1', agent: 'ziru', tool: 'claude', herdr_status: 'working', bus_status: 'active', gap: '-', manager: 'operator', manager_state: 'operator', created_at: '2026-09-10T08:00:00Z', context_used: 243787 },
      { pane_id: 'w1:p2', agent: 'impl-kolo', tool: 'claude', herdr_status: 'working', bus_status: 'active', gap: '-', manager: 'ziru', manager_state: 'live', created_at: '2026-09-10T08:01:00Z' },
    ] }],
  }],
  unplaced: [],
}
const placement = buildSidebarNodes(board)
const supervision = buildSupervisionNodes(board)
const rails = { fleetRail: { width: 260, collapsed: false }, notesRail: { width: 300, collapsed: true } }

test('context used survives both tree projections and renders on agent rows only', () => {
  assert.equal(placement.get('pane:w1:p1')?.contextUsed, 243787)
  assert.equal(supervision.get('agent:ziru')?.contextUsed, 243787)
  assert.equal(placement.get('pane:w1:p2')?.contextUsed, undefined)

  const nestedBoard: Board = { ...board, workspaces: board.workspaces.map((workspace) => ({ ...workspace, tabs: workspace.tabs.map((tab) => ({ ...tab, panes: tab.panes.map((pane) => pane.agent === 'ziru' ? { ...pane, subagents: [{ pane_id: '-', agent: 'ziru-task', tool: 'claude', herdr_status: '-', bus_status: 'active', gap: 'no visible pane', parent_agent: 'ziru', context_used: 1121 }] } : pane) })) })) }
  assert.equal(buildSidebarNodes(nestedBoard).get('pane:w1:p1:subagent:ziru-task')?.contextUsed, 1121)
  assert.equal(buildSupervisionNodes(nestedBoard).get('agent:ziru-task')?.contextUsed, 1121)

  const html = renderToStaticMarkup(createElement('div', { className: 'agent-row', title: contextUsedTooltip(243787) },
    createElement(ContextUsed, { value: 243787 }), createElement(ContextUsed, {})))
  assert.match(html, /class="context-used">244k<\/span>/)
  assert.equal((html.match(/class="context-used"/g) ?? []).length, 1)
  assert.match(html, /context 243,787 tokens/)
  assert.doesNotMatch(html, /context-used">[^<]*%/)
})

test('row click guard contains every trailing interactive control', () => {
  assert.match(treeClickGuardSelector, /\.tree-disclosure/)
  assert.match(treeClickGuardSelector, /\.launch-agent-button/)
  assert.match(treeClickGuardSelector, /\.rename-agent-button/)
})

test('the fleet view persists through the real shell serializer and parser (default supervision)', () => {
  assert.equal(defaultFleetView, 'supervision')
  const written = shellPreferencesValue({ ...rails, expandedItems: ['operator'], knownWorkspaceItems: null, knownManagerItems: ['agent:ziru'], fleetView: 'placement' })
  const read = parseShellPreferences(JSON.stringify(written))
  assert.equal(read?.fleetView, 'placement')
  assert.deepEqual(read?.expandedItems, ['operator'])
  assert.deepEqual(read?.knownManagerItems, ['agent:ziru'])
  assert.equal(read?.knownWorkspaceItems, undefined)
  assert.equal(parseShellPreferences(JSON.stringify(shellPreferencesValue({ ...rails, expandedItems: null, knownWorkspaceItems: null, knownManagerItems: null, fleetView: 'supervision' })))?.fleetView, 'supervision')
  assert.equal(parseShellPreferences(JSON.stringify(shellPreferencesValue({ ...rails, expandedItems: null, knownWorkspaceItems: null, knownManagerItems: null, fleetView: 'groups' })))?.fleetView, 'groups')
  // The hook writes through the serializer, so the field cannot be dropped on one path only.
  const hook = readFileSync(new URL('../src/features/layout/useLayoutPersistence.ts', import.meta.url), 'utf8')
  assert.match(hook, /shellPreferencesValue\(\{ fleetRail, notesRail, expandedItems, knownWorkspaceItems, knownManagerItems, fleetView \}\)/)
})

test('the parser rejects an unknown fleet view and keeps it optional', () => {
  const stored = { version: 1, rails: { fleet: rails.fleetRail, notes: rails.notesRail } }
  assert.equal(parseShellPreferences(JSON.stringify(stored))?.fleetView, undefined)
  assert.equal(parseShellPreferences(JSON.stringify({ ...stored, fleetView: 'graph' })), null)
})

test('expansion transition: first board opens both trees, later boards only add unseen managers and workspaces', () => {
  assert.deepEqual(defaultExpanded(placement), ['workspace:w1', 'unplaced'])
  assert.deepEqual(defaultExpanded(supervision), ['operator', 'agent:ziru', 'unadopted', 'terminals'])
  assert.equal(defaultExpanded(placement).some((id) => defaultExpanded(supervision).includes(id)), false)
  assert.deepEqual(managerItems(supervision), ['agent:ziru'])
  const first = reconcileExpansion(placement, supervision, { expandedItems: null, knownWorkspaceItems: null, knownManagerItems: null })
  assert.deepEqual(first, { expandedItems: ['workspace:w1', 'unplaced', 'operator', 'agent:ziru', 'unadopted', 'terminals'], knownWorkspaceItems: ['workspace:w1'], knownManagerItems: ['agent:ziru'] })
  // A browser with placement-only state opens the supervision groups once, keeping what it had.
  const upgraded = reconcileExpansion(placement, supervision, { expandedItems: ['unplaced'], knownWorkspaceItems: ['workspace:w1'], knownManagerItems: null })
  assert.deepEqual(upgraded, { expandedItems: ['unplaced', 'operator', 'agent:ziru', 'unadopted', 'terminals'], knownManagerItems: ['agent:ziru'] })
  // A new manager subtree opens; a collapsed known one stays collapsed.
  const grown = buildSupervisionNodes({ ...board, unplaced: [{ pane_id: '-', agent: 'sesh-nabi', tool: 'claude', herdr_status: '-', bus_status: 'listening', gap: 'no visible pane', manager: 'impl-kolo', manager_state: 'live', created_at: '2026-09-10T08:02:00Z' }] })
  const added = reconcileExpansion(placement, grown, { expandedItems: ['operator'], knownWorkspaceItems: ['workspace:w1'], knownManagerItems: ['agent:ziru'] })
  assert.deepEqual(added, { expandedItems: ['operator', 'agent:impl-kolo'], knownWorkspaceItems: ['workspace:w1'], knownManagerItems: ['agent:ziru', 'agent:impl-kolo'] })
})

test('expandedItems survive a view switch: the transition never runs on the view and never shrinks the array', () => {
  // A user collapsed riko/ziru and the operator; every re-run with the same board is a no-op, whatever view is showing.
  const collapsed = { expandedItems: ['unadopted', 'terminals'], knownWorkspaceItems: ['workspace:w1'], knownManagerItems: ['agent:ziru'] }
  assert.equal(reconcileExpansion(placement, supervision, collapsed), null)
  assert.equal(reconcileExpansion(placement, supervision, { ...collapsed, expandedItems: [] }), null)
  // The component applies only this transition: no expansion is computed in the component, and the view is not an effect input.
  const sidebar = readFileSync(new URL('../src/features/sidebar/FleetSidebar.tsx', import.meta.url), 'utf8')
  assert.doesNotMatch(sidebar, /defaultExpanded|managerItems\(/)
  assert.equal((sidebar.match(/onExpandedItems\(/g) ?? []).length, 2, 'expected the tree setter wiring and the transition apply, nothing else')
  const effects = [...sidebar.matchAll(/useEffect\([\s\S]*?\}, \[([^\]]*)\]\)/g)].map((match) => match[1])
  assert.equal(effects.filter((deps) => /\bview\b/.test(deps)).length, 1, 'only the selection effect depends on view')
  assert.match(sidebar, /const nodes = view === 'placement' \? placementNodes : view === 'groups' \? groupNodes : supervisionNodes/)
  // The groups map is one more input to the same single transition, never a separate expansion path.
  assert.equal((sidebar.match(/reconcileExpansion\(/g) ?? []).length, 1)
  assert.match(sidebar, /reconcileExpansion\(placementNodes, supervisionNodes, \{ expandedItems, knownWorkspaceItems, knownManagerItems \}, groupNodes\)/)
  assert.match(sidebar, /state: \{ expandedItems: expandedItems \?\? emptyExpandedItems, selectedItems \}/)
  assert.match(sidebar, /<ContextUsed value=\{node\.contextUsed\} \/>/)
  assert.match(sidebar, /\$\{contextUsedTooltip\(node\.contextUsed\)\}/)
})
