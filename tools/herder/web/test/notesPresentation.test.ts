import assert from 'node:assert/strict'
import test from 'node:test'

import type { Board } from '../src/types.ts'
import type { Note } from '../src/features/notes/notesStore.ts'
import { liveRosterNames, noteGroupRows, noteTransferText } from '../src/features/notes/notesPresentation.ts'

const board: Board = {
  workspaces: [{
    workspace_id: 'w1', number: 1, label: 'one', focused: true, pane_count: 1, tab_count: 1, active_tab_id: 't1', agent_status: 'active',
    tabs: [{ tab_id: 't1', number: 1, label: 'tab', focused: true, pane_count: 1, agent_status: 'active', panes: [{ pane_id: 'p1', agent: 'kilo', tool: 'codex', herdr_status: 'active', bus_status: 'active', gap: '-', subagents: [{ pane_id: '-', agent: 'muro', tool: 'claude', herdr_status: '-', bus_status: 'listening', gap: '-' }] }] }],
  }],
  unplaced: [{ pane_id: '-', agent: 'riko', tool: 'codex', herdr_status: '-', bus_status: 'blocked', gap: '-' }],
}

const note = (id: string, group: string, updated: number, source?: Note['source']): Note => ({ id, group, text: `text ${id}`, created: 1, updated, ...(source ? { source } : {}) })

test('live roster names include nested and unplaced agents once', () => {
  assert.deepEqual(liveRosterNames(board), ['kilo', 'muro', 'riko'])
})

test('group rows are recent-first and flag groups absent from the roster', () => {
  const rows = noteGroupRows([note('1', 'kilo', 10), note('2', 'gone', 30), note('3', 'general', 20)], liveRosterNames(board))
  assert.deepEqual(rows.map((row) => [row.group, row.orphaned]), [['general', false], ['gone', true], ['kilo', false]])
})

test('transfer text uses one exact quote-aware format for copy and hand-off', () => {
  assert.equal(noteTransferText({ ...note('1', 'kilo', 1, { kind: 'file', path: 'src/App.tsx', start: 7, end: 9 }), quote: 'const one = 1\nconst two = 2', text: 'keep both' }), 'src/App.tsx:7-9\n```\nconst one = 1\nconst two = 2\n```\n\nkeep both')
  assert.equal(noteTransferText({ ...note('2', 'kilo', 1, { kind: 'diff', path: 'src/App.tsx', base: 'merge-base', start: 12, end: 12 }), quote: '```literal```', text: '' }), 'src/App.tsx:12 (vs merge-base)\n```\n```literal```\n```')
  assert.equal(noteTransferText({ ...note('3', 'kilo', 1, { kind: 'transcript', agent: 'kilo' }), quote: 'first line\n\nthird line', text: 'reply' }), "from kilo's transcript:\n> first line\n> \n> third line\n\nreply")
  assert.equal(noteTransferText(note('4', 'kilo', 1)), 'text 4')
})

test('legacy anchored notes stay comments and never gain fabricated quote fences', () => {
  assert.equal(noteTransferText(note('1', 'kilo', 1, { kind: 'file', path: 'src/App.tsx', start: 7, end: 9 })), 'src/App.tsx:7-9\ntext 1')
  assert.equal(noteTransferText(note('2', 'kilo', 1, { kind: 'transcript', agent: 'kilo' })), "from kilo's transcript:\ntext 2")
})
