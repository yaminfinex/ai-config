import assert from 'node:assert/strict'
import test from 'node:test'
import { agentBoardTitle, agentHeaderIdentity, bareHcomName } from '../src/shared/agentIdentity.ts'
import type { Board, Row } from '../src/types.ts'

function row(agent: string, title?: string): Row {
  return { pane_id: `pane-${agent}`, agent, tool: 'codex', herdr_status: 'active', bus_status: 'active', gap: '', ...(title === undefined ? {} : { title }) }
}

const board: Board = {
  workspaces: [{ workspace_id: 'workspace', number: 1, label: 'work', focused: true, pane_count: 1, tab_count: 1, active_tab_id: 'tab', agent_status: 'active', tabs: [{ tab_id: 'tab', number: 1, label: 'agents', focused: true, pane_count: 1, agent_status: 'active', panes: [{ ...row('impl-vimu', 'Pane names'), subagents: [row('review-zami', 'Review')] }] }] }],
  unplaced: [row('build-cudo'), row('riko')],
}

test('bareHcomName returns the last tagged segment or the unchanged bare name', () => {
  assert.equal(bareHcomName('impl-vimu'), 'vimu')
  assert.equal(bareHcomName('review-zami'), 'zami')
  assert.equal(bareHcomName('riko'), 'riko')
  assert.equal(bareHcomName('a-b-cudo'), 'cudo')
  assert.equal(bareHcomName('x-'), 'x-')
  assert.equal(bareHcomName(''), '')
})

test('agent tab title prefers the live board title and falls back to the hcom name', () => {
  assert.equal(agentBoardTitle(board, 'impl-vimu'), 'Pane names')
  assert.equal(agentBoardTitle(board, 'review-zami'), 'Review')
  assert.equal(agentBoardTitle(board, 'build-cudo'), 'build-cudo')
  assert.equal(agentBoardTitle(board, 'missing-melu'), 'missing-melu')
  assert.equal(agentBoardTitle({ ...board, unplaced: [row('blank-bubu', '')] }, 'blank-bubu'), 'blank-bubu')
})

test('tagged in-pane identity shows the bare name with the full name alongside', () => {
  assert.deepEqual(agentHeaderIdentity('impl-vimu'), { primary: 'vimu', secondary: 'impl-vimu' })
})

test('untagged in-pane identity shows only the bare name', () => {
  assert.deepEqual(agentHeaderIdentity('riko'), { primary: 'riko' })
})
