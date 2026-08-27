import assert from 'node:assert/strict'
import test from 'node:test'

import { buildSidebarNodes } from '../src/features/sidebar/sidebarNodes.ts'
import type { Board } from '../src/types.ts'

test('explicit subagents are direct children of their parent agent row', () => {
  const board: Board = {
    workspaces: [{
      workspace_id: 'w1', number: 1, label: 'invented', focused: false, pane_count: 1, tab_count: 1,
      active_tab_id: 't1', agent_status: 'active',
      tabs: [{ tab_id: 't1', number: 1, label: 'work', focused: false, pane_count: 1, agent_status: 'active', panes: [{
        pane_id: 'w1:p1', agent: 'probe-fame', tool: 'claude', herdr_status: 'active', bus_status: 'active', gap: '-',
        subagents: [{
          pane_id: '-', agent: 'probe-child', tool: 'claude', herdr_status: '-', bus_status: 'active', gap: 'no visible pane', parent_agent: 'probe-fame',
        }],
      }] }],
    }],
    unplaced: [],
  }

  const nodes = buildSidebarNodes(board)
  assert.deepEqual(nodes.get('pane:w1:p1')?.children, ['pane:w1:p1:subagent:probe-child'])
  assert.equal(nodes.get('pane:w1:p1:subagent:probe-child')?.kind, 'subagent')
  assert.deepEqual(nodes.get('unplaced')?.children, [])
})
