import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

import { agentScreenChoice, screenPanePresentation, unattributedTerminalWarning } from '../src/features/screen/screenPresentation.ts'
import type { AgentDetail, Pane } from '../src/types.ts'

const pane: Pane = {
  pane_id: 'w4R:p5', label: 'pjsafter-rava', agent: '-', tool: '-',
  herdr_status: 'unknown', bus_status: '-', gap: 'no bus row',
}

function agent(overrides: Partial<AgentDetail> = {}): AgentDetail {
  return {
    name: 'pjsafter-rava', tool: 'claude', session_id: '4b4390ef-72dd-4093-a92d-1811543ca3a5',
    directory: '/mnt/bench-nvme/herdr-worktrees/ai-config/pane-join-screen-switch',
    launch_context: {
      env: { SSH_CONNECTION: '100.68.218.51 54089 100.73.240.104 22', SSH_TTY: '/dev/pts/42' },
      git_branch: 'pane-join-screen-switch', process_id: '33882c26-2a62-45d4-b7dd-5658f115dabd',
      terminal_preset_effective: 'herdr', tty: '',
    },
    bus_status: 'listening', herdr_status: '-', gap: 'no visible pane', pane: null, ...overrides,
  }
}

test('agent-less Herdr panes are described as unattributed, never agent-free', () => {
  assert.deepEqual(screenPanePresentation(pane), {
    label: 'Unattributed terminal',
    warning: unattributedTerminalWarning,
  })
  const board = readFileSync(new URL('../src/features/board/BoardPanel.tsx', import.meta.url), 'utf8')
  assert.match(board, /screenPanePresentation/)
  assert.doesNotMatch(board, />shell</)
})

test('agent screen choice requires a live proven pane and defaults to transcript', () => {
  assert.deepEqual(agentScreenChoice(agent(), undefined), {
    enabled: false, active: false, paneID: undefined, reason: 'No live pane.',
  })
  const placed = agent({ pane: { workspace_id: 'w4R', tab_id: 'w4R:t1', pane_id: 'w4R:p1' }, gap: '-' })
  assert.deepEqual(agentScreenChoice(placed, undefined), {
    enabled: true, active: false, paneID: 'w4R:p1', reason: '',
  })
  assert.equal(agentScreenChoice(placed, 'w4R:p1').active, true)
})

test('retired agents cannot expose a screen even if stale pane data exists', () => {
  const stale = agent({ bus_status: 'retired', pane: { workspace_id: 'w4R', tab_id: 'w4R:t1', pane_id: 'w4R:p1' } })
  assert.deepEqual(agentScreenChoice(stale, 'w4R:p1'), {
    enabled: false, active: false, paneID: undefined, reason: 'Retired agents have no live pane.',
  })
})
