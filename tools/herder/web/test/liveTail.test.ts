import assert from 'node:assert/strict'
import test from 'node:test'
import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { LiveTailRegion, liveTailNotice, liveTailRowCount, liveTailShown, screenTailRows, stripAnsi } from '../src/features/transcript/liveTail.ts'
import { screenSubscriptionPaneIDs } from '../src/features/workspace/screenSubscriptions.ts'
import { eventStreamURL } from '../src/stream/useFleetStream.ts'
import type { ScreenFrame } from '../src/types.ts'

const frame = (text: string, status: ScreenFrame['status'] = 'available'): ScreenFrame => ({ pane_id: 'w1:p1', status, text, truncated: false })

test('screenTailRows drops blank rows, keeps the last N, and returns ANSI-free text', () => {
  const rows = Array.from({ length: 12 }, (_, index) => `row ${index + 1}`)
  const text = rows.flatMap((row) => [row, '', '   ']).join('\n') + '\n'
  assert.deepEqual(screenTailRows(frame(text)), rows.slice(-liveTailRowCount))
  assert.equal(liveTailRowCount, 8)
  assert.deepEqual(screenTailRows(frame(text), 3), ['row 10', 'row 11', 'row 12'])
  assert.deepEqual(screenTailRows(frame('\x1b[32m● \x1b[0m\x1b[1mBash\x1b[0m(ls)\r\n\x1b]8;;http://x\x07link\x1b]8;;\x07  \n\x1b[2K\n\x1b7done\x1b8')), ['● Bash(ls)', 'link', 'done'])
  assert.equal(stripAnsi('\x1b[?25l\x1b[38;2;10;20;30mcolour\x1b[m'), 'colour')
  assert.deepEqual(screenTailRows(undefined), [])
  assert.deepEqual(screenTailRows(frame('gone', 'unavailable')), [])
  assert.deepEqual(screenTailRows(frame('one\ntwo'), 0), [])
})

test('the region is shown only while visible, working, and in transcript mode', () => {
  assert.equal(liveTailShown({ visible: true, status: 'active', screenMode: false }), true)
  assert.equal(liveTailShown({ visible: false, status: 'active', screenMode: false }), false)
  assert.equal(liveTailShown({ visible: true, status: 'listening', screenMode: false }), false)
  assert.equal(liveTailShown({ visible: true, status: 'blocked', screenMode: false }), false)
  assert.equal(liveTailShown({ visible: true, status: '-', screenMode: false }), false)
  assert.equal(liveTailShown({ visible: true, status: 'active', screenMode: true }), false)
})

test('the region renders the label, the status word, and the rows or a notice', () => {
  const html = renderToStaticMarkup(createElement(LiveTailRegion, { rows: ['● Bash(ls)', 'thinking…'], status: 'active', notice: '' }))
  assert.match(html, /<aside class="transcript-live-tail" aria-label="Live screen tail" style="--live-tail-rows:8">/)
  assert.match(html, /<span class="live-tail-word">live<\/span><span class="live-tail-status">active<\/span>/)
  assert.match(html, /<pre class="live-tail-rows">● Bash\(ls\)\nthinking…<\/pre>/)
  assert.doesNotMatch(html, /live-tail-notice/)
  const waiting = renderToStaticMarkup(createElement(LiveTailRegion, { rows: [], status: 'active', notice: liveTailNotice(undefined) }))
  assert.match(waiting, /<div class="live-tail-notice">waiting for the pane screen…<\/div>/)
  assert.doesNotMatch(waiting, /live-tail-rows"/)
  assert.equal(liveTailNotice(frame('', 'unavailable')), 'pane screen unavailable')
  assert.equal(liveTailNotice({ ...frame('', 'unavailable'), detail: 'pane gone' }), 'pane gone')
  assert.equal(liveTailNotice(frame('x')), '')
})

test('the stream subscribes to a tail pane only while the panel registered it', () => {
  const proven = ['w3:p3']
  const agents = ['mavu', 'zira']
  assert.deepEqual(screenSubscriptionPaneIDs(proven, agents, {}, {}), ['w3:p3'])
  assert.deepEqual(screenSubscriptionPaneIDs(proven, agents, {}, { mavu: 'w1:p1' }), ['w3:p3', 'w1:p1'])
  assert.deepEqual(screenSubscriptionPaneIDs(proven, agents, { zira: 'w2:p1' }, { mavu: 'w1:p1' }), ['w3:p3', 'w1:p1', 'w2:p1'])
  // The same pane in screen mode and as a tail subscribes once; a tail pane for
  // an agent whose panel is gone (name not open) contributes nothing.
  assert.deepEqual(screenSubscriptionPaneIDs([], agents, { mavu: 'w1:p1' }, { mavu: 'w1:p1' }), ['w1:p1'])
  assert.deepEqual(screenSubscriptionPaneIDs([], ['zira'], {}, { mavu: 'w1:p1' }), [])
  // Registered on mount, withdrawn on unmount: the URL follows.
  const shown = { visible: true, status: 'active', screenMode: false }
  const tails: Record<string, string> = {}
  const register = (paneID?: string) => { if (paneID) tails.mavu = paneID; else delete tails.mavu }
  register(liveTailShown(shown) ? 'w1:p1' : undefined)
  assert.equal(eventStreamURL(agents, screenSubscriptionPaneIDs([], agents, {}, tails)), '/api/events?agents=mavu%2Czira&screens=w1%3Ap1')
  register(liveTailShown({ ...shown, visible: false }) ? 'w1:p1' : undefined)
  assert.equal(eventStreamURL(agents, screenSubscriptionPaneIDs([], agents, {}, tails)), '/api/events?agents=mavu%2Czira')
  register(liveTailShown({ ...shown, status: 'listening' }) ? 'w1:p1' : undefined)
  assert.equal(eventStreamURL(agents, screenSubscriptionPaneIDs([], agents, {}, tails)), '/api/events?agents=mavu%2Czira')
})
