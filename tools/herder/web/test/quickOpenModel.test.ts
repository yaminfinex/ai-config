import assert from 'node:assert/strict'
import test from 'node:test'

import { quickOpenActionRows, quickOpenEnterTarget, quickOpenInitialIndex, quickOpenKeyboardRows } from '../src/features/files/quickOpenModel.ts'

const spaces = [
  { id: 'main', name: 'main', order: 0, created: 0, updated: 0 },
  { id: 'review', name: 'review queue', order: 1, created: 0, updated: 0 },
  { id: 'notes', name: 'weekly review', order: 2, created: 0, updated: 0 },
]

test('quick open ranks exact, prefix, then substring within spaces before agents', () => {
  assert.deepEqual(quickOpenActionRows('review', spaces, ['reviewer', 'my-review-agent', 'review'], false), [
    { kind: 'space', id: 'review', label: 'review queue' },
    { kind: 'space', id: 'notes', label: 'weekly review' },
    { kind: 'create', name: 'review', label: 'Create space “review”' },
    { kind: 'agent', name: 'review', label: 'review' },
    { kind: 'agent', name: 'reviewer', label: 'reviewer' },
    { kind: 'agent', name: 'my-review-agent', label: 'my-review-agent' },
    { kind: 'note', text: 'review', label: 'New note: review' },
  ])
})

test('the note row is always last, carries the trimmed text, and is absent for an empty query', () => {
  const rows = quickOpenActionRows('  todo: call back  ', spaces, ['podi'], true, true, 'main')
  assert.deepEqual(rows.at(-1), { kind: 'note', text: 'todo: call back', label: 'New note: todo: call back' })
  assert.equal(rows.filter((row) => row.kind === 'note').length, 1)
  assert.equal(quickOpenActionRows('', spaces, ['podi'], false).some((row) => row.kind === 'note'), false)
  assert.equal(quickOpenActionRows('   ', spaces, ['podi'], false).some((row) => row.kind === 'note'), false)
})

test('keyboard order is actions, then files, then the note row', () => {
  const rows = quickOpenActionRows('review', spaces, ['reviewer'], false)
  const noteIndex = rows.findIndex((row) => row.kind === 'note')
  assert.deepEqual(quickOpenKeyboardRows(rows, 2), [
    { kind: 'action', index: 0 }, { kind: 'action', index: 1 }, { kind: 'action', index: 2 }, { kind: 'action', index: 3 },
    { kind: 'file', index: 0 }, { kind: 'file', index: 1 },
    { kind: 'action', index: noteIndex },
  ])
  assert.deepEqual(quickOpenEnterTarget(rows, 'review', 4, true, 2), { kind: 'file', index: 0 })
  assert.deepEqual(quickOpenEnterTarget(rows, 'review', 6, true, 2), { kind: 'action', index: noteIndex })
  assert.equal(quickOpenEnterTarget(rows, 'review', 7, true, 2), null)
})

test('a fresh palette starts on the first openable row and a typed query starts unselected', () => {
  assert.equal(quickOpenInitialIndex(quickOpenActionRows('', spaces, ['podi'], false, true, 'main'), ''), 0)
  const agentsOnly = quickOpenActionRows('', [], ['podi'], false, true, null)
  assert.equal(agentsOnly[quickOpenInitialIndex(agentsOnly, '')].kind, 'agent')
  assert.equal(quickOpenInitialIndex(quickOpenActionRows('', [], [], false), ''), -1)
  assert.equal(quickOpenInitialIndex(quickOpenActionRows('review', spaces, ['podi'], false), 'review'), -1)
  assert.equal(quickOpenInitialIndex(quickOpenActionRows('new place', [], [], false), 'new place'), -1)
})

test('an exact space suppresses create and the cap suppresses it deterministically', () => {
  assert.equal(quickOpenActionRows('main', spaces, [], false).some((row) => row.kind === 'create'), false)
  assert.equal(quickOpenActionRows('new place', spaces, [], true).some((row) => row.kind === 'create'), false)
  assert.equal(quickOpenActionRows('', spaces, [], false).some((row) => row.kind === 'create'), false)
})

test('Enter prefers an exact live agent over the synthetic create command', () => {
  const rows = quickOpenActionRows('podi', spaces, ['podi'], false)
  const target = quickOpenEnterTarget(rows, 'podi', -1, true)
  assert.deepEqual(target, { kind: 'action', index: rows.findIndex((row) => row.kind === 'agent') })
})

test('Enter opens a partial agent match before matching spaces and files', () => {
  const query = ' ReViEw '
  const rows = quickOpenActionRows(query, spaces, ['my-review-agent'], false)
  assert.deepEqual(quickOpenEnterTarget(rows, query, -1, true), {
    kind: 'action', index: rows.findIndex((row) => row.kind === 'agent'),
  })
})

test('Enter prefers an exact space label over a partial agent match', () => {
  const spaceRows = quickOpenActionRows('main', spaces, ['main-agent'], false)
  assert.deepEqual(quickOpenEnterTarget(spaceRows, 'main', -1, false), { kind: 'action', index: 0 })

  const agentRows = quickOpenActionRows('podi', spaces, ['podi'], false)
  assert.deepEqual(quickOpenEnterTarget(agentRows, 'podi', -1, false), {
    kind: 'action', index: agentRows.findIndex((row) => row.kind === 'agent'),
  })
})

test('Enter chooses the first matching agent in rendered order', () => {
  const rows = quickOpenActionRows('liha', spaces, ['test-liha', 'liha-helper'], false)
  const firstAgent = rows.findIndex((row) => row.kind === 'agent')
  assert.deepEqual(rows[firstAgent], { kind: 'agent', name: 'liha-helper', label: 'liha-helper' })
  assert.deepEqual(quickOpenEnterTarget(rows, 'liha', -1, true), { kind: 'action', index: firstAgent })
})

test('Enter opens the first matching space when no agent matches', () => {
  const rows = quickOpenActionRows('review', spaces, [], false)
  assert.deepEqual(quickOpenEnterTarget(rows, 'review', -1, true), { kind: 'action', index: 0 })
})

test('Enter falls through to a file when no action matches', () => {
  const rows = quickOpenActionRows('missing', spaces, ['test-liha'], false)
  assert.deepEqual(quickOpenEnterTarget(rows, 'missing', -1, true), { kind: 'file' })
})

test('Enter falls through to New note when no action or file matches', () => {
  const rows = quickOpenActionRows('missing', spaces, ['test-liha'], false)
  assert.deepEqual(quickOpenEnterTarget(rows, 'missing', -1, false), { kind: 'action', index: rows.findIndex((row) => row.kind === 'note') })
})

test('Enter keeps the implicit order when the note row is present: an agent match beats the note', () => {
  const rows = quickOpenActionRows('podi', spaces, ['podi-helper'], false)
  assert.equal(rows.some((row) => row.kind === 'note'), true)
  assert.deepEqual(quickOpenEnterTarget(rows, 'podi', -1, false), { kind: 'action', index: rows.findIndex((row) => row.kind === 'agent') })
  assert.deepEqual(quickOpenEnterTarget(rows, 'podi', -1, true), { kind: 'action', index: rows.findIndex((row) => row.kind === 'agent') })
})

test('Enter preserves the highlighted action and file targets', () => {
  const rows = quickOpenActionRows('review', spaces, ['my-review-agent'], false)
  const leading = rows.length - 1
  assert.deepEqual(quickOpenEnterTarget(rows, 'review', 1, true), { kind: 'action', index: 1 })
  assert.deepEqual(quickOpenEnterTarget(rows, 'review', leading, true), { kind: 'file', index: 0 })
  assert.deepEqual(quickOpenEnterTarget(rows, 'review', leading, false), { kind: 'action', index: leading })
  assert.equal(quickOpenEnterTarget(rows, 'review', leading + 1, false), null)
})

test('create is arrow-select only (reflexive Enter goes to the note, never create) and an empty reflexive Enter does nothing', () => {
  const rows = quickOpenActionRows('new place', spaces, [], false)
  const createIndex = rows.findIndex((row) => row.kind === 'create')
  assert.deepEqual(quickOpenEnterTarget(rows, 'new place', -1, false), { kind: 'action', index: rows.findIndex((row) => row.kind === 'note') })
  assert.deepEqual(quickOpenEnterTarget(rows, 'new place', createIndex, false), { kind: 'action', index: createIndex })
  assert.equal(quickOpenEnterTarget(quickOpenActionRows('', spaces, [], false), '', -1, false), null)
})

test('pane-send rows require an active pane, exclude the current space, and preserve reflexive Enter', () => {
  assert.equal(quickOpenActionRows('send', spaces, [], false).some((row) => row.kind.startsWith('send')), false)
  const rows = quickOpenActionRows('send', spaces, [], false, true, 'main')
  assert.deepEqual(rows.filter((row) => row.kind === 'send-space').map((row) => row.label), [
    'Send this pane to review queue', 'Send this pane to weekly review',
  ])
  assert.equal(rows.some((row) => row.kind === 'send-new'), true)
  assert.deepEqual(quickOpenEnterTarget(rows, 'send', -1, true), { kind: 'file' })
})
