import assert from 'node:assert/strict'
import test from 'node:test'

import { agentBusStatus, agentStatusPresentation } from '../src/shared/agentStatus.ts'
import type { Board } from '../src/types.ts'

test('fleet bus statuses map to honest operator-facing lifecycle semantics', () => {
  assert.deepEqual(agentStatusPresentation('active'), {
    className: 'active', label: 'active', meaning: 'agent is currently working',
  })
  assert.deepEqual(agentStatusPresentation('listening'), {
    className: 'listening', label: 'listening', meaning: 'agent is available and waiting',
  })
  assert.deepEqual(agentStatusPresentation('blocked'), {
    className: 'blocked', label: 'blocked', meaning: 'agent cannot proceed',
  })
  assert.deepEqual(agentStatusPresentation('-'), {
    className: 'unknown', label: 'unknown', meaning: 'agent status is unavailable',
  })
  assert.deepEqual(agentStatusPresentation('invented-future-state'), {
    className: 'unknown', label: 'invented-future-state', meaning: 'agent status is unavailable',
  })
})

test('open-tab status follows placed and unplaced rows from a fleet snapshot', () => {
  const board: Board = {
    workspaces: [{
      workspace_id: 'w1', number: 1, label: 'invented', focused: true, pane_count: 1, tab_count: 1,
      active_tab_id: 't1', agent_status: 'active',
      tabs: [{ tab_id: 't1', number: 1, label: 'work', focused: true, pane_count: 1, agent_status: 'active', panes: [{
        pane_id: 'w1:p1', agent: 'vile', tool: 'codex', herdr_status: 'idle', bus_status: 'active', gap: '-',
        subagents: [{ pane_id: '-', agent: 'vile-task', tool: 'claude', herdr_status: '-', bus_status: 'listening', gap: 'no visible pane', parent_agent: 'vile' }],
      }] }],
    }],
    unplaced: [{ pane_id: '-', agent: 'dore', tool: 'claude', herdr_status: '-', bus_status: 'blocked', gap: 'no pane' }],
  }

  assert.equal(agentBusStatus(board, 'vile'), 'active')
  assert.equal(agentBusStatus(board, 'vile-task'), 'listening')
  assert.equal(agentBusStatus(board, 'dore'), 'blocked')
  assert.equal(agentBusStatus(board, 'missing'), '-')
})
