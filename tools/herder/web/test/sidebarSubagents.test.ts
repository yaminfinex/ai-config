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

test('terminal panes sort after every agent pane within a workspace', () => {
  const board: Board = {
    workspaces: [{
      workspace_id: 'w1', number: 1, label: 'sort', focused: false, pane_count: 3, tab_count: 2,
      active_tab_id: 't1', agent_status: 'active',
      tabs: [{ tab_id: 't1', number: 1, label: 'terminals', focused: false, pane_count: 1, agent_status: 'unknown', panes: [
        { pane_id: 'p-shell', label: 'shell', current_command: 'htop', agent: '-', tool: '-', herdr_status: '-', bus_status: '-', gap: '-' },
      ] }, { tab_id: 't2', number: 2, label: 'agents', focused: false, pane_count: 2, agent_status: 'active', panes: [
        { pane_id: 'p-z', agent: 'zeta', tool: 'codex', herdr_status: 'active', bus_status: 'active', gap: '-' },
        { pane_id: 'p-a', agent: 'alpha', tool: 'claude', herdr_status: 'idle', bus_status: 'listening', gap: '-' },
      ] }],
    }],
    unplaced: [],
  }
  const nodes = buildSidebarNodes(board)
  assert.deepEqual(nodes.get('workspace:w1')?.children, ['pane:p-z', 'pane:p-a', 'pane:p-shell'])
  assert.equal(nodes.get('pane:p-shell')?.name, 'htop')
})
