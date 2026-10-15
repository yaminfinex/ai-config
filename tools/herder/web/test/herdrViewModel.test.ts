import assert from 'node:assert/strict'
import test from 'node:test'
import { connectionLabel, focusFrame, followTarget, herdrTermURL, leaveGraceMs, parseNotice, reconnectDelay, resizeFrame, shouldFollow } from '../src/features/herdr/herdrViewModel.ts'
import type { Board } from '../src/types.ts'

const board: Board = {
  workspaces: [{
    workspace_id: 'w1', number: 1, label: 'repo', focused: true, pane_count: 2, tab_count: 1, active_tab_id: 't1', agent_status: 'idle',
    tabs: [{ tab_id: 't1', number: 1, label: 'main', focused: true, pane_count: 2, agent_status: 'idle', panes: [
      { pane_id: 'w1:p1', agent: 'alpha', tool: 'claude', herdr_status: 'idle', bus_status: 'listening', gap: '' },
      { pane_id: 'w1:p2', agent: 'bravo', tool: 'codex', herdr_status: 'idle', bus_status: 'listening', gap: '' },
    ] }],
  }],
  unplaced: [
    { pane_id: '', agent: 'ghost', tool: 'claude', herdr_status: '', bus_status: 'listening', gap: 'no pane' },
    { pane_id: '-', agent: 'shade', tool: 'claude', herdr_status: '-', bus_status: 'listening', gap: 'no visible pane' },
  ],
}

test('the term URL follows the page scheme and host and carries the first size', () => {
  assert.equal(herdrTermURL({ protocol: 'http:', host: 'box.tail:4400' }), 'ws://box.tail:4400/api/herdr/term')
  assert.equal(herdrTermURL({ protocol: 'https:', host: 'box.tail' }, { cols: 200, rows: 50 }), 'wss://box.tail/api/herdr/term?cols=200&rows=50')
})

test('control frames are the server contract: resize and focus', () => {
  assert.deepEqual(JSON.parse(resizeFrame(132, 43)), { type: 'resize', cols: 132, rows: 43 })
  assert.deepEqual(JSON.parse(focusFrame('w1:p2')), { type: 'focus', pane_id: 'w1:p2' })
})

test('only known notices parse', () => {
  assert.deepEqual(parseNotice('{"type":"exit","code":0}'), { type: 'exit', code: 0 })
  assert.deepEqual(parseNotice('{"type":"focus_failed","pane_id":"w1:p9","detail":"gone"}'), { type: 'focus_failed', pane_id: 'w1:p9', detail: 'gone' })
  assert.equal(parseNotice('{"type":"surprise"}'), null)
  assert.equal(parseNotice('not json'), null)
  assert.equal(parseNotice('null'), null)
})

test('reconnects back off from half a second to ten, and leaving keeps the client a minute', () => {
  assert.deepEqual([0, 1, 2, 3, 4, 5, 9].map(reconnectDelay), [500, 1000, 2000, 4000, 8000, 10_000, 10_000])
  assert.equal(leaveGraceMs, 60_000)
  assert.equal(connectionLabel('reconnecting', 1500), 'reconnecting in 2 s…')
  assert.equal(connectionLabel('ended'), 'herdr exited')
  assert.equal(connectionLabel('live'), 'live')
})

test('follow targets the pane of the agent the web is on', () => {
  assert.equal(followTarget(board, { onLens: true, lensAgent: 'bravo', activeAgent: 'alpha' }), 'w1:p2', 'the lens follows the selected card, not the dock')
  assert.equal(followTarget(board, { onLens: false, activeAgent: 'alpha' }), 'w1:p1')
  assert.equal(followTarget(board, { onLens: false, aloneAgent: 'bravo', activeAgent: 'alpha' }), 'w1:p2', 'an agent alone covers the dock')
  assert.equal(followTarget(board, { onLens: false, activeAgent: 'alpha', terminal: 'w2:p7' }), 'w2:p7', 'a terminal the view was opened on is its own pane')
  assert.equal(followTarget(board, { onLens: true, lensAgent: 'bravo', terminal: 'w2:p7' }), 'w2:p7', 'over the lens too')
  assert.equal(followTarget(board, { onLens: false, aloneAgent: 'bravo', terminal: 'w2:p7' }), 'w2:p7', 'and over an agent alone')
  assert.equal(followTarget(board, { onLens: true, lensAgent: '—' }), null, 'a card with no agent follows nothing')
  assert.equal(followTarget(board, { onLens: false, activeAgent: 'ghost' }), null, 'an agent with no pane follows nothing')
  assert.equal(followTarget(board, { onLens: false, activeAgent: 'shade' }), null, 'the board\'s "-" is no pane')
  assert.equal(followTarget(board, { onLens: false, terminal: '-' }), null)
  assert.equal(followTarget(undefined, { onLens: false, activeAgent: 'alpha' }), null)
})

test('follow sends once per new target while open, live and on', () => {
  const live = { open: true, follow: true, connection: 'live' as const, sent: null }
  assert.equal(shouldFollow(live, 'w1:p1'), true)
  assert.equal(shouldFollow({ ...live, sent: 'w1:p1' }, 'w1:p1'), false, 'the same pane is not sent twice')
  assert.equal(shouldFollow({ ...live, sent: 'w1:p1' }, 'w1:p2'), true)
  assert.equal(shouldFollow({ ...live, follow: false }, 'w1:p1'), false)
  assert.equal(shouldFollow({ ...live, open: false }, 'w1:p1'), false, 'a hidden view moves nobody')
  assert.equal(shouldFollow({ ...live, connection: 'reconnecting' }, 'w1:p1'), false)
  assert.equal(shouldFollow(live, null), false)
})
