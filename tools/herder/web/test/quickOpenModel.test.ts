import assert from 'node:assert/strict'
import test from 'node:test'

import { quickOpenActionRows, quickOpenEnterTarget, quickOpenInitialIndex, quickOpenInitialSelection, quickOpenKeyboardRows, quickOpenMoveSelection, quickOpenSelectedIndex, quickOpenSelectionKeys } from '../src/features/files/quickOpenModel.ts'

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
  assert.deepEqual(quickOpenEnterTarget(rows, 'review', 4, 'available', 2), { kind: 'file', index: 0 })
  assert.deepEqual(quickOpenEnterTarget(rows, 'review', 6, 'available', 2), { kind: 'action', index: noteIndex })
  assert.equal(quickOpenEnterTarget(rows, 'review', 7, 'available', 2), null)
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
  const target = quickOpenEnterTarget(rows, 'podi', -1, 'available')
  assert.deepEqual(target, { kind: 'action', index: rows.findIndex((row) => row.kind === 'agent') })
})

test('Enter opens a partial agent match before matching spaces and files', () => {
  const query = ' ReViEw '
  const rows = quickOpenActionRows(query, spaces, ['my-review-agent'], false)
  assert.deepEqual(quickOpenEnterTarget(rows, query, -1, 'available'), {
    kind: 'action', index: rows.findIndex((row) => row.kind === 'agent'),
  })
})

test('Enter prefers an exact space label over a partial agent match', () => {
  const spaceRows = quickOpenActionRows('main', spaces, ['main-agent'], false)
  assert.deepEqual(quickOpenEnterTarget(spaceRows, 'main', -1, 'none'), { kind: 'action', index: 0 })

  const agentRows = quickOpenActionRows('podi', spaces, ['podi'], false)
  assert.deepEqual(quickOpenEnterTarget(agentRows, 'podi', -1, 'none'), {
    kind: 'action', index: agentRows.findIndex((row) => row.kind === 'agent'),
  })
})

test('Enter chooses the first matching agent in rendered order', () => {
  const rows = quickOpenActionRows('liha', spaces, ['test-liha', 'liha-helper'], false)
  const firstAgent = rows.findIndex((row) => row.kind === 'agent')
  assert.deepEqual(rows[firstAgent], { kind: 'agent', name: 'liha-helper', label: 'liha-helper' })
  assert.deepEqual(quickOpenEnterTarget(rows, 'liha', -1, 'available'), { kind: 'action', index: firstAgent })
})

test('Enter opens the first matching space when no agent matches', () => {
  const rows = quickOpenActionRows('review', spaces, [], false)
  assert.deepEqual(quickOpenEnterTarget(rows, 'review', -1, 'available'), { kind: 'action', index: 0 })
})

test('Enter falls through to a file when no action matches', () => {
  const rows = quickOpenActionRows('missing', spaces, ['test-liha'], false)
  assert.deepEqual(quickOpenEnterTarget(rows, 'missing', -1, 'available'), { kind: 'file' })
})

test('Enter falls through to New note when no action or file matches', () => {
  const rows = quickOpenActionRows('missing', spaces, ['test-liha'], false)
  assert.deepEqual(quickOpenEnterTarget(rows, 'missing', -1, 'none'), { kind: 'action', index: rows.findIndex((row) => row.kind === 'note') })
})

test('Enter keeps the implicit order when the note row is present: an agent match beats the note', () => {
  const rows = quickOpenActionRows('podi', spaces, ['podi-helper'], false)
  assert.equal(rows.some((row) => row.kind === 'note'), true)
  assert.deepEqual(quickOpenEnterTarget(rows, 'podi', -1, 'none'), { kind: 'action', index: rows.findIndex((row) => row.kind === 'agent') })
  assert.deepEqual(quickOpenEnterTarget(rows, 'podi', -1, 'available'), { kind: 'action', index: rows.findIndex((row) => row.kind === 'agent') })
})

test('Enter preserves the highlighted action and file targets', () => {
  const rows = quickOpenActionRows('review', spaces, ['my-review-agent'], false)
  const leading = rows.length - 1
  assert.deepEqual(quickOpenEnterTarget(rows, 'review', 1, 'available'), { kind: 'action', index: 1 })
  assert.deepEqual(quickOpenEnterTarget(rows, 'review', leading, 'available'), { kind: 'file', index: 0 })
  assert.deepEqual(quickOpenEnterTarget(rows, 'review', leading, 'none'), { kind: 'action', index: leading })
  assert.equal(quickOpenEnterTarget(rows, 'review', leading + 1, 'none'), null)
})

test('create is arrow-select only (reflexive Enter goes to the note, never create) and an empty reflexive Enter does nothing', () => {
  const rows = quickOpenActionRows('new place', spaces, [], false)
  const createIndex = rows.findIndex((row) => row.kind === 'create')
  assert.deepEqual(quickOpenEnterTarget(rows, 'new place', -1, 'none'), { kind: 'action', index: rows.findIndex((row) => row.kind === 'note') })
  assert.deepEqual(quickOpenEnterTarget(rows, 'new place', createIndex, 'none'), { kind: 'action', index: createIndex })
  assert.equal(quickOpenEnterTarget(quickOpenActionRows('', spaces, [], false), '', -1, 'none'), null)
})

test('pane-send rows require an active pane, exclude the current space, and preserve reflexive Enter', () => {
  assert.equal(quickOpenActionRows('send', spaces, [], false).some((row) => row.kind.startsWith('send')), false)
  const rows = quickOpenActionRows('send', spaces, [], false, true, 'main')
  assert.deepEqual(rows.filter((row) => row.kind === 'send-space').map((row) => row.label), [
    'Send this pane to review queue', 'Send this pane to weekly review',
  ])
  assert.equal(rows.some((row) => row.kind === 'send-new'), true)
  assert.deepEqual(quickOpenEnterTarget(rows, 'send', -1, 'available'), { kind: 'file' })
})

test('reflexive Enter with no exact action: available → file, none → note, pending → nothing', () => {
  const rows = quickOpenActionRows('missing', spaces, ['test-liha'], false)
  const noteIndex = rows.findIndex((row) => row.kind === 'note')
  assert.deepEqual(quickOpenEnterTarget(rows, 'missing', -1, 'available'), { kind: 'file' })
  assert.deepEqual(quickOpenEnterTarget(rows, 'missing', -1, 'none'), { kind: 'action', index: noteIndex })
  assert.equal(quickOpenEnterTarget(rows, 'missing', -1, 'pending'), null)
  // An explicitly selected note row saves regardless of the lookup state.
  const selected = quickOpenSelectedIndex(rows, [], 'action:note')
  assert.deepEqual(quickOpenEnterTarget(rows, 'missing', selected, 'pending', 0), { kind: 'action', index: noteIndex })
  assert.deepEqual(quickOpenEnterTarget(rows, 'missing', quickOpenSelectedIndex(rows, ['a', 'b'], 'action:note'), 'available', 2), { kind: 'action', index: noteIndex })
})

test('selection keeps its identity: a selected note stays selected when files arrive, a vanished file selection is gone', () => {
  const rows = quickOpenActionRows('missing', spaces, ['missing-helper'], false)
  assert.deepEqual(quickOpenSelectionKeys(rows, ['r\0a.ts']), ['action:create:missing', 'action:agent:missing-helper', 'file:r\0a.ts', 'action:note'])
  const selected = quickOpenMoveSelection(rows, [], null, 'up')
  assert.equal(selected, 'action:note')
  assert.equal(quickOpenSelectedIndex(rows, [], selected), 2)
  assert.equal(quickOpenSelectedIndex(rows, ['r\0a.ts', 'r\0b.ts'], selected), 4)
  const file = quickOpenMoveSelection(rows, ['r\0a.ts', 'r\0b.ts'], 'action:agent:missing-helper', 'down')
  assert.equal(file, 'file:r\0a.ts')
  assert.equal(quickOpenSelectedIndex(rows, ['r\0a.ts', 'r\0b.ts'], file), 2)
  assert.equal(quickOpenSelectedIndex(rows, ['r\0b.ts'], file), -1)
  assert.equal(quickOpenSelectedIndex(rows, [], null), -1)
})

test('arrow moves wrap both ways and start from the ends when nothing is selected', () => {
  const rows = quickOpenActionRows('missing', spaces, ['missing-helper'], false)
  const keys = quickOpenSelectionKeys(rows, ['r\0a.ts'])
  assert.equal(quickOpenMoveSelection(rows, ['r\0a.ts'], null, 'down'), keys[0])
  assert.equal(quickOpenMoveSelection(rows, ['r\0a.ts'], null, 'up'), keys.at(-1))
  assert.equal(quickOpenMoveSelection(rows, ['r\0a.ts'], keys.at(-1), 'down'), keys[0])
  assert.equal(quickOpenMoveSelection(rows, ['r\0a.ts'], keys[0], 'up'), keys.at(-1))
  assert.equal(quickOpenMoveSelection(rows, ['r\0a.ts'], keys[1], 'down'), keys[2])
  assert.equal(quickOpenMoveSelection([], [], null, 'down'), null)
  // A gone selection restarts from the end the arrow points at.
  assert.equal(quickOpenMoveSelection(rows, [], 'file:r\0a.ts', 'down'), keys[0])
})

test('the initial selection is the first openable row as a key on an empty query, none on a typed one', () => {
  assert.equal(quickOpenInitialSelection(quickOpenActionRows('', spaces, ['podi'], false, true, 'main'), ''), 'action:space:main')
  assert.equal(quickOpenInitialSelection(quickOpenActionRows('', [], ['podi'], false), ''), 'action:agent:podi')
  assert.equal(quickOpenInitialSelection(quickOpenActionRows('', [], [], false), ''), null)
  assert.equal(quickOpenInitialSelection(quickOpenActionRows('review', spaces, ['podi'], false), 'review'), null)
})
